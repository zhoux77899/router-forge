"""Offline native API contract tests; no Harbor/Python packages are installed."""

import asyncio
import copy
from contextlib import ExitStack
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import types
import unittest
from unittest.mock import patch
from uuid import uuid4


spec = importlib.util.spec_from_file_location("router_forge_harbor_bridge", Path(__file__).with_name("harbor_bridge.py"))
bridge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bridge)
SHA = "a" * 64
TASK_SHA = "b" * 64
GIT_SHA = "c" * 40


def plain(value):
    if isinstance(value, Model):
        return value.model_dump(mode="json")
    if isinstance(value, (GitId, PackageId)):
        return {key: plain(item) for key, item in vars(value).items()}
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, dict):
        return {key: plain(item) for key, item in value.items()}
    if isinstance(value, list):
        return [plain(item) for item in value]
    return value


class Model:
    model_fields = {}

    def __init__(self, **values):
        self.values = values
        for key, value in values.items():
            setattr(self, key, value)

    def model_dump(self, mode="json"):
        return plain(self.values)


class GitId:
    def __init__(self, path, git_url, git_commit_id):
        self.path = Path(path)
        self.git_url = git_url
        self.git_commit_id = git_commit_id

    def get_name(self):
        return self.path.name


class PackageId:
    def __init__(self, org, name, ref):
        self.org, self.name, self.ref = org, name, ref

    def get_name(self):
        return self.org + "/" + self.name


