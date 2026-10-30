package executor

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestInstalledHarborOfflinePhaseTLSClients(t *testing.T) {
	if os.Getenv("ROUTER_FORGE_OFFLINE_NATIVE_NETWORK") != "1" {
		t.Skip("opt-in installed Harbor loopback-only phase TLS client check")
	}
	const script = `import asyncio,json,socket,ssl,sys,threading
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
root=Path(sys.argv[1])
real_connect=socket.socket.connect
def loopback_only(self,address):
    require_network(isinstance(address,tuple) and address[0] in ('127.0.0.1','::1'))
    return real_connect(self,address)
socket.socket.connect=loopback_only
socket.socket.connect_ex=lambda *a,**kw: (_ for _ in ()).throw(RuntimeError('owned loopback only'))
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'owned-native-phase-tls')
    def log_message(self,*args): pass
server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
server.daemon_threads=True
server_context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
server_context.load_cert_chain(root/'certificate.pem',root/'key.pem')
server.socket=server_context.wrap_socket(server.socket,server_side=True)
thread=threading.Thread(target=server.serve_forever,daemon=True)
thread.start()
target='https://127.0.0.1:'+str(server.server_port)+'/owned'
contract=json.loads((root/'network-contract.json').read_text())
validate_phase_contract(contract)
configure_source_network(contract['source'])
_network_contract=contract
import httpx,requests,urllib.request
phases=[('source',False),('agent',True),('verifier',sys.argv[2]=='custom'),('source',False)]
checked=[]
def check(name,call):
    for phase,expected in phases:
        token=_network_phase.set(phase)
        try:
            try: success=call()==b'owned-native-phase-tls'
            except Exception: success=False
            assert success is expected,(name,phase,expected,success)
            checked.append([name,phase,success])
        finally: _network_phase.reset(token)
try:
    with httpx.Client(timeout=3) as client:
        check('httpx-reused',lambda:client.get(target).content)
    with requests.Session() as client:
        check('requests-reused',lambda:client.get(target,timeout=3).content)
    check('stdlib',lambda:urllib.request.urlopen(target,timeout=3).read())
    async def check_async():
        async with httpx.AsyncClient(timeout=3) as client:
            for phase,expected in phases:
                token=_network_phase.set(phase)
                try:
                    try: success=(await client.get(target)).content==b'owned-native-phase-tls'
                    except Exception: success=False
                    assert success is expected,('httpx-async-reused',phase,expected,success)
                    checked.append(['httpx-async-reused',phase,success])
                finally: _network_phase.reset(token)
    asyncio.run(check_async())
finally:
    server.shutdown()
    server.server_close()
    thread.join(timeout=3)
print(json.dumps({'loopback_only':True,'native_installed_clients':True,'checks':len(checked),'verifier_custom_ca':sys.argv[2]=='custom','trials_started':0,'model_calls':0}))`
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "strict-verifier", true: "custom-verifier"}[custom], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			root := projectTestDir(t)
			nativeRoot := root
			mode := "strict"
			if custom {
				mode = "custom"
			}
			var command Command
			if runtime.GOOS == "windows" {
				wsl, err := exec.LookPath("wsl.exe")
				if err != nil {
					t.Fatal("installed native offline fixture requires WSL", err)
				}
				data, _, err := runCommand(ctx, Command{Path: wsl, Args: []string{"--distribution", "Ubuntu", "--exec", "wslpath", "-a", root}}, transportEnvironment(), "", nil, 8192)
				if err != nil {
					t.Fatal(err)
				}
				nativeRoot = strings.TrimSpace(string(data))
				launcher := `h=$(command -v harbor); test -n "$h" || h="$HOME/.local/bin/harbor"; test -f "$h" || exit 78; IFS= read -r first < "$h"; p=${first#\#!}; exec "$p" -I -B - ` + shellQuote(nativeRoot) + " " + shellQuote(mode)
				command = Command{Path: wsl, Args: []string{"--distribution", "Ubuntu", "--exec", "/bin/sh", "-c", launcher}}
			} else {
				python, err := nativeHarborPython(domain.Environment{}, "")
				if err != nil {
					t.Fatal(err)
				}
				command = Command{Path: python, Args: []string{"-I", "-B", "-", nativeRoot, mode}}
			}
			server := newUntrustedTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			defer server.Close()
			certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			key, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"certificate.pem": certificate, "key.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})} {
				if err := writePrivate(filepath.Join(root, name), data); err != nil {
					t.Fatal(err)
				}
			}
			spec := fixtureSpec()
			spec.Snapshot.Network.Agent.TLSMode, spec.Snapshot.Network.Agent.TLSRiskAccepted = domain.NetworkSkipTLS, true
			spec.Snapshot.Network.Agent.TrustMode, spec.Snapshot.Network.Agent.CACertPath = domain.NetworkCustomCA, "/missing/inactive.pem"
			if custom {
				spec.Snapshot.Network.Verifier.TrustMode, spec.Snapshot.Network.Verifier.CACertPath = domain.NetworkCustomCA, nativeRoot+"/certificate.pem"
				if err := privateDir(filepath.Join(root, "network", "verifier")); err != nil {
					t.Fatal(err)
				}
				if err := writePrivate(filepath.Join(root, "network", "verifier", "ca.pem"), certificate); err != nil {
					t.Fatal(err)
				}
			}
			sealFixtureSpec(&spec)
			network, err := staticExecutionNetwork(spec, nativeRoot, "")
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(network)
			if err != nil {
				t.Fatal(err)
			}
			if err := writePrivate(filepath.Join(root, "network-contract.json"), data); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(command.Path, command.Args...)
			cmd.Env = transportEnvironment()
			cmd.Stdin = strings.NewReader(nativeHarborTLS + "\n" + nativeHarborNetwork + "\n" + script)
			output := &boundedBuffer{limit: 64 << 10}
			cmd.Stdout, cmd.Stderr = output, output
			process, err := startOwned(ctx, cmd)
			if err != nil {
				t.Fatal(err)
			}
			err = process.wait()
			data, truncated := output.bytes()
			if err != nil || truncated || !strings.Contains(string(data), `"checks": 16`) {
				t.Fatalf("installed native phase TLS fixture failed: %v\n%s", err, data)
			}
			t.Log(string(data))
		})
	}
}
