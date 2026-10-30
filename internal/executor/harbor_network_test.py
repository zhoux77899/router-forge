"""Socket-blocked, stdlib-only phase contract and native lifecycle fixtures."""

import asyncio
import copy
from enum import Enum
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import types
import unittest
from unittest.mock import patch
from uuid import uuid4


def blocked(*args, **kwargs):
    raise AssertionError("offline phase fixture forbids sockets")


native_connect = socket.socket.connect

def connect_owned_loopback(self, address):
    if isinstance(address,tuple) and address[0] in ("127.0.0.1","::1"):
        return native_connect(self,address)
    return blocked(self,address)

socket.socket.connect = connect_owned_loopback
socket.socket.connect_ex = blocked
socket.create_connection = blocked


def fresh():
    spec = importlib.util.spec_from_file_location("router_forge_phase_fixture", Path(__file__).with_name("harbor_network.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def stage(name, root):
    host_directory = str(root / "network" / name)
    directory = host_directory if name in ("source", "build") else "/etc/router-forge/network/" + name
    env = {"ROUTER_FORGE_NETWORK_STAGE":name,"ROUTER_FORGE_NETWORK_BIN":directory + "/bin","npm_config_strict_ssl":"true","NO_PROXY":"","no_proxy":""}
    result = {"profile":{"id":"profile_" + name,"revision":"revision_1","name":name,"scope":"environment","environment_id":"environment_fixture","proxy_mode":"direct","tls_mode":"verify","trust_mode":"system","tls_risk_accepted":False,"has_credentials":False,"mirrors":{key:{"mode":"native"} for key in ("apt","pypi","npm","node","uv","uv_python")}},
              "env":env,"mounts": [] if name in ("source","build") else [{"type":"bind","source":host_directory,"target":directory,"read_only":True}]}
    if name in ("agent", "verifier"):
        result["host_env"] = {**env,"ROUTER_FORGE_NETWORK_BIN":str(Path(host_directory) / "bin")}
    return result


class Docker:
    def __init__(self):
        self.calls = []
        self.uploads = []

    def _merge_env(self, env):
        merged = {"HTTP_PROXY":"http://ambient.invalid:8080","https_proxy":"http://ambient.invalid:8080","No_Proxy":"ambient.invalid","SSL_CERT_FILE":"/ambient/ca.pem","Requests_CA_Bundle":"/ambient/ca.pem","GIT_SSL_NO_VERIFY":"true","NODE_TLS_REJECT_UNAUTHORIZED":"0","NODE_OPTIONS":"--require /ambient/shim","PYTHONPATH":"/ambient/agent/shim","ROUTER_FORGE_ATTEMPT_TOKEN":"private-model-lease","OPENAI_API_KEY":"private-model-lease","KEEP_NATIVE":"preserved"}
        merged.update(env or {})
        return merged

    async def exec(self, command, env=None):
        return await self._compose_exec(command, service="main", env=self._merge_env(env), cwd=None, timeout_sec=None, user=None)

    async def _compose_exec(self, command, *, service, env, cwd, timeout_sec, user):
        # The real native API uses _platform.exec_shell_args(command).
        self.calls.append({"command":command,"env":env,"service":service})
        return self.calls[-1]

    async def upload_file(self, source_path, target_path):
        self.uploads.append((source_path,target_path))

    async def upload_dir(self, source_dir, target_dir):
        self.uploads.append((source_dir,target_dir))


class Agent:
    async def setup(self, environment):
        return await environment.exec("printf setup")

    async def run(self, instruction, environment, context):
        return await environment.exec("printf agent")


class Verifier:
    def __init__(self, environment):
        self.environment = environment

    async def verify(self):
        return await self.environment.exec("printf verifier", env={"HTTP_PROXY":"http://persistent-agent.invalid:8080"})


class AgentName(str, Enum):
    OPENCODE = "opencode"


class Factory:
    @staticmethod
    def get_agent_class(name):
        return Agent


class PhaseTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(__file__).resolve().parent / "test-work" / ("phase-" + uuid4().hex)
        self.root.mkdir(parents=True,mode=0o700)
        self.addCleanup(shutil.rmtree,self.root)
        self.network = fresh()
        self.contract = {name:stage(name,self.root) for name in ("source","build","agent","verifier")}
        self.contract["build_args"] = {"NO_PROXY":"","no_proxy":""}

    def test_explicit_missing_selection_scopes_and_tls(self):
        self.network.validate_phase_contract(self.contract)
        for mutate in (lambda n:n.pop("agent"),lambda n:n["source"]["profile"].update(scope="desktop"),lambda n:n["build"]["profile"].update(tls_mode="skip",tls_risk_accepted=True),lambda n:n["verifier"]["profile"].update(tls_mode="skip",tls_risk_accepted=True),lambda n:n["source"]["profile"].pop("proxy_mode")):
            value=copy.deepcopy(self.contract);mutate(value)
            with self.assertRaises(self.network.UnsupportedNetworkPolicy):
                self.network.validate_phase_contract(value)
        source=copy.deepcopy(self.contract["source"]);source["profile"].update(proxy_mode="proxy");source["env"]["HTTP_PROXY"]="http://user:private-password@proxy.invalid"
        with self.assertRaises(self.network.UnsupportedNetworkPolicy):
            self.network.validate_stage(source,"source")

    def test_actual_native_shape_requires_independent_env_mounts_and_build_args(self):
        overlay=self.root / "compose.json"
        overlay.write_text(json.dumps({"services":{"main":{"build":{"args":self.contract["build_args"]}}}}))
        mounts=self.contract["agent"]["mounts"] + self.contract["verifier"]["mounts"]
        raw={"environment":{"env":{},"mounts":mounts,"kwargs":{"mounts":mounts},"extra_docker_compose":[str(overlay)]},"agents":[{"env":self.contract["agent"]["env"]}],"verifier":{"env":self.contract["verifier"]["env"]}}
        self.network.validate_phase_contract(self.contract,raw)
        for mutate in (lambda r:r["environment"]["env"].update(HTTP_PROXY="http://ambient.invalid"),lambda r:r["verifier"].update(env=self.contract["agent"]["env"]),lambda r:r["environment"]["kwargs"].update(mounts=[])):
            value=copy.deepcopy(raw);mutate(value)
            with self.assertRaises(self.network.UnsupportedNetworkPolicy):
                self.network.validate_phase_contract(self.contract,value)
        overlay.write_text(json.dumps({"services":{"main":{"build":{"args":{"HTTP_PROXY":"http://wrong.invalid"}}}}}))
        with self.assertRaises(self.network.UnsupportedNetworkPolicy):
            self.network.validate_phase_contract(self.contract,raw)

    def test_custom_mirrors_reject_literal_native_overrides_and_unrelated_shell_variables(self):
        agent=self.contract["agent"]
        for key in ("pypi","npm","uv","node","uv_python"):
            agent["profile"]["mirrors"][key]={"mode":"custom","url":"https://" + key + ".mirror.invalid/"}
        self.network.verify_mirror_command('pip install --index-url "$PIP_INDEX_URL" package\nuv pip install --default-index "$UV_DEFAULT_INDEX" package\nnpm install --registry "$npm_config_registry" package',agent)
        for command in ('echo "$UNRELATED"; pip install --index-url https://wrong.invalid package','pip install --index-url=https://wrong.invalid package','uv pip install --index-url https://wrong.invalid package','pip install --extra-index-url https://other.invalid package','npm install --registry=https://wrong.invalid package','curl https://nodejs.org/dist/current.tar.gz','curl https://github.com/astral-sh/python-build-standalone/releases/download/item'):
            with self.subTest(command=command),self.assertRaises(self.network.UnsupportedNetworkPolicy):
                self.network.verify_mirror_command(command,agent)

    def test_strict_verifier_rejects_client_tls_proxy_and_shared_config_overrides(self):
        verifier = self.contract["verifier"]
        for command in ('curl --insecure --version', 'curl -kfsS --version', 'curl --proxy http://other.invalid --version', 'curl --config /shared/curlrc --version', 'wget --no-check-certificate --help', 'git -c http.sslVerify=false --version', 'pip install --trusted-host other.invalid package', 'uv pip install --insecure-host other.invalid package', 'npm install --strict-ssl=false package', 'NODE_TLS_REJECT_UNAUTHORIZED=0 node --version'):
            with self.subTest(command=command), self.assertRaises(self.network.UnsupportedNetworkPolicy):
                self.network.scoped_execution_command(command, verifier, True)
        self.network.scoped_execution_command('curl --version; wget --help; git --version; uv pip install --system pytest', verifier, True)

    def test_private_contract_rejects_host_path_target_and_build_proxy_tampering(self):
        for mutate in (lambda c:c["agent"]["host_env"].update(ROUTER_FORGE_NETWORK_BIN="/wrong/shared/bin"), lambda c:c["build_args"].update(HTTPS_PROXY="http://unselected.invalid"), lambda c:c["verifier"]["profile"].update(environment_id="other-host"), lambda c:c["agent"]["env"].update(HTTP_PROXY="http://unselected.invalid")):
            value=copy.deepcopy(self.contract);mutate(value)
            with self.assertRaises(self.network.UnsupportedNetworkPolicy):
                self.network.validate_phase_contract(value)

    def test_no_proxy_matching_and_target_selection(self):
        source=self.contract["source"];source["profile"].update(proxy_mode="proxy",proxy_auth="none",proxy_url="http://selected.invalid:8080",proxy_targets=["https"],no_proxy="internal.invalid,.example.invalid,10.0.0.0/8,exact.invalid:443");source["env"].update(HTTPS_PROXY="http://selected.invalid:8080",https_proxy="http://selected.invalid:8080",NO_PROXY=source["profile"]["no_proxy"],no_proxy=source["profile"]["no_proxy"])
        self.network.validate_stage(source,"source")
        for host in ("localhost","127.0.0.1","internal.invalid","sub.example.invalid","10.2.3.4","exact.invalid"):
            self.assertIsNone(self.network.proxy_for_url(source,"https://" + host + "/item"))
        self.assertEqual(self.network.proxy_for_url(source,"https://external.invalid/item"),"http://selected.invalid:8080")
        self.assertIsNone(self.network.proxy_for_url(source,"http://external.invalid/item"))
        self.assertEqual(self.network.proxy_for_url(source,"https://exact.invalid:8443/item"),"http://selected.invalid:8080")

    def test_native_lifecycle_unsets_merged_both_case_proxy_tls_and_model_auth(self):
        # Fresh subclasses prevent wrapping another fixture's lifecycle methods.
        docker=type("DockerFixture",(Docker,),{})
        agent=type("AgentFixture",(Agent,),{})
        verifier=type("VerifierFixture",(Verifier,),{})
        factory=type("FactoryFixture",(),{"get_agent_class":staticmethod(lambda name:agent)})
        modules={"harbor.environments.docker.docker":types.SimpleNamespace(DockerEnvironment=docker),"harbor.verifier.verifier":types.SimpleNamespace(Verifier=verifier),"harbor.agents.factory":types.SimpleNamespace(AgentFactory=factory),"harbor.models.agent.name":types.SimpleNamespace(AgentName=AgentName)}
        selected=self.contract["agent"];selected["profile"].update(tls_mode="skip",tls_risk_accepted=True);selected["env"].update(ROUTER_FORGE_SKIP_TLS_VERIFICATION="1",UV_INSECURE_HOST="*",GIT_SSL_NO_VERIFY="true",NODE_TLS_REJECT_UNAUTHORIZED="0",npm_config_strict_ssl="false",PYTHONPATH="/etc/router-forge/network/agent/python")
        selected["host_env"]={**selected["env"],"ROUTER_FORGE_NETWORK_BIN":str(self.root / "network" / "agent" / "bin"),"PYTHONPATH":str(self.root / "network" / "agent" / "python")}
        with patch.dict(sys.modules,modules):
            self.network.install_phase_network(self.contract,"opencode")
            environment=docker()
            asyncio.run(agent().setup(environment))
            asyncio.run(agent().run("instruction",environment,None))
            asyncio.run(verifier(environment).verify())
            self.assertEqual(self.network._network_phase.get(),"source")
            first,last=environment.calls[0],environment.calls[-1]
            self.assertEqual(first["env"]["ROUTER_FORGE_NETWORK_STAGE"],"agent")
            self.assertEqual(first["env"]["NODE_TLS_REJECT_UNAUTHORIZED"],"0")
            self.assertEqual(last["env"]["ROUTER_FORGE_NETWORK_STAGE"],"verifier")
            for key in ("HTTP_PROXY","https_proxy","No_Proxy","SSL_CERT_FILE","Requests_CA_Bundle","NODE_TLS_REJECT_UNAUTHORIZED","GIT_SSL_NO_VERIFY","PYTHONPATH","ROUTER_FORGE_ATTEMPT_TOKEN","OPENAI_API_KEY"):
                self.assertNotIn(key,last["env"])
            self.assertEqual(last["env"]["KEEP_NATIVE"],"preserved")
            self.assertIn("unset",last["command"])
            token=self.network._network_phase.set("verifier")
            try:
                with self.assertRaises(self.network.UnsupportedNetworkPolicy):
                    asyncio.run(environment._compose_exec("true",service="unapproved-sidecar",env={},cwd=None,timeout_sec=None,user=None))
                original_tests=self.root / "owned-original-verifier"
                original_tests.mkdir()
                original_script=original_tests / "test.sh"
                content="#!/bin/bash\ncurl --insecure --version\n"
                original_script.write_text(content)
                before=len(environment.uploads)
                with self.assertRaises(self.network.UnsupportedNetworkPolicy):
                    asyncio.run(environment.upload_dir(original_tests,"/tests"))
                self.assertEqual(len(environment.uploads),before)
                self.assertEqual(original_script.read_text(),content)
            finally:
                self.network._network_phase.reset(token)

    @unittest.skipUnless(os.name=="posix" and Path("/usr/bin/env").is_file(),"real unsetting shell is native Linux only")
    def test_real_shell_removes_image_environment_not_only_empty_merge_values(self):
        command=self.network.scoped_execution_command("/usr/bin/env",self.contract["verifier"],True)
        inherited=Docker()._merge_env({"ALL_PROXY":"http://image.invalid:8080","all_proxy":"http://image.invalid:8080","SSL_CERT_DIR":"/image/ca-dir","Uv_Insecure_Host":"*","npm_config_strict_ssl":"false"})
        inherited["PATH"]="/usr/bin:/bin"
        result=subprocess.run(["/bin/bash","-c",command],env=inherited,capture_output=True,text=True,timeout=5,check=True)
        actual=dict(line.split("=",1) for line in result.stdout.splitlines())
        for key in inherited:
            if self.network.is_network_name(key) or key in self.network._MODEL_NAMES:
                if key not in self.contract["verifier"]["env"]:
                    self.assertNotIn(key,actual)
        self.assertEqual(actual["npm_config_strict_ssl"],"true")
        self.assertEqual(actual["ROUTER_FORGE_NETWORK_STAGE"],"verifier")
        self.assertEqual(actual["KEEP_NATIVE"],"preserved")


if __name__=="__main__":
    unittest.main()