class NativeStubs:
    def __init__(self):
        self.package = True
        self.metadata_calls = []
        self.dataset_inputs = []
        self.factory_calls = []
        self.download_calls = []
        self.resolve_calls = 0
        self.expansion_calls = 0
        self.raise_metadata = False
        self.raise_download = False
        self.cached = False
        self.actual_commit = None
        self.actual_hash = None
        self.actual_name = "native__original-task-name"
        self.missing_task = False
        self.extra_trials = False
        self.extra_tasks = False
        self.trial_name_override = None
        self.drop_parsed = None
        self.drop_trial = None
        self.invalid_options = False
        self.skipped_options = False
        self.preflight_calls = []
        self.preflight_error = None
        self.metadata = Model(name=bridge.DATASET, version="sha256:" + SHA, task_ids=[PackageId("fixture", "task", "sha256:" + TASK_SHA)], dataset_version_content_hash=SHA, dataset_version_id="native-version-record")
        self.modules = {}
        outer = self

        class DatasetConfig(Model):
            model_fields = dict.fromkeys(("name", "version", "ref", "registry_url", "registry_path"))

            def __init__(self, **values):
                outer.dataset_inputs.append(copy.deepcopy(values))
                super().__init__(version=None, ref=None, registry_url=None, registry_path=None, **values)

            def is_package(self):
                return outer.package

            async def get_task_configs(self):
                raise AssertionError("a second floating dataset resolution is forbidden")

        class TaskConfig(Model):
            model_fields = dict.fromkeys(("path", "git_url", "git_commit_id", "name", "ref", "overwrite", "download_dir", "source"))

            def get_task_id(self):
                if self.values.get("name"):
                    org, name = self.name.split("/", 1)
                    return PackageId(org, name, self.ref)
                return GitId(self.path, self.git_url, self.git_commit_id)

        class TrialConfig(Model):
            model_fields = dict.fromkeys(("task", "trial_name"))

            def __init__(self, task):
                name = task.get_task_id().get_name().split("/")[-1]
                trial_name = outer.trial_name_override or name[:32].rstrip("_-") + "__AbC1234"
                super().__init__(task=task, trial_name=trial_name)

        class AgentConfig(Model):
            model_fields = dict.fromkeys(("name", "model_name", "base_url", "model_api_key_name", "model_api_format", "skip_model_parsing", "n_concurrent", "kwargs", "env"))

        class AgentFactory:
            @staticmethod
            def run_preflight(config, *, skip_credential_checks=False):
                outer.preflight_calls.append((config.model_dump(mode="json"), skip_credential_checks))
                if outer.preflight_error is not None:
                    raise outer.preflight_error

        class JobConfig(Model):
            model_fields = dict.fromkeys(("tasks", "datasets", "agents", "n_attempts", "n_concurrent_trials", "retry", "environment", "verifier", "install_only"))

            @classmethod
            def model_validate(cls, raw):
                values = copy.deepcopy(raw)
                if outer.drop_parsed:
                    section, key = outer.drop_parsed
                    if section == "agents":
                        values[section][0].pop(key, None)
                    else:
                        values[section].pop(key, None)
                values["tasks"] = [TaskConfig(**task) for task in values["tasks"]]
                values["agents"] = [AgentConfig(**agent) for agent in values["agents"]]
                return cls(**values)

        class TaskClient:
            async def download_tasks(self, task_ids, overwrite=False, output_dir=None, export=False):
                outer.download_calls.append((task_ids, overwrite, output_dir, export))
                if outer.raise_download:
                    raise OSError("https://private-token@native.fixture.test/task?secret=hidden")
                if len(task_ids) != 1:
                    raise AssertionError("download expanded beyond the selected task")
                if tempfile.gettempdir() != str(output_dir / ".harbor-scratch"):
                    raise AssertionError("native scratch escaped the private download directory")
                task_id = task_ids[0]
                path = output_dir / "native-layout" / "opaque-cache-folder"
                path.mkdir(parents=True, mode=0o700)
                if not outer.missing_task:
                    (path / "task.toml").write_text('name = "' + outer.actual_name + '"\n')
                    (path / "instruction.md").write_text("original instruction\n")
                    (path / "verify.sh").write_text("original verifier\n")
                    (path / "Dockerfile").write_text("FROM original-image\n")
                return Model(results=[Model(path=path, cached=outer.cached, content_hash=outer.actual_hash if outer.actual_hash is not None else getattr(task_id, "ref", "").removeprefix("sha256:"), resolved_git_commit_id=outer.actual_commit if outer.actual_commit is not None else getattr(task_id, "git_commit_id", None))])

        class PackageDatasetClient:
            async def get_dataset_metadata(self, name):
                outer.metadata_calls.append(name)
                print("native progress with a signed URL?secret=hidden")
                if outer.raise_metadata:
                    raise OSError("https://private-token@native.fixture.test/data?secret=hidden")
                return outer.metadata

        class RegistryClient(PackageDatasetClient):
            pass

        RegistryClient.__module__ = "harbor.registry.client.json"

        class RegistryClientFactory:
            @staticmethod
            def create(*args, **kwargs):
                outer.factory_calls.append((args, kwargs))
                return RegistryClient()

        class JobPlan:
            @staticmethod
            async def resolve_task_configs(config):
                outer.resolve_calls += 1
                if config.datasets:
                    raise AssertionError("job attempted dataset resolution")
                return config.tasks * (2 if outer.extra_tasks else 1)

            @staticmethod
            def build_trial_configs(config, task_configs, *, job_id):
                outer.expansion_calls += 1
                values = config.model_dump(mode="json")
                trial = {key: values[key] for key in ("environment", "verifier", "install_only", "timeout_multiplier", "artifacts", "extra_instruction_paths", "extra_instructions")}
                trial.update(agent=values["agents"][0], task=task_configs[0].model_dump(mode="json"))
                if outer.drop_trial:
                    section, key = outer.drop_trial
                    trial[section].pop(key, None)
                return [Model(**trial)] * (2 if outer.extra_trials else 1)

        class Task:
            def __init__(self, task_dir):
                self.name = (task_dir / "task.toml").read_text().split('"')[1]
                if not (task_dir / "verify.sh").is_file():
                    raise ValueError("original verifier missing")

        async def validate_job_config(config):
            class Report:
                skipped = ["native options schema unavailable"] if outer.skipped_options else []

                def raise_for_errors(self):
                    if outer.invalid_options:
                        raise ValueError("unsupported native agent kwargs")
            return Report()

        self.publish("harbor.models.job.config", DatasetConfig=DatasetConfig, JobConfig=JobConfig)
        self.publish("harbor.agents.factory", AgentFactory=AgentFactory)
        self.publish("harbor.models.trial.config", TaskConfig=TaskConfig, TrialConfig=TrialConfig,
                     AgentConfig=AgentConfig,
                     EnvironmentConfig=type("EnvironmentConfig", (Model,), {"model_fields": dict.fromkeys(("type", "mounts", "extra_docker_compose", "env", "kwargs"))}),
                     VerifierConfig=type("VerifierConfig", (Model,), {"model_fields": dict.fromkeys(("disable", "env", "kwargs"))}))
        self.publish("harbor.models.task.id", GitTaskId=GitId, PackageTaskId=PackageId)
        self.publish("harbor.tasks.client", TaskClient=TaskClient, TaskDownloadResult=type("TaskDownloadResult", (Model,), {"model_fields": dict.fromkeys(("path", "cached", "content_hash", "resolved_git_commit_id"))}))
        self.publish("harbor.models.task.task", Task=Task)
        self.publish("harbor.job_plan", JobPlan=JobPlan)
        self.publish("harbor.cli.config_validation", validate_job_config=validate_job_config)
        self.publish("harbor.registry.client.package", PackageDatasetClient=PackageDatasetClient)
        self.publish("harbor.registry.client.factory", RegistryClientFactory=RegistryClientFactory)
        self.publish("harbor.constants", HARBOR_REGISTRY_WEBSITE_URL="https://installed.fixture.test/hub", DEFAULT_REGISTRY_URL="https://installed.fixture.test/native-default.json")
        self.publish("harbor.auth.client", SUPABASE_URL="https://physical.fixture.test/native-db")

    def publish(self, name, **values):
        parts = name.split(".")
        for index in range(1, len(parts) + 1):
            prefix = ".".join(parts[:index])
            self.modules.setdefault(prefix, types.ModuleType(prefix))
        self.modules[name].__dict__.update(values)


class HarborBridgeTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(__file__).resolve().parent / "test-work" / ("native-" + uuid4().hex)
        self.root.mkdir(parents=True, mode=0o700)
        self.stubs = NativeStubs()
        self.patch_modules = patch.dict(sys.modules, self.stubs.modules)
        self.patch_modules.start()
        self.old_tempdir = tempfile.tempdir
        self.patch_env = patch.dict(os.environ)
        self.patch_env.start()

    def tearDown(self):
        tempfile.tempdir = self.old_tempdir
        self.patch_env.stop()
        self.patch_modules.stop()
        shutil.rmtree(self.root)
        try:
            self.root.parent.rmdir()
        except OSError:
            pass

    def call(self, request):
        with patch.object(sys, "argv", ["bridge", json.dumps(request)]), patch("sys.stdout", new_callable=io.StringIO) as output, patch("sys.stderr", new_callable=io.StringIO):
            code = bridge.main()
            data = output.getvalue()
        self.assertNotIn("secret=hidden", data)
        self.assertNotIn("private-token", data)
        return code, json.loads(data)

    def resolve(self):
        code, response = self.call({"operation": "resolve", "dataset": bridge.DATASET})
        self.assertEqual(code, 0, response)
        return response["resolution"]

    def download_request(self, kind="package"):
        directory = self.root / "download"
        directory.mkdir(mode=0o700)
        task = {"name": "fixture/task", "kind": "package", "package_name": "fixture/task", "ref": "sha256:" + TASK_SHA, "revision": "native-revision"}
        if kind == "git":
            task = {"name": "git-task", "kind": "git", "path": "nonstandard/layout/git-task", "git_url": "https://code.fixture.test/repo.git", "git_commit": GIT_SHA}
        return {"operation": "materialize", "download_dir": str(directory), "task": task}

    def job_request(self, kind="package", ca=False):
        env = {"NO_PROXY": "router-forge-gateway,localhost,127.0.0.1", "no_proxy": "router-forge-gateway,localhost,127.0.0.1"}
        environment = {"type": "docker", "delete": True, "mounts": [], "extra_docker_compose": [str(self.root / "relay-compose.json")], "env": env, "kwargs": {}}
        if ca:
            mount = {"type": "bind", "source": str(self.root / "ca.pem"), "target": "/etc/router-forge/ca.pem", "read_only": True}
            environment["mounts"] = [mount]
            environment["kwargs"]["mounts"] = [mount]
            env.update({key: "/etc/router-forge/ca.pem" for key in ("SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO")})
            env["UV_NATIVE_TLS"] = "true"
        task = {"name": "fixture/task", "ref": "sha256:" + TASK_SHA, "overwrite": False, "download_dir": str(self.root / "download"), "source": bridge.DATASET}
        if kind == "git":
            task = {"path": "unusual/git-task", "git_url": "https://code.fixture.test/repo.git", "git_commit_id": GIT_SHA, "overwrite": False, "download_dir": str(self.root / "download")}
        raw = {"n_attempts": 1, "n_concurrent_trials": 1, "retry": {"max_retries": 0}, "install_only": False, "skip_credential_checks": False, "timeout_multiplier": 1,
               "agents": [{"name": "native-agent", "model_name": "model-fixture", "base_url": "http://router-forge-gateway:49123/v1", "model_api_key_name": "ATTEMPT_TOKEN", "model_api_format": "openai_responses", "skip_model_parsing": True, "n_concurrent": 1, "override_timeout_sec": 60, "override_setup_timeout_sec": 30, "max_timeout_sec": 60, "resume_trajectory": False, "skills": [], "mcp_servers": [], "extra_allowed_hosts": ["router-forge-gateway"], "kwargs": {"native_option": 3}, "env": env}],
               "tasks": [task], "environment": environment, "verifier": {"disable": False, "env": env, "kwargs": {}},
               "datasets": [], "metrics": [], "artifacts": [], "extra_instruction_paths": [], "extra_instructions": [], "source_jobs": []}
        path = self.root / "job.json"
        path.write_text(json.dumps(raw))
        request = {"operation": "validate", "config_path": str(path), "api_key_name": "ATTEMPT_TOKEN"}
        if ca:
            request["ca_cert_path"] = str(self.root / "ca.pem")
        return request, raw, path

    def test_package_default_is_delegated_once_without_registry_fallback(self):
        result = self.resolve()
        self.assertEqual(self.stubs.dataset_inputs, [{"name": bridge.DATASET}])
        self.assertEqual(self.stubs.metadata_calls, [bridge.DATASET])
        self.assertEqual(self.stubs.factory_calls, [])
        self.assertEqual(result["version"], "sha256:" + SHA)
        self.assertEqual(result["dataset_digest"], "sha256:" + SHA)
        self.assertEqual(result["dataset_revision"], "native-version-record")
        self.assertEqual(result["registry_url"], "https://installed.fixture.test/hub")
        self.assertEqual(result["registry_metadata_url"], "https://physical.fixture.test/native-db")
        self.assertNotIn("git_commit", result["tasks"][0])

    def test_git_native_default_and_heterogeneous_sources(self):
        self.stubs.package = False
        self.stubs.metadata = Model(name=bridge.DATASET, version="selected-by-native-default", task_ids=[GitId("different/z-task", "https://code.fixture.test/one.git", GIT_SHA.upper()), GitId("another/a-task", "https://code.fixture.test/two.git", "d" * 64)])
        result = self.resolve()
        self.assertEqual(self.stubs.factory_calls, [((), {})])
        self.assertEqual(self.stubs.metadata_calls, [bridge.DATASET])
        self.assertEqual(result["version"], "selected-by-native-default")
        self.assertEqual(result["registry_kind"], "json")
        self.assertEqual([task["name"] for task in result["tasks"]], ["a-task", "z-task"])
        self.assertEqual(result["tasks"][1]["git_commit"], GIT_SHA)

    def test_only_exact_qualified_dataset_is_allowed(self):
        for name in ("swebench-verified", "other/swe-bench-verified", bridge.DATASET + "@latest"):
            with self.subTest(name=name):
                code, response = self.call({"operation": "resolve", "dataset": name})
                self.assertEqual(code, 1)
                self.assertEqual(response["error_code"], "invalid_request")
        self.assertEqual(self.stubs.metadata_calls, [])

    def test_network_failure_is_explicit_and_never_uses_alternate_source(self):
        self.stubs.raise_metadata = True
        code, response = self.call({"operation": "resolve", "dataset": bridge.DATASET})
        self.assertEqual(code, 1)
        self.assertEqual(response["error_code"], "resolution_failed")
        self.assertEqual(self.stubs.factory_calls, [])
        self.assertEqual(len(self.stubs.metadata_calls), 1)

    def test_missing_digest_commit_duplicate_or_task_is_not_invented(self):
        bad = [Model(name=bridge.DATASET, version="sha256:None", task_ids=[PackageId("fixture", "task", "sha256:" + TASK_SHA)]),
               Model(name=bridge.DATASET, version="sha256:" + SHA, task_ids=[PackageId("fixture", "task", "latest")]),
               Model(name=bridge.DATASET, version="sha256:" + SHA, task_ids=[]),
               Model(name=bridge.DATASET, version="sha256:" + SHA, task_ids=[PackageId("fixture", "task", "sha256:" + TASK_SHA)] * 2),
               Model(name=bridge.DATASET, version="sha256:" + SHA, task_ids=[GitId("x/task", "https://code.fixture.test/repo.git", None)])]
        for metadata in bad:
            with self.subTest(metadata=metadata.version):
                self.stubs.metadata = metadata
                code, response = self.call({"operation": "resolve", "dataset": bridge.DATASET})
                self.assertEqual(code, 1)
                self.assertEqual(response["error_code"], "invalid_source")

    def test_unsafe_source_metadata_and_bounds(self):
        constants = self.stubs.modules["harbor.constants"]
        for url in ("https://token@host.fixture.test/hub", "https://host.fixture.test/hub?token=secret", "http://host.fixture.test/hub", "https://host.fixture.test/hub\n"):
            constants.HARBOR_REGISTRY_WEBSITE_URL = url
            code, response = self.call({"operation": "resolve", "dataset": bridge.DATASET})
            self.assertEqual((code, response["error_code"]), (1, "invalid_source"))
        constants.HARBOR_REGISTRY_WEBSITE_URL = "https://installed.fixture.test/hub"
        self.stubs.metadata.values["description"] = "x" * 2048
        with patch.object(bridge, "MAX_METADATA_BYTES", 1024):
            code, response = self.call({"operation": "resolve", "dataset": bridge.DATASET})
        self.assertEqual((code, response["error_code"]), (1, "response_limit"))

    def test_five_hundred_tasks_fit_control_transport(self):
        self.stubs.metadata.task_ids[:] = [PackageId("fixture", "task-" + str(index), "sha256:" + TASK_SHA) for index in range(500)]
        result = self.resolve()
        self.assertEqual(len(result["tasks"]), 500)
        self.assertLess(len(json.dumps(result).encode()), 512 << 10)
        with patch.object(bridge, "MAX_RESPONSE_BYTES", 512):
            code, response = self.call({"operation": "resolve", "dataset": bridge.DATASET})
        self.assertEqual((code, response["error_code"]), (1, "response_limit"))

    def test_exact_package_digest_download_and_actual_native_name(self):
        request = self.download_request()
        code, response = self.call(request)
        self.assertEqual(code, 0, response)
        result = response["materialized"]
        self.assertEqual(result["task"], request["task"])
        self.assertEqual(result["name"], self.stubs.actual_name)
        self.assertNotEqual(result["name"], "task")
        self.assertEqual(result["trial_prefix"], "task")
        self.assertNotEqual(result["trial_prefix"], result["name"])
        self.assertEqual(self.stubs.metadata_calls, [])
        ids, overwrite, directory, export = self.stubs.download_calls[0]
        self.assertEqual(ids[0].ref, request["task"]["ref"])
        self.assertTrue(overwrite)
        self.assertFalse(export)
        self.assertEqual(directory, Path(request["download_dir"]))
        self.assertEqual((Path(result["path"]) / "verify.sh").read_text(), "original verifier\n")
        self.assertEqual((Path(result["path"]) / "Dockerfile").read_text(), "FROM original-image\n")

    def test_native_trial_prefix_is_not_the_materialized_name_or_full_basename(self):
        request = self.download_request()
        basename = "django__a-very-long-native-task-name-with-a-tail"
        request["task"]["name"] = request["task"]["package_name"] = "fixture/" + basename
        code, response = self.call(request)
        self.assertEqual(code, 0, response)
        prefix = response["materialized"]["trial_prefix"]
        self.assertEqual(prefix, basename[:32].rstrip("_-"))
        self.assertNotEqual(prefix, basename)
        self.assertNotEqual(prefix, response["materialized"]["name"])

    def test_unsupported_native_trial_directory_shape_fails_explicitly(self):
        request = self.download_request()
        self.stubs.trial_name_override = "unsafe/prefix__suffix"
        code, response = self.call(request)
        self.assertEqual((code, response["error_code"]), (1, "unsupported_api"))

    def test_exact_git_download_preserves_original_reference(self):
        request = self.download_request("git")
        code, response = self.call(request)
        self.assertEqual(code, 0, response)
        task_id = self.stubs.download_calls[0][0][0]
        self.assertEqual(task_id.git_commit_id, GIT_SHA)
        self.assertEqual(task_id.path.as_posix(), "nonstandard/layout/git-task")
        self.assertEqual(response["materialized"]["task"], request["task"])

    def test_download_rejects_cache_or_wrong_digest_commit(self):
        for kind, flag, value, expected in (("package", "cached", True, "cached_task"), ("package", "actual_hash", "e" * 64, "task_mismatch"), ("git", "actual_commit", "d" * 40, "task_mismatch")):
            with self.subTest(kind=kind, flag=flag):
                request = self.download_request(kind)
                setattr(self.stubs, flag, value)
                code, response = self.call(request)
                self.assertEqual((code, response["error_code"]), (1, expected))
                setattr(self.stubs, flag, None if flag != "cached" else False)
                shutil.rmtree(Path(request["download_dir"]))

    def test_missing_task_download_network_failure_and_nonempty_directory(self):
        request = self.download_request()
        self.stubs.raise_download = True
        code, response = self.call(request)
        self.assertEqual((code, response["error_code"]), (1, "download_failed"))
        self.stubs.raise_download = False
        code, response = self.call(request)
        self.assertEqual((code, response["error_code"]), (1, "invalid_directory"))
        shutil.rmtree(Path(request["download_dir"]))
        request = self.download_request()
        self.stubs.missing_task = True
        code, response = self.call(request)
        self.assertEqual((code, response["error_code"]), (1, "task_missing"))

    def test_native_one_trial_validation_for_git_and_package(self):
        for kind in ("git", "package"):
            request, raw, path = self.job_request(kind, ca=True)
            before = path.read_bytes()
            code, response = self.call(request)
            self.assertEqual(code, 0, response)
            self.assertEqual(response["trials"], 1)
            self.assertEqual(path.read_bytes(), before)
        self.assertEqual(self.stubs.metadata_calls, [])
        self.assertEqual(self.stubs.download_calls, [])
        self.assertEqual(self.stubs.resolve_calls, 2)
        self.assertEqual(self.stubs.expansion_calls, 2)
        self.assertEqual(len(self.stubs.preflight_calls), 2)
        for agent, skip_credentials in self.stubs.preflight_calls:
            self.assertTrue(agent["skip_model_parsing"])
            self.assertFalse(skip_credentials)

    def test_tls_skip_requires_matching_explicit_native_contract(self):
        request, raw, _path = self.job_request()
        request["skip_tls_verification"] = True
        request["ca_cert_path"] = str(self.root / "missing-retained-ca.pem")
        env = raw["environment"]["env"]
        env.update({"ROUTER_FORGE_SKIP_TLS_VERIFICATION": "1", "GIT_SSL_NO_VERIFY": "true", "NODE_TLS_REJECT_UNAUTHORIZED": "0", "npm_config_strict_ssl": "false", "UV_INSECURE_HOST": "*", "CURL_HOME": "/etc/router-forge/tls", "WGETRC": "/etc/router-forge/tls/wgetrc", "PYTHONPATH": "/etc/router-forge/tls/python"})
        mount = {"type": "bind", "source": str(self.root / "tls"), "target": "/etc/router-forge/tls", "read_only": True}
        raw["environment"]["mounts"] = [mount]
        raw["environment"]["kwargs"]["mounts"] = [mount]
        bridge.validate_contract(raw, request)
        for change in (
            lambda config: config["environment"]["env"].pop("GIT_SSL_NO_VERIFY"),
            lambda config: config["environment"]["env"].update(SSL_CERT_FILE="/missing/ca.pem"),
            lambda config: config["environment"]["mounts"][0].update(read_only=False),
            lambda config: config["environment"]["mounts"].append({"type": "bind", "source": request["ca_cert_path"], "target": "/etc/router-forge/ca.pem", "read_only": True}),
        ):
            config = copy.deepcopy(raw)
            change(config)
            with self.assertRaises(bridge.BridgeError):
                bridge.validate_contract(config, request)
        with self.assertRaises(bridge.BridgeError):
            bridge.validate_contract(raw, {**request, "skip_tls_verification": False})

    def test_tls_policy_is_typed_scoped_and_cannot_be_silently_ignored(self):
        code, response = self.call({"operation": "capabilities", "skip_tls_verification": "false"})
        self.assertEqual((code, response["error_code"]), (1, "invalid_request"))
        with patch.object(bridge, "apply_tls_policy") as apply:
            code, response = self.call({"operation": "capabilities", "skip_tls_verification": True})
            self.assertEqual(code, 0, response)
            apply.assert_called_once_with(True)
        with patch.object(bridge, "apply_tls_policy", side_effect=bridge.UnsupportedTLSPolicy("secret=hidden")):
            code, response = self.call({"operation": "capabilities", "skip_tls_verification": True})
            self.assertEqual((code, response["error_code"]), (1, "unsupported_tls"))

    def test_schema_and_expansion_must_honor_routing_overlay_kwargs_task_and_verifier(self):
        for stage, field in (("drop_parsed", ("agents", "base_url")), ("drop_parsed", ("agents", "kwargs")), ("drop_parsed", ("environment", "extra_docker_compose")), ("drop_trial", ("agent", "model_api_format")), ("drop_trial", ("environment", "env")), ("drop_trial", ("environment", "kwargs")), ("drop_trial", ("verifier", "disable")), ("drop_trial", ("task", "ref"))):
            with self.subTest(stage=stage, field=field):
                request, _raw, _path = self.job_request()
                setattr(self.stubs, stage, field)
                code, response = self.call(request)
                self.assertEqual((code, response["error_code"]), (1, "contract_ignored"))
                setattr(self.stubs, stage, None)

    def test_invalid_contract_never_starts_or_expands_a_trial(self):
        for field, value in (("n_attempts", 2), ("n_concurrent_trials", 2), ("retry", {"max_retries": 1}), ("install_only", True), ("datasets", [{"name": bridge.DATASET}])):
            request, raw, path = self.job_request()
            raw[field] = value
            path.write_text(json.dumps(raw))
            code, response = self.call(request)
            self.assertEqual((code, response["error_code"]), (1, "invalid_job"))
        request, raw, path = self.job_request()
        raw["agents"][0]["kwargs"]["version"] = "old-version"
        path.write_text(json.dumps(raw))
        code, response = self.call(request)
        self.assertEqual((code, response["error_code"]), (1, "invalid_job"))
        self.assertEqual(self.stubs.expansion_calls, 0)

    def test_runtime_fields_survive_native_empty_field_exclusion_and_env_serializers(self):
        class SerializedVerifier(Model):
            model_fields = dict.fromkeys(("disable", "kwargs", "extra_allowed_hosts", "env"))

            def model_dump(self, mode="json"):
                return {"disable": self.disable, "env": {"HTTP_PROXY": "${HTTP_PROXY}"}}

        verifier = SerializedVerifier(disable=False, kwargs={}, extra_allowed_hosts=[], env={"HTTP_PROXY": "http://fixture-proxy.invalid:1234"})
        view = bridge.native_view(verifier)
        self.assertEqual(view["kwargs"], {})
        self.assertEqual(view["extra_allowed_hosts"], [])
        self.assertEqual(view["env"], verifier.env)
        self.assertEqual(bridge.native_view(set()), [])
        self.assertEqual(bridge.native_view({"z", "a"}), ["a", "z"])
        request, raw, _path = self.job_request()
        raw["agents"][0]["override_timeout_sec"] = 60.0
        bridge.validate_contract(raw, request)
        raw["agents"][0]["override_timeout_sec"] = float("nan")
        with self.assertRaises(bridge.BridgeError):
            bridge.validate_contract(raw, request)

    def test_actual_multiple_expansion_or_unvalidated_options_fail(self):
        for flag, expected in (("extra_trials", "contract_ignored"), ("extra_tasks", "contract_ignored"), ("invalid_options", "invalid_job"), ("skipped_options", "unsupported_api")):
            request, _raw, _path = self.job_request()
            setattr(self.stubs, flag, True)
            code, response = self.call(request)
            self.assertEqual((code, response["error_code"]), (1, expected))
            setattr(self.stubs, flag, False)

    def test_agent_preflight_failures_are_sanitized_before_task_expansion(self):
        for error, expected in ((ValueError("bare native model: private-token?secret=hidden"), "invalid_job"),
                                (RuntimeError("missing credential: private-token?secret=hidden"), "invalid_job"),
                                (AttributeError("missing native credential API"), "unsupported_api"),
                                (NotImplementedError("native credential preflight unavailable"), "unsupported_api")):
            with self.subTest(error=type(error).__name__):
                request, raw, path = self.job_request()
                before = path.read_bytes()
                self.stubs.preflight_error = error
                code, response = self.call(request)
                self.assertEqual((code, response["error_code"]), (1, expected))
                agent, skip_credentials = self.stubs.preflight_calls[-1]
                self.assertEqual(agent, raw["agents"][0])
                self.assertFalse(skip_credentials)
                self.assertEqual(path.read_bytes(), before)
        self.assertEqual(self.stubs.metadata_calls, [])
        self.assertEqual(self.stubs.download_calls, [])
        self.assertEqual(self.stubs.resolve_calls, 0)
        self.assertEqual(self.stubs.expansion_calls, 0)

    def test_missing_or_incompatible_agent_preflight_api_is_explicit(self):
        factory = self.stubs.modules["harbor.agents.factory"].AgentFactory
        for preflight in (None, lambda config: None):
            with patch.object(factory, "run_preflight", preflight):
                for request in ({"operation": "capabilities"}, self.job_request()[0]):
                    code, response = self.call(request)
                    self.assertEqual((code, response["error_code"]), (1, "unsupported_api"))
        self.assertEqual(self.stubs.preflight_calls, [])
        self.assertEqual(self.stubs.metadata_calls, [])
        self.assertEqual(self.stubs.download_calls, [])
        self.assertEqual(self.stubs.expansion_calls, 0)

    def test_missing_native_capability_fails_without_install_or_alternate_import(self):
        for module, name in (("harbor.tasks.client", "TaskClient"), ("harbor.job_plan", "JobPlan"), ("harbor.cli.config_validation", "validate_job_config"), ("harbor.agents.factory", "AgentFactory")):
            with patch.object(self.stubs.modules[module], name, None):
                code, response = self.call({"operation": "capabilities"})
                self.assertEqual((code, response["error_code"]), (1, "unsupported_api"))
        self.assertEqual(self.stubs.metadata_calls, [])
        self.assertEqual(self.stubs.download_calls, [])


