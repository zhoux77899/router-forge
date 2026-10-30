package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func controlNetworkFixture(scope domain.NetworkScope, environmentID string) domain.NetworkProfile {
	profile := domain.NetworkProfile{Name: "explicit network", Scope: scope, EnvironmentID: environmentID, ProxyMode: domain.NetworkDirect, TLSMode: domain.NetworkVerifyTLS, TrustMode: domain.NetworkSystemCA}
	if scope == domain.NetworkEnvironment {
		profile.Mirrors = &domain.NetworkMirrors{APT: domain.NetworkSource{Mode: domain.NetworkNative}, PyPI: domain.NetworkSource{Mode: domain.NetworkNative}, NPM: domain.NetworkSource{Mode: domain.NetworkNative}, Node: domain.NetworkSource{Mode: domain.NetworkNative}, UV: domain.NetworkSource{Mode: domain.NetworkNative}, UVPython: domain.NetworkSource{Mode: domain.NetworkNative}}
	}
	return profile
}

func TestNetworkSettingsAreProtectedAndFirstUseNeverInfersModes(t *testing.T) {
	f := setup(t, false)
	code, body := f.request(t, "GET", "/api/network-profiles", nil, true)
	profiles := result[[]domain.NetworkProfile](t, code, body)
	if len(profiles) != 0 {
		t.Fatal("network profiles were seeded")
	}
	code, _ = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: domain.NetworkProfile{Name: "unset"}}, true)
	if code != 400 {
		t.Fatal("missing network choices accepted", code)
	}
	code, body = f.request(t, "POST", "/api/environments", domain.Environment{Name: "unconfigured host", Type: "local"}, true)
	environment := result[domain.Environment](t, code, body)
	code, _ = f.request(t, "POST", "/api/environments/"+environment.ID+"/inspect", map[string]any{}, true)
	if code != 400 {
		t.Fatal("unselected source network was used for inspect", code)
	}
	code, _ = f.request(t, "GET", "/api/benchmarks/swebench-verified/resolve?environment_id="+environment.ID, nil, true)
	if code != 400 || f.exec.resolveCalls.Load() != 0 {
		t.Fatal("unselected source network was used for a download", code)
	}
}

func TestNetworkCheckUsesExplicitTargetAndNeverMakesInferenceOrFollowsRedirects(t *testing.T) {
	f := setup(t, false)
	seen := 0
	var wrongMethod bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		wrongMethod = r.Method != http.MethodHead
		w.Header().Set("Location", f.provider.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer target.Close()
	code, body := f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: controlNetworkFixture(domain.NetworkDesktop, "")}, true)
	profile := result[domain.NetworkProfile](t, code, body)
	code, body = f.request(t, "POST", "/api/network-profiles/"+profile.ID+"/check", domain.NetworkCheckInput{ProfileRevision: profile.Revision, TargetURL: target.URL}, true)
	check := result[domain.NetworkCheck](t, code, body)
	if seen != 1 || wrongMethod || !check.Reachable || check.HTTPStatus == nil || *check.HTTPStatus != http.StatusTemporaryRedirect || check.TLSVerified != nil || f.calls.Load() != 0 || check.TargetURL != target.URL {
		t.Fatal("check changed method, scope or followed redirect", check)
	}
	code, _ = f.request(t, "POST", "/api/network-profiles/"+profile.ID+"/check", domain.NetworkCheckInput{ProfileRevision: profile.Revision}, true)
	if code != 400 || seen != 1 {
		t.Fatal("missing target was replaced with a default probe")
	}
	code, _ = f.request(t, "POST", "/api/network-profiles/"+profile.ID+"/check", domain.NetworkCheckInput{ProfileRevision: profile.Revision, TargetURL: "https://user:private-password@target.invalid"}, true)
	if code != 400 {
		t.Fatal("credential-bearing check URL accepted")
	}
}

func TestModelNetworkCheckStrictSkipRestoreHasSeparateOperationRisk(t *testing.T) {
	f := setup(t, false)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Error("check was not HEAD")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	code, body := f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: controlNetworkFixture(domain.NetworkDesktop, "")}, true)
	profile := result[domain.NetworkProfile](t, code, body)
	check := func(accepted bool) (int, []byte) {
		return f.request(t, "POST", "/api/network-profiles/"+profile.ID+"/check", domain.NetworkCheckInput{ProfileRevision: profile.Revision, TargetURL: target.URL, ConfirmTLSRisk: accepted}, true)
	}
	code, body = check(false)
	strict := result[domain.NetworkCheck](t, code, body)
	if strict.Reachable || len(strict.Issues) == 0 {
		t.Fatal("self-signed target passed strict check")
	}
	profile.TLSMode, profile.TrustMode, profile.CACertPath, profile.TLSRiskAccepted = domain.NetworkSkipTLS, domain.NetworkCustomCA, "Z:\\missing\\retained.pem", true
	code, _ = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile}, true)
	if code != 400 {
		t.Fatal("public confirmation metadata was accepted as a new risk approval")
	}
	code, body = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile, ConfirmTLSRisk: true}, true)
	profile = result[domain.NetworkProfile](t, code, body)
	code, _ = check(false)
	if code != 400 {
		t.Fatal("saved profile confirmation authorized an unrelated check operation")
	}
	code, body = check(true)
	skipped := result[domain.NetworkCheck](t, code, body)
	if !skipped.Reachable || skipped.TLSVerified == nil || *skipped.TLSVerified || profile.CACertPath != "Z:\\missing\\retained.pem" || f.calls.Load() != 0 {
		t.Fatal("skip/ignored CA/check scope was not honored", skipped)
	}
	profile.TLSMode, profile.TrustMode = domain.NetworkVerifyTLS, domain.NetworkSystemCA
	code, body = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile}, true)
	profile = result[domain.NetworkProfile](t, code, body)
	code, body = check(false)
	restored := result[domain.NetworkCheck](t, code, body)
	if restored.Reachable || profile.TLSRiskAccepted {
		t.Fatal("strict verification was not restored")
	}
}

