"""Private, capability-checked phase isolation for the installed Harbor process."""

import contextvars
import functools
import importlib
import inspect
import json
import os
from pathlib import Path
import re
import shlex
import ssl


class UnsupportedNetworkPolicy(Exception):
    pass


_network_phase = contextvars.ContextVar("router_forge_network_phase", default="source")
_network_contract = None
_NETWORK_NAMES = {
    "PIP_INDEX_URL", "PIP_EXTRA_INDEX_URL", "PIP_TRUSTED_HOST", "PIP_FIND_LINKS",
    "UV_INDEX", "UV_DEFAULT_INDEX", "UV_EXTRA_INDEX_URL", "UV_FIND_LINKS", "UV_INSECURE_HOST",
    "NPM_CONFIG_REGISTRY", "NPM_CONFIG_DISTURL", "YARN_REGISTRY", "BUN_CONFIG_REGISTRY",
    "CARGO_REGISTRY_INDEX", "CARGO_HTTP_CHECK_REVOKE", "GOINSECURE", "GOSUMDB", "GONOSUMDB",
    "GODEBUG", "NODE_OPTIONS", "JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS",
    "CURL_HOME", "WGETRC", "PYTHONPATH", "APT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM",
    "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_COUNT", "PIP_CONFIG_FILE", "UV_NO_CONFIG",
    "NPM_CONFIG_USERCONFIG", "NPM_CONFIG_GLOBALCONFIG", "ROUTER_FORGE_NETWORK_STAGE",
    "ROUTER_FORGE_NETWORK_BIN",
}
_MODEL_NAMES = {"OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ROUTER_FORGE_ATTEMPT_TOKEN"}
_CA_TOKENS = {"CA", "CACERT", "CACERTS", "CAFILE", "CAPATH", "CAINFO", "CAROOT", "CERT", "CERTS", "CERTIFICATE", "CERTIFICATES", "TRUSTSTORE", "KEYSTORE"}


def is_network_name(name):
    name = name.upper()
    return (name in _NETWORK_NAMES or any(part in name for part in ("PROXY", "MIRROR", "SSL", "TLS"))
            or bool(set(name.split("_")) & _CA_TOKENS)
            or name.startswith(("GIT_CONFIG_", "PIP_", "UV_", "NPM_CONFIG_", "CARGO_REGISTRIES_")))


def clean_network_environment(environment, verifier=False):
    return {key: value for key, value in environment.items()
            if not is_network_name(key) and (not verifier or key.upper() not in _MODEL_NAMES)}


def require_network(condition):
    if not condition:
        raise UnsupportedNetworkPolicy("installed Harbor cannot honor the explicit phase network contract")


