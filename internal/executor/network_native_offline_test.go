package executor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

// This opt-in check imports only installed native APIs. It cannot open a network
// connection, create a container, install a package, or start a Harbor trial.
func TestInstalledHarborOfflineNetworkAPI(t *testing.T) {
	if os.Getenv("ROUTER_FORGE_OFFLINE_NATIVE_NETWORK") != "1" {
		t.Skip("opt-in installed Harbor offline network API check")
	}
	const code = `import inspect,json,socket,sys,importlib,subprocess
from uuid import uuid4
sys.dont_write_bytecode=True
def blocked(*args,**kwargs): raise RuntimeError('offline fixture prohibits runtime/network')
socket.socket.connect=blocked
socket.socket.connect_ex=blocked
socket.create_connection=blocked
real_popen=subprocess.Popen
subprocess.Popen=blocked
root=Path(sys.argv[1])
raw=json.loads((root/'job-config.json').read_text())
contract=json.loads((root/'network-contract.json').read_text())
validate_phase_contract(contract,raw)
docker,verifier,agent=native_network_api('opencode')
from harbor.models.job.config import JobConfig
from harbor.job_plan import JobPlan
config=JobConfig.model_validate(raw)
assert_honored(raw,native_view(config))
trials=JobPlan.build_trial_configs(config,config.tasks,job_id=uuid4())
assert len(trials)==1
trial=native_view(trials[0])
for field in ('environment','verifier'):
    assert_honored(raw[field],trial[field])
assert_honored(raw['agents'][0],trial['agent'])
assert_honored(raw['tasks'][0],trial['task'])
assert raw['environment']['env']=={}
assert raw['agents'][0]['env']['ROUTER_FORGE_SKIP_TLS_VERIFICATION']=='1'
assert 'ROUTER_FORGE_SKIP_TLS_VERIFICATION' not in raw['verifier']['env']
install_phase_network(contract,'opencode')
assert _network_contract['verifier']['profile']['tls_mode']=='verify'
assert inspect.signature(docker._compose_exec).parameters.get('env') is not None
guards={'curl':[['--insecure','--version'],['-kfsS','--version'],['--proxy','http://unselected.invalid','--version'],['--config','/shared/curlrc','--version']],
        'wget':[['--no-check-certificate','--help'],['--execute','check_certificate=off','--help'],['-qecheck_certificate=off','--help']],
        'git':[['-c','http.sslVerify=false','--version'],['-chttp.proxy=http://unselected.invalid','--version'],['--config-env=http.proxy=UNSELECTED','--version']]}
checked=0
for tool,options in guards.items():
    for arguments in options:
        process=real_popen(['/bin/sh',str(root/'network'/'verifier'/'bin'/tool),*arguments],cwd=root,env={'PATH':'/usr/bin:/bin','HOME':str(root)},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        stdout,stderr=process.communicate(timeout=5)
        assert process.returncode==78 and b'explicit network policy rejects' in stderr
        checked+=1
for tool in ('curl','git'):
    process=real_popen(['/bin/sh',str(root/'network'/'verifier'/'bin'/tool),'--version'],cwd=root,env={'PATH':'/usr/bin:/bin','HOME':str(root)},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    stdout,stderr=process.communicate(timeout=5)
    assert process.returncode==0
import unittest
fixture_spec=importlib.util.spec_from_file_location('owned_phase_tests',root/'harbor_network_test.py')
fixture=importlib.util.module_from_spec(fixture_spec)
fixture_spec.loader.exec_module(fixture)
def owned_shell(args,*a,**kw):
    if isinstance(args,list) and len(args)==3 and args[:2]==['/bin/bash','-c'] and args[2].splitlines()[-1]=='exec bash -c /usr/bin/env':
        return real_popen(args,*a,**kw)
    return blocked(args,*a,**kw)
subprocess.Popen=owned_shell
result=unittest.TextTestRunner(stream=sys.stderr).run(unittest.defaultTestLoader.loadTestsFromModule(fixture))
assert result.wasSuccessful() and not result.skipped
print(json.dumps({'offline':True,'native_job_schema':True,'one_config_expansion':len(trials),'phase_lifecycle_hooks':True,'verifier_strict':True,'owned_client_guards':checked,'linux_phase_tests':result.testsRun,'connections':0,'trials_started':0}))`
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := projectTestDir(t)
	nativeRoot := root
	var command Command
	if runtime.GOOS == "windows" {
		wsl, err := exec.LookPath("wsl.exe")
		if err != nil {
			t.Fatal("WSL is unavailable for the explicit offline API check")
		}
		pathData, _, err := runCommand(ctx, Command{Path: wsl, Args: []string{"--distribution", "Ubuntu", "--exec", "wslpath", "-a", root}}, transportEnvironment(), "", nil, 8192)
		if err != nil {
			t.Fatal("could not map owned offline fixture root", err)
		}
		nativeRoot = strings.TrimSpace(string(pathData))
		script := `h=$(command -v harbor); test -n "$h" || h="$HOME/.local/bin/harbor"; test -f "$h" || exit 78; IFS= read -r first < "$h"; p=${first#\#!}; exec "$p" -I -B - ` + shellQuote(nativeRoot)
		command = Command{Path: wsl, Args: []string{"--distribution", "Ubuntu", "--exec", "/bin/sh", "-c", script}}
	} else {
		python, err := nativeHarborPython(domain.Environment{}, "")
		if err != nil {
			t.Fatal(err)
		}
		command = Command{Path: python, Args: []string{"-I", "-B", "-", nativeRoot}}
	}
	spec := fixtureSpec()
	spec.Snapshot.Network.Agent.TLSMode = domain.NetworkSkipTLS
	spec.Snapshot.Network.Agent.TLSRiskAccepted = true
	spec.Snapshot.Network.Agent.TrustMode = domain.NetworkCustomCA
	spec.Snapshot.Network.Agent.CACertPath = "/missing/inactive-ca.pem"
	sealFixtureSpec(&spec)
	cfg, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", nativeRoot+"/jobs", nativeRoot+"/compose.json")
	if err != nil {
		t.Fatal(err)
	}
	network, err := staticExecutionNetwork(spec, nativeRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	ownedVerifier, err := stageNetworkConfig(spec.Snapshot.Network.Verifier, "verifier", root, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeNetworkClientWrappers(ownedVerifier, true); err != nil {
		t.Fatal(err)
	}
	phaseTests, err := os.ReadFile("harbor_network_test.py")
	if err != nil {
		t.Fatal(err)
	}
	jobBytes, _ := json.Marshal(cfg)
	networkBytes, _ := json.Marshal(network)
	for name, data := range map[string][]byte{"job-config.json": jobBytes, "network-contract.json": networkBytes, "compose.json": []byte(`{"services":{"main":{}}}`), "harbor_network.py": []byte(nativeHarborNetwork), "harbor_network_test.py": phaseTests} {
		if err := writePrivate(filepath.Join(root, name), data); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Stdin = strings.NewReader(nativeHarborTLS + "\n" + nativeHarborNetwork + "\n" + strings.Replace(nativeHarborBridgeBody, "if __name__ == \"__main__\":", "if False:", 1) + "\n" + code)
	cmd.Env = transportEnvironment()
	output := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = output, output
	process, err := startOwned(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	err = process.wait()
	data, truncated := output.bytes()
	if err != nil || truncated {
		t.Fatalf("offline installed Harbor API check failed: %v\n%s", err, data)
	}
	if !strings.Contains(string(data), `"trials_started": 0`) || !strings.Contains(string(data), `"native_job_schema": true`) {
		t.Fatal("actual offline native schema assertions did not run", string(data))
	}
	t.Log(string(data))
}