func TestNetworkCheckReportsOnlyTheSelectedCustomCAActuallyLoaded(t *testing.T) {
	f := setup(t, false)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Error("network check invoked a non-HEAD operation")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw})
	caPath := filepath.Join(t.TempDir(), "selected-ca.pem")
	if err := os.WriteFile(caPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	profile := controlNetworkFixture(domain.NetworkDesktop, "")
	profile.TrustMode, profile.CACertPath = domain.NetworkCustomCA, caPath
	code, body := f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile}, true)
	profile = result[domain.NetworkProfile](t, code, body)
	check := func(risk bool) domain.NetworkCheck {
		code, body := f.request(t, "POST", "/api/network-profiles/"+profile.ID+"/check", domain.NetworkCheckInput{ProfileRevision: profile.Revision, TargetURL: target.URL, ConfirmTLSRisk: risk}, true)
		return result[domain.NetworkCheck](t, code, body)
	}
	strict := check(false)
	digest := sha256.Sum256(ca)
	if !strict.Reachable || strict.TLSVerified == nil || !*strict.TLSVerified || strict.CASHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("custom CA observation did not match the actual loaded trust", strict)
	}
	profile.TLSMode, profile.CACertPath = domain.NetworkSkipTLS, filepath.Join(t.TempDir(), "missing-retained.pem")
	code, body = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile, ConfirmTLSRisk: true}, true)
	profile = result[domain.NetworkProfile](t, code, body)
	skipped := check(true)
	if !skipped.Reachable || skipped.TLSVerified == nil || *skipped.TLSVerified || skipped.CASHA256 != "" || f.calls.Load() != 0 {
		t.Fatal("skipped verification falsely reported a loaded retained CA or invoked inference", skipped)
	}
}

func TestDraftStartApprovalBindsHashAndIndependentRiskScopes(t *testing.T) {
	f := setup(t, true)
	draft := f.draft(t)
	snapshot := draft.Snapshot
	profile := snapshot.Network.Model
	profile.TLSMode, profile.TLSRiskAccepted = domain.NetworkSkipTLS, true
	code, body := f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile, ConfirmTLSRisk: true}, true)
	snapshot.Network.Model = result[domain.NetworkProfile](t, code, body)
	agent := snapshot.Network.Agent
	agent.ID, agent.Revision, agent.CreatedAt = "", "", ""
	agent.Name, agent.TLSMode, agent.TLSRiskAccepted = "explicit agent skip", domain.NetworkSkipTLS, true
	code, body = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: agent, ConfirmTLSRisk: true}, true)
	snapshot.Network.Agent = result[domain.NetworkProfile](t, code, body)
	snapshot.Environment.Networks.AgentProfileID = snapshot.Network.Agent.ID
	snapshot.ConfigHash, _ = snapshot.ApprovalHash()
	if err := snapshot.ValidateSources(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	if _, err := f.store.DB.ExecContext(context.Background(), "UPDATE evaluation_runs SET snapshot_json=? WHERE id=?", string(data), draft.ID); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]domain.StartInput{
		"wrong-hash":             {ConfirmPaid: true, ConfigHash: draft.Snapshot.ConfigHash, ConfirmExecutionTLSRisk: true, ConfirmModelTLSRisk: true},
		"missing-execution-risk": {ConfirmPaid: true, ConfigHash: snapshot.ConfigHash, ConfirmModelTLSRisk: true},
		"missing-model-risk":     {ConfirmPaid: true, ConfigHash: snapshot.ConfigHash, ConfirmExecutionTLSRisk: true},
		"missing-payment":        {ConfigHash: snapshot.ConfigHash, ConfirmExecutionTLSRisk: true, ConfirmModelTLSRisk: true},
	} {
		t.Run(name, func(t *testing.T) {
			code, _ := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", input, true)
			if code < 400 || f.exec.calls.Load() != 0 || f.calls.Load() != 0 {
				t.Fatal("partial or stale approval started a run", code)
			}
		})
	}
	code, body = f.request(t, "POST", "/api/runs/"+draft.ID+"/start", domain.StartInput{ConfirmPaid: true, ConfigHash: snapshot.ConfigHash, ConfirmExecutionTLSRisk: true, ConfirmModelTLSRisk: true}, true)
	result[domain.Run](t, code, body)
	select {
	case <-f.exec.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	if f.calls.Load() != 0 {
		t.Fatal("loopback blocked worker invoked inference")
	}
}