def validate_stage(stage, name):
    require_network(isinstance(stage, dict) and isinstance(stage.get("profile"), dict) and isinstance(stage.get("env"), dict))
    profile, env = stage["profile"], stage["env"]
    require_network(profile.get("proxy_mode") in ("direct", "proxy") and profile.get("tls_mode") in ("verify", "skip") and profile.get("trust_mode") in ("system", "custom"))
    require_network(profile.get("scope") == "environment" and profile.get("id") and profile.get("revision") and profile.get("environment_id"))
    require_network(env.get("ROUTER_FORGE_NETWORK_STAGE") == name)
    require_network(all(isinstance(key, str) and re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) and isinstance(value, str) and "\x00" not in value for key, value in env.items()))
    require_network(not any(key.lower() in ("username", "password", "proxy_username", "proxy_password", "proxy_authorization") for key in stage))
    from urllib.parse import urlsplit
    for key, value in env.items():
        if "proxy" in key.lower() and key.lower() != "no_proxy":
            parsed = urlsplit(value)
            require_network(parsed.scheme in ("http", "https") and parsed.hostname and parsed.username is None and parsed.password is None and not parsed.query and not parsed.fragment)
    proxy_keys = {"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"}
    active_keys = set(env) & proxy_keys
    if profile["proxy_mode"] == "direct":
        require_network(not active_keys and not profile.get("proxy_url") and not profile.get("proxy_targets") and not profile.get("proxy_auth") and not profile.get("relay_mode"))
    else:
        targets = profile.get("proxy_targets")
        require_network(isinstance(targets, list) and targets and len(targets) == len(set(targets)) and set(targets).issubset({"http", "https", "all"}))
        expected_keys = set()
        for target in targets:
            expected_keys.update(proxy_keys if target == "all" else {target.upper() + "_PROXY", target + "_proxy"})
        require_network(active_keys == expected_keys and len({env[key] for key in active_keys}) == 1)
        upstream = urlsplit(profile.get("proxy_url", ""))
        require_network(upstream.scheme in ("http", "https") and upstream.hostname and upstream.username is None and upstream.password is None and upstream.path in ("", "/") and not upstream.query and not upstream.fragment)
        actual = env[next(iter(active_keys))]
        if profile.get("proxy_auth") == "basic":
            import ipaddress
            relay = urlsplit(actual)
            try:
                address = ipaddress.ip_address(relay.hostname)
            except ValueError:
                raise UnsupportedNetworkPolicy("authenticated phase requires an owned literal relay address") from None
            require_network(profile.get("has_credentials") is True and profile.get("credential_revision") and relay.scheme == "http" and relay.port and actual != profile["proxy_url"])
            if name == "source":
                require_network(profile.get("relay_mode") == "loopback" and address.is_loopback)
            else:
                require_network(name in ("agent", "verifier") and profile.get("relay_mode") == "docker" and address.is_private and not address.is_loopback and not address.is_unspecified and not address.is_link_local and not address.is_multicast)
        else:
            require_network(profile.get("proxy_auth") == "none" and not profile.get("relay_mode") and actual == profile["proxy_url"])
    require_network(env.get("NO_PROXY") == env.get("no_proxy") and isinstance(env.get("NO_PROXY"), str))
    selected_no_proxy = profile.get("no_proxy", "").strip(", ")
    require_network(env["NO_PROXY"] == selected_no_proxy or name == "agent" and env["NO_PROXY"].startswith(selected_no_proxy + ("," if selected_no_proxy else "")))
    if profile["tls_mode"] == "skip":
        require_network(name not in ("build", "verifier") and profile.get("tls_risk_accepted") is True)
        require_network(env.get("ROUTER_FORGE_SKIP_TLS_VERIFICATION") == "1" and env.get("UV_INSECURE_HOST") == "*")
        require_network(not any(key in env for key in ("SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "npm_config_cafile")))
    else:
        require_network(not any(key in env for key in ("ROUTER_FORGE_SKIP_TLS_VERIFICATION", "GIT_SSL_NO_VERIFY", "NODE_TLS_REJECT_UNAUTHORIZED", "UV_INSECURE_HOST")))
        require_network(env.get("npm_config_strict_ssl") == "true")
    if name in ("agent", "verifier") and profile.get("proxy_auth") == "basic":
        require_network(profile.get("relay_mode") == "docker")
    mirrors = profile.get("mirrors")
    require_network(isinstance(mirrors, dict) and set(mirrors) == {"apt", "pypi", "npm", "node", "uv", "uv_python"})
    for key, mirror in mirrors.items():
        require_network(isinstance(mirror, dict) and mirror.get("mode") in ("native", "custom"))
        if mirror["mode"] == "custom":
            if key == "apt":
                require_network(mirror.get("sources") and not mirror.get("url"))
            else:
                value = urlsplit(mirror.get("url", ""))
                require_network(value.scheme in ("http", "https") and value.hostname and value.username is None and value.password is None and not value.query and not value.fragment)
    mirror_variables = {"pypi": ("PIP_INDEX_URL",), "npm": ("npm_config_registry",), "node": ("NODEJS_ORG_MIRROR", "NVM_NODEJS_ORG_MIRROR", "N_NODE_MIRROR", "npm_config_disturl"), "uv": ("UV_INDEX_URL", "UV_DEFAULT_INDEX"), "uv_python": ("UV_PYTHON_INSTALL_MIRROR",)}
    for key, variables in mirror_variables.items():
        for variable in variables:
            require_network(env.get(variable) == mirrors[key].get("url") if mirrors[key]["mode"] == "custom" else variable not in env)
    ca_keys = {"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "PIP_CERT", "npm_config_cafile"}
    if profile["tls_mode"] == "verify" and profile["trust_mode"] == "custom":
        certificate = str(Path(env["ROUTER_FORGE_NETWORK_BIN"]).parent / "ca.pem")
        require_network(profile.get("ca_cert_path") and all(env.get(key) == certificate for key in ca_keys))
    else:
        require_network(not ca_keys.intersection(env))
    mounts = stage.get("mounts")
    require_network(isinstance(mounts, list))
    if name in ("agent", "verifier"):
        require_network(len(mounts) == 1 and mounts[0].get("type") == "bind" and mounts[0].get("read_only") is True and mounts[0].get("target") == "/etc/router-forge/network/" + name and Path(mounts[0].get("source", "")).is_absolute())
        host_env = stage.get("host_env")
        require_network(isinstance(host_env, dict) and set(host_env) == set(env))
        target, source = mounts[0]["target"], Path(mounts[0]["source"])
        for key, value in env.items():
            expected = str(source / value[len(target) + 1:]) if value.startswith(target + "/") else str(source) if value == target else value
            require_network(host_env[key] == expected)
    else:
        require_network(not mounts and not stage.get("host_env"))
    if name == "build":
        require_network(profile["tls_mode"] == "verify" and profile["trust_mode"] == "system" and profile.get("proxy_auth") != "basic" and all(item["mode"] == "native" for item in mirrors.values()))
    return stage


def validate_phase_contract(contract, raw=None):
    require_network(isinstance(contract, dict))
    for name in ("source", "build", "agent", "verifier"):
        validate_stage(contract.get(name), name)
    require_network(contract["verifier"]["profile"]["tls_mode"] == "verify")
    require_network(len({contract[name]["profile"]["environment_id"] for name in ("source", "build", "agent", "verifier")}) == 1)
    build_args = {key: value for key, value in contract["build"]["env"].items() if key.lower() in ("http_proxy", "https_proxy", "all_proxy", "no_proxy")}
    require_network(contract.get("build_args") == build_args)
    if raw is not None:
        require_network(raw["environment"]["env"] == {})
        require_network(raw["agents"][0]["env"] == contract["agent"]["env"])
        require_network(raw["verifier"]["env"] == contract["verifier"]["env"])
        mounts = contract["agent"].get("mounts", []) + contract["verifier"].get("mounts", [])
        require_network(raw["environment"].get("mounts") == mounts and raw["environment"].get("kwargs", {}).get("mounts") == mounts)
        for mount in mounts:
            require_network(mount.get("type") == "bind" and mount.get("read_only") is True and Path(mount.get("source", "")).is_absolute())
        build_args = contract.get("build_args")
        require_network(isinstance(build_args, dict) and all(key.lower() in ("http_proxy", "https_proxy", "all_proxy", "no_proxy") and isinstance(value, str) for key, value in build_args.items()))
        overlay_path = Path(raw["environment"]["extra_docker_compose"][0])
        require_network(overlay_path.is_file() and overlay_path.stat().st_size <= 512 << 10)
        overlay = json.loads(overlay_path.read_text())
        service = overlay.get("services", {}).get("main", {})
        if "build" in service:
            require_network(service["build"].get("args") == build_args)
    return contract


def verify_mirror_command(command, stage):
    mirrors = stage["profile"]["mirrors"]
    options = (("pypi", ("--index-url", "-i"), {"$PIP_INDEX_URL", "${PIP_INDEX_URL}"}),
               ("uv", ("--default-index", "--index-url", "-i"), {"$UV_DEFAULT_INDEX", "${UV_DEFAULT_INDEX}", "$UV_INDEX_URL", "${UV_INDEX_URL}"}),
               ("npm", ("--registry",), {"$npm_config_registry", "${npm_config_registry}", "$NPM_CONFIG_REGISTRY", "${NPM_CONFIG_REGISTRY}"}))
    for key, flags, variables in options:
        if mirrors[key]["mode"] != "custom":
            continue
        for line in command.splitlines():
            if key == "pypi" and re.search(r"(?:^|[ ;])uv(?:[ ;]|$)", line):
                continue
            if key == "uv" and not re.search(r"(?:^|[ ;])uv(?:[ ;]|$)", line):
                continue
            for flag in flags:
                pattern = r"(?:^|\s)" + re.escape(flag) + r"(?:=|\s+)([\"'][^\"']*[\"']|[^\s;]+)"
                for match in re.finditer(pattern, line):
                    value = match.group(1).strip("\"'")
                    require_network(value == mirrors[key]["url"] or value in variables)
            if key in ("pypi", "uv"):
                require_network(not re.search(r"--(?:extra-index-url|extra-index|find-links)(?:=|\s)", line))
    if mirrors["node"]["mode"] == "custom":
        require_network(not re.search(r"https?://(?:nodejs\.org|unofficial-builds\.nodejs\.org)/", command))
    if mirrors["uv_python"]["mode"] == "custom":
        require_network("https://github.com/astral-sh/python-build-standalone/releases/" not in command)


def verify_client_command(command, stage):
    verify_mirror_command(command, stage)
    strict = stage["profile"]["tls_mode"] == "verify"
    require_network(not re.search(r"(?:^|[\s;])(?:export\s+)?(?:HTTP_PROXY|http_proxy|HTTPS_PROXY|https_proxy|ALL_PROXY|all_proxy|NO_PROXY|no_proxy)\s*=", command))
    if strict:
        require_network(not re.search(r"(?:^|[\s;])(?:export\s+)?(?:GIT_SSL_NO_VERIFY|NODE_TLS_REJECT_UNAUTHORIZED|PIP_TRUSTED_HOST|UV_INSECURE_HOST|SSL_CERT_FILE|SSL_CERT_DIR|REQUESTS_CA_BUNDLE|NODE_EXTRA_CA_CERTS)\s*=", command))
    tools = {"curl", "wget", "git", "pip", "pip3", "uv", "npm", "npx"}
    for line in command.splitlines():
        if not any(re.search(r"(?:^|[\s;/])" + tool + r"\s", line) for tool in tools):
            continue
        try:
            lexer = shlex.shlex(line, posix=True, punctuation_chars=";&|()")
            lexer.whitespace_split = True
            tokens = list(lexer)
        except ValueError:
            raise UnsupportedNetworkPolicy("native client command cannot validate explicit network options") from None
        for position, token in enumerate(tokens):
            tool = token.rsplit("/", 1)[-1]
            if tool not in tools:
                continue
            options = []
            for option in tokens[position + 1:]:
                if option and set(option).issubset(set(";&|()")):
                    break
                options.append(option)
            for index, option in enumerate(options):
                flag = option.split("=", 1)[0].lower()
                if tool == "curl":
                    require_network(flag not in {"--proxy", "--noproxy", "--preproxy", "--proxy-user", "--proxy-header", "--config", "--cacert", "--capath", "--proxy-cacert", "--proxy-capath", "--socks4", "--socks4a", "--socks5", "--socks5-hostname"})
                    if strict:
                        require_network(flag not in {"--insecure", "--proxy-insecure"})
                    if option.startswith("-") and not option.startswith("--"):
                        for character in option[1:]:
                            require_network(character not in "KxU" and (not strict or character != "k"))
                            if character in "dDeEFHmortu wz".replace(" ", ""):
                                break
                elif tool == "wget":
                    require_network(flag not in {"--execute", "--config", "--proxy", "--no-proxy", "--ca-certificate", "--ca-directory"} and not option.startswith(("-e", "-Y")))
                    if strict:
                        require_network(flag != "--no-check-certificate" and option.lower() not in ("--check-certificate=off", "--check-certificate=false"))
                elif tool == "git":
                    configuration = options[index + 1] if option == "-c" and index + 1 < len(options) else option[2:] if option.startswith("-c") else ""
                    key = configuration.split("=", 1)[0].lower()
                    require_network(not key.startswith(("http.", "https.", "include.", "includeif.", "url.")) and key != "core.gitproxy" and not option.startswith("--config-env"))
                elif tool in ("pip", "pip3", "uv"):
                    require_network(flag not in {"--proxy", "--cert", "--client-cert", "--config-file"})
                    if strict:
                        require_network(flag not in {"--trusted-host", "--insecure-host", "--allow-insecure-host"})
                elif tool in ("npm", "npx"):
                    require_network(flag not in {"--proxy", "--https-proxy", "--noproxy", "--cafile", "--userconfig", "--globalconfig"})
                    if strict:
                        require_network(option.lower() not in {"--strict-ssl=false", "--strict-ssl=0", "--no-strict-ssl"})


def scoped_execution_command(command, stage, verifier=False):
    require_network(isinstance(command, str) and "\x00" not in command)
    verify_client_command(command, stage)
    # This runs AFTER Docker's image ENV, native persistent env and per-exec env
    # have been merged. Empty strings are not used as a substitute for unsetting.
    lines = ["test -x /usr/bin/env && test -x /usr/bin/cut && test -x /usr/bin/tr || exit 78",
             "rf_original_path=$PATH",
             "for rf_name in $(/usr/bin/env | /usr/bin/cut -d= -f1); do",
             "  rf_upper=$(printf '%s' \"$rf_name\" | /usr/bin/tr '[:lower:]' '[:upper:]')",
             "  case \"$rf_upper\" in",
             "    *PROXY*|*MIRROR*|*TLS*|*SSL*|*_CA_*|CA_*|*_CA|*_CERT_*|CERT_*|*_CERT|*CACERT*|*CAFILE*|*CAPATH*|*CAINFO*|*CAROOT*|*CERTIFICATE*|*TRUSTSTORE*|*KEYSTORE*|GIT_CONFIG_*|PIP_*|UV_*|NPM_CONFIG_*|CARGO_REGISTRIES_*) unset \"$rf_name\" ;;",
             "  esac", "done"]
    unset = sorted(_NETWORK_NAMES | (_MODEL_NAMES if verifier else set()))
    lines.append("unset " + " ".join(unset + [item.lower() for item in unset]))
    for key, value in sorted(stage["env"].items()):
        lines.append("export " + key + "=" + shlex.quote(value))
    lines.append("export PATH=" + shlex.quote(stage["env"].get("ROUTER_FORGE_NETWORK_BIN", "")) + ":\"$rf_original_path\"")
    lines.append("unset rf_name rf_upper rf_original_path")
    # Native Harbor uses bash for main-task commands. Preserve that language,
    # command, user, cwd, timeout, redirections and the original verifier file.
    lines.append("exec bash -c " + shlex.quote(command))
    return "\n".join(lines)


def native_network_api(agent_name):
    try:
        docker = importlib.import_module("harbor.environments.docker.docker").DockerEnvironment
        verifier = importlib.import_module("harbor.verifier.verifier").Verifier
        factory = importlib.import_module("harbor.agents.factory").AgentFactory
        agent_type = importlib.import_module("harbor.models.agent.name").AgentName
        agent = factory.get_agent_class(agent_type(agent_name))
        for owner, name, parameters in ((docker, "_compose_exec", ("command", "service", "env")), (docker, "exec", ("command", "env")), (docker, "upload_file", ("source_path",)), (docker, "upload_dir", ("source_dir",)), (verifier, "verify", ("self",)), (agent, "setup", ("environment",)), (agent, "run", ("environment",))):
            method = getattr(owner, name, None)
            require_network(callable(method) and inspect.iscoroutinefunction(method) and set(parameters).issubset(inspect.signature(method).parameters))
        require_network("_merge_env" in inspect.getsource(docker.exec) and "_compose_exec" in inspect.getsource(docker.exec))
        require_network("exec_shell_args" in inspect.getsource(docker._compose_exec))
        return docker, verifier, agent
    except (ImportError, AttributeError, TypeError, ValueError, OSError):
        raise UnsupportedNetworkPolicy("installed Harbor lacks required phase lifecycle APIs") from None


def install_phase_network(contract, agent_name):
    global _network_contract
    validate_phase_contract(contract)
    docker, verifier, agent = native_network_api(agent_name)
    require_network(_network_contract is None)
    _network_contract = contract

    def scope_method(method, phase):
        @functools.wraps(method)
        async def scoped(*args, **kwargs):
            token = _network_phase.set(phase)
            try:
                return await method(*args, **kwargs)
            finally:
                _network_phase.reset(token)
        return scoped

    verifier.verify = scope_method(verifier.verify, "verifier")
    agent.setup = scope_method(agent.setup, "agent")
    agent.run = scope_method(agent.run, "agent")
    compose_exec = docker._compose_exec
    signature = inspect.signature(compose_exec)

    @functools.wraps(compose_exec)
    async def execute(*args, **kwargs):
        phase = _network_phase.get()
        if phase not in ("agent", "verifier"):
            return await compose_exec(*args, **kwargs)
        bound = signature.bind(*args, **kwargs)
        require_network(bound.arguments.get("service") == "main")
        stage = contract[phase]
        merged = clean_network_environment(bound.arguments.get("env") or {}, phase == "verifier")
        merged.update(stage["env"])
        bound.arguments["env"] = merged
        bound.arguments["command"] = scoped_execution_command(bound.arguments["command"], stage, phase == "verifier")
        return await compose_exec(*bound.args, **bound.kwargs)

    docker._compose_exec = execute
    def validate_upload(filename, stage):
        require_network(not filename.is_symlink() and filename.is_file())
        if filename.suffix in (".sh", ".bash"):
            require_network(filename.stat().st_size <= 1 << 20)
            verify_client_command(filename.read_text(), stage)

    def wrap_upload(method, parameter, directory=False):
        signature = inspect.signature(method)

        @functools.wraps(method)
        async def upload_checked(*args, **kwargs):
            phase = _network_phase.get()
            bound = signature.bind(*args, **kwargs)
            if phase in ("agent", "verifier"):
                source = Path(bound.arguments[parameter])
                if directory:
                    require_network(source.is_dir() and not source.is_symlink())
                    count, total = 0, 0
                    for root, directories, files in os.walk(source, followlinks=False):
                        require_network(not any((Path(root) / name).is_symlink() for name in directories))
                        for name in files:
                            filename = Path(root) / name
                            count += 1
                            total += filename.stat().st_size
                            require_network(count <= 4096 and total <= 16 << 20)
                            validate_upload(filename, contract[phase])
                else:
                    validate_upload(source, contract[phase])
            return await method(*args, **kwargs)
        return upload_checked

    for name, parameter, directory in (("upload_file", "source_path", False), ("upload_dir", "source_dir", True)):
        method = getattr(docker, name, None)
        if callable(method) and inspect.iscoroutinefunction(method) and parameter in inspect.signature(method).parameters:
            setattr(docker, name, wrap_upload(method, parameter, directory))
        else:
            raise UnsupportedNetworkPolicy("native upload cannot validate original phase client options")


def bypass_proxy(host, port, no_proxy):
    import ipaddress
    hostname = host.lower().rstrip(".")
    if hostname == "localhost" or hostname.endswith(".localhost"):
        return True
    try:
        address = ipaddress.ip_address(hostname)
        if address.is_loopback:
            return True
    except ValueError:
        address = None
    for entry in no_proxy.split(","):
        entry = entry.strip().lower()
        if entry == "*":
            return True
        if not entry:
            continue
        try:
            if address is not None and address in ipaddress.ip_network(entry, strict=False):
                return True
            ipaddress.ip_network(entry, strict=False)
            continue
        except ValueError:
            pass
        entry_port = None
        if entry.startswith("[") and "]:" in entry:
            entry, entry_port = entry[1:].split("]:", 1)
        elif entry.count(":") == 1:
            entry, entry_port = entry.rsplit(":", 1)
        if entry_port is not None and entry_port != str(port):
            continue
        suffix_only = entry.startswith((".", "*."))
        suffix = entry.removeprefix("*").lstrip(".")
        if hostname.endswith("." + suffix) or not suffix_only and hostname == suffix:
            return True
    return False


def proxy_for_url(stage, target):
    from urllib.parse import urlsplit
    parsed = urlsplit(str(target))
    require_network(parsed.scheme in ("http", "https") and parsed.hostname and parsed.username is None)
    if stage["profile"]["proxy_mode"] == "direct" or bypass_proxy(parsed.hostname, parsed.port or (443 if parsed.scheme == "https" else 80), stage["env"].get("NO_PROXY", stage["profile"].get("no_proxy", ""))):
        return None
    env = stage["env"]
    return env.get(parsed.scheme.upper() + "_PROXY") or env.get(parsed.scheme + "_proxy") or env.get("ALL_PROXY") or env.get("all_proxy")


def install_host_network(fallback, host_paths=False):
    def selected_stage():
        selected = _network_contract.get(_network_phase.get(), fallback) if _network_contract else fallback
        if host_paths and selected.get("host_env"):
            return {**selected, "env":selected["host_env"]}
        return selected

    try:
        httpx = importlib.import_module("httpx")
    except ModuleNotFoundError as exc:
        if exc.name != "httpx":
            raise UnsupportedNetworkPolicy("native HTTPX dependencies are unavailable") from None
    else:
        require_network(all("proxy" in inspect.signature(owner).parameters and "trust_env" in inspect.signature(owner).parameters for owner in (httpx.HTTPTransport, httpx.AsyncHTTPTransport)))

        class ScopedTransport(httpx.BaseTransport):
            def __init__(self, stage):
                self.transports = {}

            def handle_request(self, request):
                stage = selected_stage()
                proxy = proxy_for_url(stage, request.url)
                key = (_network_phase.get(), proxy)
                if key not in self.transports:
                    self.transports[key] = httpx.HTTPTransport(proxy=proxy, verify=ssl.create_default_context(), trust_env=False, retries=0)
                return self.transports[key].handle_request(request)

            def close(self):
                for transport in self.transports.values():
                    transport.close()

        class ScopedAsyncTransport(httpx.AsyncBaseTransport):
            def __init__(self, stage):
                self.transports = {}

            async def handle_async_request(self, request):
                stage = selected_stage()
                proxy = proxy_for_url(stage, request.url)
                key = (_network_phase.get(), proxy)
                if key not in self.transports:
                    self.transports[key] = httpx.AsyncHTTPTransport(proxy=proxy, verify=ssl.create_default_context(), trust_env=False, retries=0)
                return await self.transports[key].handle_async_request(request)

            async def aclose(self):
                for transport in self.transports.values():
                    await transport.aclose()

        def constructor(original, transport_type):
            @functools.wraps(original)
            def init(self, *args, **kwargs):
                require_network(kwargs.get("transport") is None and not kwargs.get("mounts") and kwargs.get("proxy") is None)
                kwargs.update(transport=transport_type(selected_stage()), trust_env=False, follow_redirects=False, http2=False)
                return original(self, *args, **kwargs)
            return init

        httpx.Client.__init__ = constructor(httpx.Client.__init__, ScopedTransport)
        httpx.AsyncClient.__init__ = constructor(httpx.AsyncClient.__init__, ScopedAsyncTransport)

    for name in ("requests", "pip._vendor.requests"):
        try:
            requests = importlib.import_module(name)
        except ModuleNotFoundError as exc:
            if exc.name == name or name.startswith(exc.name + "."):
                continue
            raise UnsupportedNetworkPolicy("native Requests dependencies are unavailable") from None
        original = requests.Session.send

        def request_sender(send):
            @functools.wraps(send)
            def send_scoped(self, request, **kwargs):
                proxy = proxy_for_url(selected_stage(), request.url)
                kwargs["proxies"] = {"http": proxy, "https": proxy} if proxy else {}
                kwargs["allow_redirects"] = False
                return send(self, request, **kwargs)
            return send_scoped

        requests.Session.send = request_sender(original)

    import urllib.request
    from urllib.error import HTTPError

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, msg, headers, newurl):
            raise HTTPError(request.full_url, code, "scoped redirects are disabled", headers, fp)

    class ExplicitProxy(urllib.request.ProxyHandler):
        def __init__(self, stage):
            self.stage = stage
            proxies = {key: stage["env"].get(key.upper() + "_PROXY") for key in ("http", "https")}
            super().__init__({key: value for key, value in proxies.items() if value})

        def proxy_open(self, request, proxy, scheme):
            from urllib.parse import urlsplit
            selected = proxy_for_url(self.stage, request.full_url)
            if selected is None:
                return None
            value = urlsplit(selected)
            original_type = request.type
            request.set_proxy(value.netloc, value.scheme)
            if original_type == value.scheme or original_type == "https":
                return None
            return self.parent.open(request, timeout=request.timeout)

    def urlopen_scoped(url, data=None, timeout=30, *, context=None):
        opener = urllib.request.build_opener(ExplicitProxy(selected_stage()), urllib.request.HTTPSHandler(context=ssl.create_default_context()), NoRedirect())
        return opener.open(url, data=data, timeout=timeout)

    urllib.request.urlopen = urlopen_scoped


def configure_process_network(stage, host_paths=False):
    validate_stage(stage, stage.get("env", {}).get("ROUTER_FORGE_NETWORK_STAGE"))
    for key in list(os.environ):
        if is_network_name(key):
            os.environ.pop(key, None)
    os.environ.update(stage["env"])
    profile = stage["profile"]

    def tls_for_phase():
        selected = _network_contract.get(_network_phase.get(), stage) if _network_contract else stage
        skip = selected["profile"]["tls_mode"] == "skip"
        environment = selected.get("host_env",selected["env"]) if host_paths else selected["env"]
        return skip, None if skip else environment.get("SSL_CERT_FILE")

    ca = stage["env"].get("SSL_CERT_FILE") if profile["tls_mode"] == "verify" else None
    apply_tls_policy(profile["tls_mode"] == "skip", ca, tls_for_phase)
    install_host_network(stage,host_paths)


def configure_source_network(stage):
    validate_stage(stage, "source")
    configure_process_network(stage,True)


def run_network_harbor(launcher, config_path, network_path):
    import runpy
    import sys
    filename = Path(network_path)
    require_network(filename.is_absolute() and filename.is_file() and filename.stat().st_size <= 512 << 10)
    contract = json.loads(filename.read_text())
    raw = json.loads(Path(config_path).read_text())
    validate_phase_contract(contract, raw)
    configure_source_network(contract["source"])
    install_phase_network(contract, raw["agents"][0]["name"])
    sys.argv = [launcher, "run", "--config", config_path]
    runpy.run_path(launcher, run_name="__main__")