@unittest.skipUnless(os.environ.get("ROUTER_FORGE_NATIVE_HARBOR_PREFLIGHT_TESTS") == "1", "requires the installed Harbor interpreter")
class NativeHarborPreflightTests(unittest.TestCase):
    call = HarborBridgeTests.call
    job_request = HarborBridgeTests.job_request

    def setUp(self):
        self.root = Path(__file__).resolve().parent / "test-work" / ("native-preflight-" + uuid4().hex)
        self.root.mkdir(parents=True, mode=0o700)
        self.addCleanup(shutil.rmtree, self.root)
        self.patches = ExitStack()
        self.addCleanup(self.patches.close)
        self.patches.enter_context(patch.dict(os.environ, {"ROUTER_FORGE_ATTEMPT_TOKEN": uuid4().hex}))
        self.forbidden_calls = []

        def forbidden(name):
            def fail(*args, **kwargs):
                self.forbidden_calls.append(name)
                raise AssertionError("preflight attempted forbidden runtime or network work")
            return fail

        import socket
        import httpx
        self.patches.enter_context(patch.object(socket.socket, "connect", side_effect=forbidden("network.connect")))
        self.patches.enter_context(patch.object(httpx.Client, "send", side_effect=forbidden("http.send")))
        self.patches.enter_context(patch.object(httpx.AsyncClient, "send", side_effect=forbidden("http.async_send")))
        import subprocess
        self.patches.enter_context(patch.object(subprocess, "Popen", autospec=True, side_effect=forbidden("subprocess.Popen")))

        from harbor.agents.factory import AgentFactory
        from harbor.agents.installed.base import BaseInstalledAgent
        from harbor.environments.factory import EnvironmentFactory
        from harbor.job import Job
        from harbor.models.job.config import DatasetConfig
        from harbor.registry.client.factory import RegistryClientFactory
        from harbor.registry.client.package import PackageDatasetClient
        from harbor.tasks.client import TaskClient

        self.preflight = self.patches.enter_context(patch.object(AgentFactory, "run_preflight", autospec=True, side_effect=AgentFactory.run_preflight))
        for owner, names in ((Job, ("create", "run")), (BaseInstalledAgent, ("setup",)),
                             (EnvironmentFactory, ("create_environment_from_config", "create_environment")),
                             (DatasetConfig, ("get_task_configs",)), (RegistryClientFactory, ("create",)),
                             (PackageDatasetClient, ("get_dataset_metadata",)), (TaskClient, ("download_tasks",))):
            for name in names:
                self.patches.enter_context(patch.object(owner, name, autospec=True, side_effect=forbidden(owner.__name__ + "." + name)))

    def opencode_request(self, model_name):
        request, raw, path = self.job_request()
        raw["agents"][0].update(name="opencode", model_name=model_name, model_api_format="openai_chat_completions",
                                model_api_key_name="ROUTER_FORGE_ATTEMPT_TOKEN", kwargs={})
        request["api_key_name"] = "ROUTER_FORGE_ATTEMPT_TOKEN"
        path.write_text(json.dumps(raw))
        return request, path

    def test_bare_model_is_rejected_by_native_agent_preflight_without_runtime_work(self):
        request, path = self.opencode_request("fixture-model")
        before = path.read_bytes()
        code, response = self.call(request)
        self.assertEqual((code, response.get("error_code")), (1, "invalid_job"))
        self.assertEqual(self.preflight.call_count, 1)
        self.assertFalse(self.preflight.call_args.kwargs["skip_credential_checks"])
        self.assertEqual(self.forbidden_calls, [])
        self.assertEqual(path.read_bytes(), before)

    def test_native_qualified_models_preflight_without_runtime_work_or_selector_rewrite(self):
        for model_name in ("router-forge/fixture-model", "router-forge/approved/provider/model-id"):
            with self.subTest(model=model_name):
                request, path = self.opencode_request(model_name)
                before = path.read_bytes()
                code, response = self.call(request)
                self.assertEqual(code, 0, response)
                self.assertEqual(response["trials"], 1)
                self.assertFalse(self.preflight.call_args.kwargs["skip_credential_checks"])
                native_agent = self.preflight.call_args.args[0]
                self.assertEqual(native_agent.model_name, model_name)
                self.assertTrue(native_agent.skip_model_parsing)
                self.assertEqual(self.forbidden_calls, [])
                self.assertEqual(path.read_bytes(), before)
        self.assertEqual(self.preflight.call_count, 2)

    def test_native_credential_preflight_is_not_skipped(self):
        request, _path = self.opencode_request("router-forge/fixture-model")
        os.environ.pop("ROUTER_FORGE_ATTEMPT_TOKEN", None)
        code, response = self.call(request)
        self.assertEqual((code, response.get("error_code")), (1, "invalid_job"))
        self.assertEqual(self.preflight.call_count, 1)
        self.assertFalse(self.preflight.call_args.kwargs["skip_credential_checks"])
        self.assertEqual(self.forbidden_calls, [])


if __name__ == "__main__":
    unittest.main(testRunner=unittest.TextTestRunner(stream=sys.stdout, verbosity=1))