func TestStartSendsOnlyFrozenExecutionCredentialsToPrivateWorker(t *testing.T) {
	f := setup(t, true)
	f.exec.capture = make(chan domain.ExecutionSpec, 1)
	draft := f.draft(t)
	snapshot := draft.Snapshot
	modelUser, modelPassword := "fixture-private-model-user", "fixture-private-model-password"
	model := snapshot.Network.Model
	model.ProxyMode, model.ProxyURL, model.ProxyAuth, model.ProxyTargets, model.RelayMode = domain.NetworkProxy, "http://127.0.0.1:9", domain.NetworkBasicAuth, []string{"https"}, domain.NetworkLoopback
	code, body := f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: model, ProxyUsername: &modelUser, ProxyPassword: &modelPassword}, true)
	snapshot.Network.Model = result[domain.NetworkProfile](t, code, body)
	agentUser, agentPassword := "fixture-private-agent-user", "fixture-private-agent-password"
	agent := snapshot.Network.Agent
	agent.ID, agent.Revision, agent.CreatedAt = "", "", ""
	agent.Name, agent.ProxyMode, agent.ProxyURL, agent.ProxyAuth, agent.ProxyTargets, agent.RelayMode = "frozen agent relay", domain.NetworkProxy, "http://127.0.0.1:9", domain.NetworkBasicAuth, []string{"http", "https"}, domain.NetworkDocker
	code, body = f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: agent, ProxyUsername: &agentUser, ProxyPassword: &agentPassword}, true)
	snapshot.Network.Agent = result[domain.NetworkProfile](t, code, body)
	snapshot.Environment.Networks.AgentProfileID = snapshot.Network.Agent.ID
	snapshot.ConfigHash, _ = snapshot.ApprovalHash()
	if err := snapshot.ValidateSources(); err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(snapshot)
	if bytes.Contains(public, []byte(agentPassword)) || bytes.Contains(public, []byte(modelPassword)) {
		t.Fatal("private credentials entered an approved public snapshot")
	}
	if _, err := f.store.DB.ExecContext(context.Background(), "UPDATE evaluation_runs SET snapshot_json=? WHERE id=?", string(public), draft.ID); err != nil {
		t.Fatal(err)
	}
	code, body = f.request(t, "POST", "/api/runs/"+draft.ID+"/start", domain.StartInput{ConfirmPaid: true, ConfigHash: snapshot.ConfigHash}, true)
	result[domain.Run](t, code, body)
	select {
	case private := <-f.exec.capture:
		if private.Networks == nil || private.Networks.Snapshot.Agent.Revision != snapshot.Network.Agent.Revision {
			t.Fatal("worker received missing or unfrozen network policy")
		}
		credential, err := private.Networks.CredentialFor(private.Networks.Snapshot.Agent)
		if err != nil || credential == nil || credential.Username != agentUser || credential.Password != agentPassword {
			t.Fatal("owned execution credential was lost")
		}
		wire, err := json.Marshal(private)
		if err != nil || !bytes.Contains(wire, []byte(agentPassword)) || bytes.Contains(wire, []byte(modelUser)) || bytes.Contains(wire, []byte(modelPassword)) {
			t.Fatal("private worker channel leaked Desktop model credentials or dropped execution credentials")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private worker spec was not delivered")
	}
	if f.calls.Load() != 0 {
		t.Fatal("blocked fixture invoked inference")
	}
}

func TestProxyCredentialWritesNeverAppearInPublicNetworkResponses(t *testing.T) {
	f := setup(t, false)
	profile := controlNetworkFixture(domain.NetworkDesktop, "")
	profile.ProxyMode, profile.ProxyURL, profile.ProxyAuth, profile.ProxyTargets, profile.RelayMode = domain.NetworkProxy, "https://proxy.invalid:8443", domain.NetworkBasicAuth, []string{"https"}, domain.NetworkLoopback
	username, password := "private-proxy-user", "private-proxy-password"
	code, body := f.request(t, "POST", "/api/network-profiles", domain.NetworkProfileInput{Profile: profile, ProxyUsername: &username, ProxyPassword: &password}, true)
	profile = result[domain.NetworkProfile](t, code, body)
	code, body = f.request(t, "GET", "/api/network-profiles", nil, true)
	result[[]domain.NetworkProfile](t, code, body)
	if bytes.Contains(body, []byte(username)) || bytes.Contains(body, []byte(password)) || strings.Contains(string(body), "proxy_username") || strings.Contains(string(body), "proxy_password") || !profile.HasCredentials {
		t.Fatal("write-only proxy credential appeared in public response")
	}
}
