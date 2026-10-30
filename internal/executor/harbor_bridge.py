"""Isolated adapter to the Python environment recorded by installed Harbor."""

import asyncio
import contextlib
import copy
from enum import Enum
import importlib
import inspect
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import sys
import tempfile
from urllib.parse import urlsplit
from uuid import UUID, uuid4


DATASET = "swe-bench/swe-bench-verified"
MAX_METADATA_BYTES = 32 << 20
MAX_RESPONSE_BYTES = 512 << 10
MAX_TASKS = 10000
NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}(?:/[A-Za-z0-9][A-Za-z0-9_.-]{0,127})?")
COMMIT = re.compile(r"(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


class BridgeError(Exception):
    def __init__(self, code):
        self.code = code
        super().__init__(code)


def require(condition, code="unsupported_api"):
    if not condition:
        raise BridgeError(code)


def text(value, limit, optional=False):
    if optional and value is None:
        return ""
    require(isinstance(value, str) and 0 < len(value.encode("utf-8")) <= limit, "invalid_source")
    require(not any(ord(c) < 32 or ord(c) == 127 for c in value), "invalid_source")
    return value


def source_url(value):
    value = text(value, 2048)
    try:
        parsed = urlsplit(value)
        require(parsed.scheme == "https" and bool(parsed.hostname), "invalid_source")
        require(parsed.username is None and parsed.password is None and not parsed.query and not parsed.fragment, "invalid_source")
        require("?" not in value and "#" not in value and "\\" not in value, "invalid_source")
        parsed.port
    except ValueError:
        raise BridgeError("invalid_source") from None
    return value


def require_fields(model, fields):
    require(isinstance(getattr(model, "model_fields", None), dict))
    require(set(fields).issubset(model.model_fields))


def require_method(owner, name, parameters=()):
    method = getattr(owner, name, None)
    require(callable(method))
    require(set(parameters).issubset(inspect.signature(method).parameters))
    return method


def native_api():
    try:
        job = importlib.import_module("harbor.models.job.config")
        trial = importlib.import_module("harbor.models.trial.config")
        ids = importlib.import_module("harbor.models.task.id")
        tasks = importlib.import_module("harbor.tasks.client")
        plan = importlib.import_module("harbor.job_plan")
        validation = importlib.import_module("harbor.cli.config_validation")
        agents = importlib.import_module("harbor.agents.factory")
        task_model = importlib.import_module("harbor.models.task.task")
        constants = importlib.import_module("harbor.constants")
        package_registry = importlib.import_module("harbor.registry.client.package")
        registry_factory = importlib.import_module("harbor.registry.client.factory")
        api = {
            "dataset": job.DatasetConfig,
            "job": job.JobConfig,
            "task_config": trial.TaskConfig,
            "trial_config": trial.TrialConfig,
            "git_id": ids.GitTaskId,
            "package_id": ids.PackageTaskId,
            "client": tasks.TaskClient,
            "plan": plan.JobPlan,
            "validate": validation.validate_job_config,
            "agent_factory": agents.AgentFactory,
            "task_model": task_model.Task,
            "constants": constants,
            "package_client": package_registry.PackageDatasetClient,
            "registry_factory": registry_factory.RegistryClientFactory,
        }
        require_fields(api["dataset"], ("name", "version", "ref", "registry_url", "registry_path"))
        require_fields(api["task_config"], ("path", "git_url", "git_commit_id", "name", "ref", "overwrite", "download_dir", "source"))
        require_fields(api["trial_config"], ("task", "trial_name"))
        require_fields(api["job"], ("tasks", "datasets", "agents", "n_attempts", "n_concurrent_trials", "retry", "environment", "verifier", "install_only"))
        require_fields(trial.AgentConfig, ("name", "model_name", "base_url", "model_api_key_name", "model_api_format", "skip_model_parsing", "n_concurrent", "kwargs", "env"))
        require_fields(trial.EnvironmentConfig, ("type", "mounts", "extra_docker_compose", "env", "kwargs"))
        require_fields(trial.VerifierConfig, ("disable", "env", "kwargs"))
        require_fields(tasks.TaskDownloadResult, ("path", "cached", "content_hash", "resolved_git_commit_id"))
        require_method(api["dataset"], "is_package")
        require_method(api["task_config"], "get_task_id")
        require_method(api["job"], "model_validate")
        require_method(api["client"], "download_tasks", ("task_ids", "overwrite", "output_dir", "export"))
        require_method(api["plan"], "resolve_task_configs", ("config",))
        require_method(api["plan"], "build_trial_configs", ("config", "task_configs", "job_id"))
        require_method(api["package_client"], "get_dataset_metadata", ("name",))
        require_method(api["registry_factory"], "create")
        require_method(api["agent_factory"], "run_preflight", ("config", "skip_credential_checks"))
        require(callable(api["validate"]))
        require("task_dir" in inspect.signature(api["task_model"]).parameters)
        return api
    except (ImportError, AttributeError, TypeError, ValueError):
        raise BridgeError("unsupported_api") from None


def registry_sources(api, kind, client=None):
    constants = api["constants"]
    if kind == "package":
        logical = getattr(client, "registry_url", None) or getattr(constants, "HARBOR_REGISTRY_WEBSITE_URL", None)
        require(logical is not None)
        metadata_url = getattr(client, "metadata_url", None) or getattr(client, "_url", None)
        if not metadata_url:
            try:
                auth = importlib.import_module("harbor.auth.client")
                metadata_url = getattr(auth, "SUPABASE_URL", None)
            except ImportError:
                metadata_url = None
        return source_url(logical), source_url(metadata_url) if metadata_url else ""
    logical = getattr(client, "_url", None) or getattr(client, "url", None) or getattr(constants, "DEFAULT_REGISTRY_URL", None)
    require(logical is not None)
    logical = source_url(logical)
    return logical, logical


def bound_metadata(metadata):
    dump = require_method(metadata, "model_dump")
    size = 0
    for part in json.JSONEncoder(ensure_ascii=True, separators=(",", ":")).iterencode(dump(mode="json")):
        size += len(part.encode("utf-8"))
        require(size <= MAX_METADATA_BYTES, "response_limit")


def validate_task(task):
    require(isinstance(task, dict), "invalid_source")
    allowed = {"name", "kind", "path", "git_url", "git_commit", "package_name", "ref", "revision"}
    require(set(task).issubset(allowed), "invalid_source")
    name = text(task.get("name"), 257)
    require(NAME.fullmatch(name), "invalid_source")
    kind = task.get("kind")
    if kind == "git":
        relative = text(task.get("path"), 2048)
        path = PurePosixPath(relative)
        require(not path.is_absolute() and str(path) == relative and relative != "." and ".." not in path.parts and ":" not in relative and "\\" not in relative and path.name == name, "invalid_source")
        source_url(task.get("git_url"))
        require(COMMIT.fullmatch(text(task.get("git_commit"), 64)), "invalid_source")
        require(not any(task.get(k) for k in ("package_name", "ref", "revision")), "invalid_source")
    elif kind == "package":
        require(task.get("package_name") == name and "/" in name, "invalid_source")
        require(DIGEST.fullmatch(text(task.get("ref"), 71)), "invalid_source")
        require(not any(task.get(k) for k in ("path", "git_url", "git_commit")), "invalid_source")
        if task.get("revision"):
            text(task["revision"], 128)
    else:
        raise BridgeError("invalid_source")
    return task


async def resolve_dataset(request, api):
    require(request.get("dataset") == DATASET, "invalid_request")
    config = api["dataset"](name=DATASET)
    require(config.name == DATASET, "invalid_source")
    require(config.version is None and config.ref is None and config.registry_url is None and config.registry_path is None, "invalid_source")
    try:
        if config.is_package():
            client = api["package_client"]()
            kind = "package"
        else:
            client = api["registry_factory"].create()
            kind = "json" if client.__class__.__module__ == "harbor.registry.client.json" else "harbor"
        getter = require_method(client, "get_dataset_metadata", ("name",))
        # No version is supplied: the installed native client owns default selection.
        metadata = await getter(config.name)
    except (ImportError, AttributeError):
        raise BridgeError("unsupported_api") from None
    require(metadata.name == DATASET, "invalid_source")
    require(isinstance(metadata.task_ids, list) and 0 < len(metadata.task_ids) <= MAX_TASKS, "invalid_source")
    bound_metadata(metadata)
    version = text(metadata.version, 256)
    dataset_digest = version if DIGEST.fullmatch(version) else ""
    if kind == "package":
        require(dataset_digest, "invalid_source")
        content_hash = getattr(metadata, "dataset_version_content_hash", None)
        if content_hash is not None:
            require("sha256:" + content_hash == dataset_digest, "invalid_source")
    revision = getattr(metadata, "revision", None)
    if revision is None:
        revision = getattr(metadata, "dataset_version_id", None)
    revision = text(str(revision), 128) if revision is not None else ""
    logical, endpoint = registry_sources(api, kind, client)
    tasks = []
    seen = set()
    for task_id in metadata.task_ids:
        name = require_method(task_id, "get_name")()
        if isinstance(task_id, api["git_id"]):
            task = {"name": name, "kind": "git", "path": task_id.path.as_posix(), "git_url": task_id.git_url, "git_commit": text(task_id.git_commit_id, 64).lower()}
        elif isinstance(task_id, api["package_id"]):
            require(name == task_id.org + "/" + task_id.name, "invalid_source")
            task = {"name": name, "kind": "package", "package_name": name, "ref": task_id.ref}
            task_revision = getattr(task_id, "revision", None)
            if task_revision is not None:
                task["revision"] = text(str(task_revision), 128)
        else:
            raise BridgeError("invalid_source")
        validate_task(task)
        require(name not in seen, "invalid_source")
        seen.add(name)
        tasks.append(task)
    tasks.sort(key=lambda task: task["name"])
    return {"dataset": DATASET, "version": version, "registry_kind": kind, "registry_url": logical, "registry_metadata_url": endpoint, "dataset_digest": dataset_digest, "dataset_revision": revision, "tasks": tasks}


def task_config(task, download_dir, api, overwrite):
    if task["kind"] == "git":
        fields = {"path": task["path"], "git_url": task["git_url"], "git_commit_id": task["git_commit"]}
    else:
        fields = {"name": task["package_name"], "ref": task["ref"]}
    fields.update(overwrite=overwrite, download_dir=str(download_dir))
    config = api["task_config"](**fields)
    assert_honored(fields, config.model_dump(mode="json"))
    return config


async def materialize_task(request, api):
    task = validate_task(request.get("task"))
    raw_dir = text(request.get("download_dir"), 2048)
    root = Path(raw_dir)
    require(root.is_absolute() and root.is_dir() and not root.is_symlink() and not any(root.iterdir()), "invalid_directory")
    require(os.name == "nt" or root.stat().st_mode & 0o077 == 0, "invalid_directory")
    root = root.resolve()
    scratch = root / ".harbor-scratch"
    scratch.mkdir(mode=0o700)
    tempfile.tempdir = str(scratch)
    for key in ("TMPDIR", "TEMP", "TMP"):
        os.environ[key] = str(scratch)
    config = task_config(task, root, api, overwrite=True)
    batch = await api["client"]().download_tasks([config.get_task_id()], overwrite=True, output_dir=root, export=False)
    require(isinstance(getattr(batch, "results", None), list) and len(batch.results) == 1, "task_missing")
    result = batch.results[0]
    require(getattr(result, "cached", None) is False, "cached_task")
    if task["kind"] == "git":
        actual = getattr(result, "resolved_git_commit_id", None)
        require(isinstance(actual, str) and COMMIT.fullmatch(actual) and actual.lower() == task["git_commit"].lower(), "task_mismatch")
    else:
        actual = getattr(result, "content_hash", None)
        require(isinstance(actual, str) and "sha256:" + actual == task["ref"], "task_mismatch")
    task_path = Path(result.path).resolve()
    require(task_path != root and task_path.is_relative_to(root) and task_path.is_dir(), "task_missing")
    try:
        original = api["task_model"](task_dir=task_path)
        name = text(original.name, 257)
        require(NAME.fullmatch(name), "task_missing")
    except (OSError, ValueError, AttributeError):
        raise BridgeError("task_missing") from None
    trial_name = text(api["trial_config"](task=config).trial_name, 256)
    prefix, separator, suffix = trial_name.rpartition("__")
    require(separator and NAME.fullmatch(prefix) and "/" not in prefix and NAME.fullmatch(suffix) and "/" not in suffix)
    logical, _endpoint = registry_sources(api, "package" if task["kind"] == "package" else "harbor")
    return {"path": str(task_path), "name": name, "trial_prefix": prefix, "task": task, "registry_url": logical}


def native_view(value):
    if isinstance(value, Enum):
        return native_view(value.value)
    if isinstance(value, (Path, UUID)):
        return str(value)
    if isinstance(value, dict):
        return {key: native_view(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [native_view(item) for item in value]
    if isinstance(value, (set, frozenset)):
        return sorted(native_view(item) for item in value)
    fields = getattr(type(value), "model_fields", None)
    if isinstance(fields, dict):
        result = require_method(value, "model_dump")(mode="json")
        require(isinstance(result, dict))
        # Some native runtime fields are deliberately excluded from serialization.
        for field in fields:
            if hasattr(value, field):
                result[field] = native_view(getattr(value, field))
        return result
    return value


def assert_honored(expected, actual):
    if isinstance(expected, dict):
        require(isinstance(actual, dict), "contract_ignored")
        for key, value in expected.items():
            require(key in actual, "contract_ignored")
            assert_honored(value, actual[key])
    elif isinstance(expected, list):
        require(isinstance(actual, list) and len(expected) == len(actual), "contract_ignored")
        for left, right in zip(expected, actual):
            assert_honored(left, right)
    else:
        require(expected == actual and (not isinstance(expected, bool) or isinstance(actual, bool)), "contract_ignored")


def validate_contract(raw, request):
    require(isinstance(raw, dict), "invalid_job")
    require(raw.get("n_attempts") == 1 and raw.get("n_concurrent_trials") == 1 and raw.get("retry", {}).get("max_retries") == 0, "invalid_job")
    require(raw.get("install_only") is False and raw.get("skip_credential_checks") is False and raw.get("timeout_multiplier") == 1, "invalid_job")
    for key in ("datasets", "metrics", "artifacts", "extra_instruction_paths", "extra_instructions", "source_jobs"):
        require(raw.get(key) == [], "invalid_job")
    require(raw.get("user_agent") is None, "invalid_job")
    require(isinstance(raw.get("agents"), list) and len(raw["agents"]) == 1 and isinstance(raw.get("tasks"), list) and len(raw["tasks"]) == 1, "invalid_job")
    agent, task = raw["agents"][0], raw["tasks"][0]
    environment, verifier = raw.get("environment", {}), raw.get("verifier", {})
    require(isinstance(agent, dict) and isinstance(task, dict) and isinstance(environment, dict) and isinstance(verifier, dict), "invalid_job")
    require(agent.get("n_concurrent") == 1 and agent.get("skip_model_parsing") is True, "invalid_job")
    require(agent.get("model_api_key_name") == request.get("api_key_name") and request.get("api_key_name"), "invalid_job")
    require(agent.get("model_api_format") in ("openai_chat_completions", "openai_responses", "anthropic_messages"), "invalid_job")
    endpoint = urlsplit(agent.get("base_url", ""))
    require(endpoint.scheme == "http" and endpoint.hostname == "router-forge-gateway" and endpoint.port and not endpoint.username and not endpoint.password and not endpoint.query and not endpoint.fragment, "invalid_job")
    require(agent.get("name") and agent.get("model_name") and all(isinstance(agent.get(k), (int, float)) and not isinstance(agent[k], bool) and math.isfinite(agent[k]) and agent[k] > 0 for k in ("override_timeout_sec", "override_setup_timeout_sec", "max_timeout_sec")), "invalid_job")
    require(agent.get("resume_trajectory") is False and not agent.get("load_trajectory") and not agent.get("import_path"), "invalid_job")
    require(not agent.get("skills") and not agent.get("mcp_servers") and endpoint.hostname in agent.get("extra_allowed_hosts", []), "invalid_job")
    require(isinstance(agent.get("kwargs"), dict) and "version" not in agent["kwargs"], "invalid_job")
    require(environment.get("type") == "docker" and environment.get("delete") is True and not environment.get("import_path") and isinstance(environment.get("kwargs"), dict), "invalid_job")
    require(isinstance(environment.get("extra_docker_compose"), list) and len(environment["extra_docker_compose"]) == 1 and Path(environment["extra_docker_compose"][0]).is_absolute(), "invalid_job")
    require(verifier.get("disable") is False and not verifier.get("import_path") and isinstance(verifier.get("kwargs"), dict), "invalid_job")
    overlay = environment.get("env")
    require(isinstance(overlay, dict) and agent.get("env") == overlay and verifier.get("env") == overlay and request["api_key_name"] not in overlay, "invalid_job")
    for key in ("NO_PROXY", "no_proxy"):
        require(isinstance(overlay.get(key), str) and all(host in overlay[key].split(",") for host in ("router-forge-gateway", "localhost", "127.0.0.1")), "invalid_job")
    require(overlay["NO_PROXY"] == overlay["no_proxy"], "invalid_job")
    ca = request.get("ca_cert_path")
    if ca:
        mount = {"type": "bind", "source": ca, "target": "/etc/router-forge/ca.pem", "read_only": True}
        require(mount in environment.get("mounts", []) and mount in environment["kwargs"].get("mounts", []), "invalid_job")
        require(all(overlay.get(k) == "/etc/router-forge/ca.pem" for k in ("SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE")) and overlay.get("UV_NATIVE_TLS") == "true", "invalid_job")
    require(task.get("overwrite") is False and Path(task.get("download_dir", "")).is_absolute(), "invalid_job")
    if task.get("name"):
        saved = {"name": task["name"], "kind": "package", "package_name": task["name"], "ref": task.get("ref")}
        require(not any(task.get(k) for k in ("path", "git_url", "git_commit_id")), "invalid_job")
    else:
        relative = task.get("path", "")
        saved = {"name": PurePosixPath(relative).name, "kind": "git", "path": relative, "git_url": task.get("git_url"), "git_commit": task.get("git_commit_id")}
        require(not task.get("ref"), "invalid_job")
    validate_task(saved)


async def validate_job(request, api):
    config_path = Path(request.get("config_path", ""))
    require(config_path.is_absolute() and config_path.is_file() and config_path.stat().st_size <= MAX_RESPONSE_BYTES, "invalid_job")
    with config_path.open("rb") as stream:
        data = stream.read(MAX_RESPONSE_BYTES + 1)
    require(len(data) <= MAX_RESPONSE_BYTES, "invalid_job")
    raw = json.loads(data)
    validate_contract(raw, request)
    config = api["job"].model_validate(copy.deepcopy(raw))
    parsed = native_view(config)
    assert_honored(raw, parsed)
    validate_contract(parsed, request)
    report = await api["validate"](config)
    require_method(report, "raise_for_errors")()
    require(not getattr(report, "skipped", []))
    try:
        preflight = api["agent_factory"].run_preflight(config.agents[0], skip_credential_checks=config.skip_credential_checks or config.install_only)
        if inspect.isawaitable(preflight):
            preflight = await preflight
        require(preflight is None)
    except (ImportError, AttributeError, NotImplementedError):
        raise BridgeError("unsupported_api") from None
    task_configs = await api["plan"].resolve_task_configs(config)
    require(len(task_configs) == 1, "contract_ignored")
    assert_honored(raw["tasks"][0], task_configs[0].model_dump(mode="json"))
    trials = api["plan"].build_trial_configs(config, task_configs, job_id=uuid4())
    require(isinstance(trials, list) and len(trials) == 1, "contract_ignored")
    trial = native_view(trials[0])
    for key in ("environment", "verifier", "install_only", "timeout_multiplier", "artifacts", "extra_instruction_paths", "extra_instructions"):
        assert_honored(raw[key], trial.get(key))
    assert_honored(raw["agents"][0], trial.get("agent"))
    assert_honored(raw["tasks"][0], trial.get("task"))
    require(not trial.get("user_agent") and not trial.get("source_trial"), "contract_ignored")
    return 1


async def dispatch(request):
    require(isinstance(request, dict) and request.get("operation") in ("resolve", "materialize", "validate", "capabilities"), "invalid_request")
    api = native_api()
    operation = request["operation"]
    try:
        if operation == "resolve":
            return {"ok": True, "resolution": await resolve_dataset(request, api)}
        if operation == "materialize":
            return {"ok": True, "materialized": await materialize_task(request, api)}
        if operation == "validate":
            return {"ok": True, "trials": await validate_job(request, api)}
        return {"ok": True}
    except BridgeError:
        raise
    except Exception:
        code = {"resolve": "resolution_failed", "materialize": "download_failed", "validate": "invalid_job"}.get(operation, "unsupported_api")
        raise BridgeError(code) from None


def main():
    original_stdout = sys.stdout
    try:
        require(len(sys.argv) == 2 and len(sys.argv[1].encode("utf-8")) <= MAX_RESPONSE_BYTES, "invalid_request")
        request = json.loads(sys.argv[1])
        # Native progress, URLs and exceptions must never enter the stdio protocol.
        with contextlib.redirect_stdout(sys.stderr):
            response = asyncio.run(dispatch(request))
        data = json.dumps(response, ensure_ascii=True, separators=(",", ":"))
        require(len(data.encode("utf-8")) <= MAX_RESPONSE_BYTES, "response_limit")
    except BridgeError as exc:
        data = json.dumps({"ok": False, "error_code": exc.code}, separators=(",", ":"))
    except Exception:
        data = '{"ok":false,"error_code":"invalid_request"}'
    original_stdout.write(data + "\n")
    original_stdout.flush()
    return 0 if json.loads(data)["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
