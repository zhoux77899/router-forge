package executor

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestNativePhaseTLSClientsCannotReuseAgentSkipForVerifier(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("installed Python unavailable")
	}
	server := newUntrustedTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "owned-phase-tls") }))
	defer server.Close()
	for _, customVerifier := range []bool{false, true} {
		root := projectTestDir(t)
		ca := filepath.Join(root, "selected-ca.pem")
		if err := writePrivate(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})); err != nil {
			t.Fatal(err)
		}
		spec := fixtureSpec()
		spec.Snapshot.Network.Agent.TLSMode = domain.NetworkSkipTLS
		spec.Snapshot.Network.Agent.TLSRiskAccepted = true
		spec.Snapshot.Network.Agent.TrustMode = domain.NetworkCustomCA
		spec.Snapshot.Network.Agent.CACertPath = filepath.Join(root, "inactive-missing.pem")
		if customVerifier {
			spec.Snapshot.Network.Verifier.TrustMode = domain.NetworkCustomCA
			spec.Snapshot.Network.Verifier.CACertPath = ca
		}
		sealFixtureSpec(&spec)
		network, err := prepareExecutionNetwork(context.Background(), spec, root, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer network.close()
		contract, _ := json.Marshal(network)
		script := nativeHarborTLS + "\n" + nativeHarborNetwork + "\n" + `import asyncio,json,sys,importlib
contract=json.loads(sys.argv[1])
client_name=sys.argv[2]
target=sys.argv[3]
custom_verifier=sys.argv[4]=='true'
if client_name!='stdlib':
    try: importlib.import_module(client_name)
    except ModuleNotFoundError:
        print('optional-client-not-installed')
        sys.exit(0)
configure_source_network(contract['source'])
_network_contract=contract
if client_name=='stdlib':
    import urllib.request
    def fetch(): return urllib.request.urlopen(target,timeout=3).read().decode()
elif client_name=='httpx':
    import httpx
    client=httpx.Client(timeout=3)
    def fetch(): return client.get(target).text
elif client_name=='requests':
    import requests
    client=requests.Session()
    client.verify=False
    def fetch(): return client.get(target,timeout=3).text
for phase,allowed in (('source',False),('agent',True),('verifier',custom_verifier),('source',False)):
    token=_network_phase.set(phase)
    try:
        try: value=fetch()
        except Exception:
            if allowed: raise
        else:
            if not allowed or value!='owned-phase-tls': raise AssertionError('phase TLS policy was reused or ignored')
    finally: _network_phase.reset(token)
if client_name=='httpx':
    client.close()
    async def asynchronous():
        async with httpx.AsyncClient(timeout=3) as client:
            for phase,allowed in (('agent',True),('verifier',custom_verifier),('source',False)):
                token=_network_phase.set(phase)
                try:
                    try: value=(await client.get(target)).text
                    except Exception:
                        if allowed: raise
                    else:
                        if not allowed or value!='owned-phase-tls': raise AssertionError('async phase TLS policy was reused')
                finally: _network_phase.reset(token)
    asyncio.run(asynchronous())
elif client_name=='requests': client.close()
print('owned-phase-tls-isolation-passed')
`
		filename := filepath.Join(root, "phase-client.py")
		if err := writePrivate(filename, []byte(script)); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"stdlib", "httpx", "requests"} {
			t.Run(name+"-custom-verifier-"+map[bool]string{true: "true", false: "false"}[customVerifier], func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				data, err := runTLSClientFixture(ctx, Command{Path: python, Args: []string{"-I", "-B", filename, string(contract), name, server.URL, map[bool]string{true: "true", false: "false"}[customVerifier]}}, transportEnvironment())
				if err != nil {
					t.Fatalf("real native phase TLS isolation failed: %v\n%s", err, data)
				}
				if strings.Contains(string(data), "optional-client-not-installed") {
					t.Skip("optional installed client unavailable")
				}
				if !strings.Contains(string(data), "owned-phase-tls-isolation-passed") {
					t.Fatal("actual phase TLS assertion was not executed", string(data))
				}
			})
		}
	}
}
