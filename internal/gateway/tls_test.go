package gateway_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestModelUpstreamTLSIsNotDisabledByRuntimeOptIn(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"model":"fixed-model","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer upstream.Close()
	for name, value := range map[string]string{"ROUTER_FORGE_SKIP_TLS_VERIFICATION": "1", "NODE_TLS_REJECT_UNAUTHORIZED": "0", "GIT_SSL_NO_VERIFY": "true", "npm_config_strict_ssl": "false"} {
		t.Setenv(name, value)
	}
	original := http.DefaultTransport
	var sharedTLS *tls.Config
	var sharedSkip bool
	if transport, ok := original.(*http.Transport); ok {
		sharedTLS = transport.TLSClientConfig
		if sharedTLS != nil {
			sharedSkip = sharedTLS.InsecureSkipVerify
		}
	}
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) {}, func(lease *gateway.Lease) {
		lease.Provider.BaseURL = upstream.URL + "/v1"
	})
	response := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","messages":[{"role":"user","content":"fixture"}]}`)
	if response.Code != http.StatusBadGateway || calls.Load() != 0 {
		t.Fatal("runtime TLS opt-in or a shared insecure transport weakened model HTTPS", response.Code, calls.Load())
	}
	if records := ledger.records(); len(records) != 1 || records[0].State != "failed" {
		t.Fatal("model TLS rejection was not durably attributed", records)
	}
	if http.DefaultTransport != original {
		t.Fatal("model isolation replaced the shared transport")
	}
	if transport, ok := original.(*http.Transport); ok && (transport.TLSClientConfig != sharedTLS || sharedTLS != nil && sharedTLS.InsecureSkipVerify != sharedSkip) {
		t.Fatal("model isolation modified shared TLS state")
	}
}

func TestModelNetworkIgnoresAmbientProxyAndTLSSettings(t *testing.T) {
	var modelCalls, ambientProxyCalls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelCalls.Add(1)
		_, _ = io.WriteString(w, `{"model":"fixed-model","choices":[]}`)
	}))
	t.Cleanup(upstream.Close)
	ambientProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ambientProxyCalls.Add(1)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	t.Cleanup(ambientProxy.Close)
	ambientCA := gatewayCAFile(t, upstream)
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, ambientProxy.URL)
	}
	for _, name := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(name, "*")
	}
	for _, name := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO", "npm_config_cafile"} {
		t.Setenv(name, ambientCA)
	}
	t.Setenv("SSL_CERT_DIR", ambientCA+".not-a-directory")
	g, _, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model") }, func(l *gateway.Lease) {
		l.Provider.BaseURL = upstream.URL
	})
	if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusBadGateway || modelCalls.Load() != 0 {
		t.Fatal("ambient CA configuration weakened an explicit strict model profile")
	}
	var explicitProxyCalls atomic.Int64
	proxy := modelProxy(t, upstream, false, func(*http.Request) { explicitProxyCalls.Add(1) })
	profile, credential := authenticatedNetwork(proxy.URL, "https")
	profile.TrustMode, profile.CACertPath = domain.NetworkCustomCA, ambientCA
	proxied := lease
	proxied.Token, proxied.Network, proxied.ProxyCredential = "explicit-proxy-lease", profile, credential
	proxied.Provider.BaseURL = "https://" + strings.Replace(upstream.Certificate().DNSNames[0], "*.", "model.", 1)
	if _, err := g.Register(proxied); err != nil {
		t.Fatal(err)
	}
	if response := request(t, g, proxied.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
		t.Fatal("ambient NO_PROXY or proxy settings overrode an explicit model proxy", response.Code)
	}
	if ambientProxyCalls.Load() != 0 || explicitProxyCalls.Load() != 1 || modelCalls.Load() != 1 {
		t.Fatal("model requests inherited ambient proxy routing")
	}
}

func TestModelNetworkStrictSkipAndStrictRestoration(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"model":"fixed-model","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	t.Cleanup(upstream.Close)
	g, ledger, strict := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model") }, func(l *gateway.Lease) {
		l.Provider.BaseURL = upstream.URL
	})
	if response := request(t, g, strict.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusBadGateway || calls.Load() != 0 {
		t.Fatal("strict self-signed model TLS was accepted")
	}
	profile := desktopNetwork()
	profile.Revision, profile.TLSMode, profile.TLSRiskAccepted = "network-revision-2", domain.NetworkSkipTLS, true
	profile.TrustMode = domain.NetworkCustomCA
	path, err := filepath.Abs(".gateway-preserved-unread-" + domain.NewID("ca") + ".pem")
	if err != nil {
		t.Fatal(err)
	}
	profile.CACertPath = path
	skip := strict
	skip.Token, skip.Network = "confirmed-model-skip", profile
	unconfirmed := skip
	unconfirmed.Token = "unconfirmed-model-skip"
	unconfirmedProfile := *profile
	unconfirmedProfile.TLSRiskAccepted = false
	unconfirmed.Network = &unconfirmedProfile
	if _, err := g.Register(unconfirmed); err == nil {
		t.Fatal("model TLS skip was accepted without independent profile risk metadata")
	}
	if _, err := g.Register(skip); err != nil || calls.Load() != 0 || profile.CACertPath != path {
		t.Fatal("confirmed skip read, removed, or rejected its preserved invalid CA path", err)
	}
	if response := request(t, g, skip.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
		t.Fatal("confirmed model TLS skip did not work", response.Code)
	}
	profile.TLSMode, profile.TLSRiskAccepted = domain.NetworkVerifyTLS, false
	if response := request(t, g, skip.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
		t.Fatal("caller mutation changed a frozen model TLS policy", response.Code)
	}
	restored := strict
	restored.Token, restored.Network = "restored-model-strict", desktopNetwork()
	restored.Network.Revision = "network-revision-3"
	if _, err := g.Register(restored); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{strict.Token, restored.Token} {
		if response := request(t, g, token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusBadGateway {
			t.Fatal("a skipped lease weakened an independent strict model lease", response.Code)
		}
	}
	if calls.Load() != 2 || len(ledger.records()) != 5 {
		t.Fatal("TLS lease checks performed hidden model calls or lost accounting")
	}
	for _, row := range ledger.records() {
		if row.CostAmount != nil || row.Currency != nil {
			t.Fatal("unknown price or currency was fabricated by a TLS policy change")
		}
	}
}

func TestModelNetworkHTTPSProxyTrustIsExplicitAndLeaseScoped(t *testing.T) {
	var modelCalls, proxyCalls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" || r.ProtoMajor != 1 {
			t.Error("HTTPS tunnel changed model authentication or native HTTP/1")
		}
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	t.Cleanup(upstream.Close)
	proxy := modelProxy(t, upstream, true, func(r *http.Request) {
		proxyCalls.Add(1)
		if r.Method != http.MethodConnect || !strings.HasPrefix(r.Header.Get("Proxy-Authorization"), "Basic ") || r.Header.Get("Authorization") != "" {
			t.Error("HTTPS proxy authentication escaped its CONNECT boundary")
		}
	})
	profile, credential := authenticatedNetwork(proxy.URL, "https")
	g, ledger, strict := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model") }, func(l *gateway.Lease) {
		l.Provider.BaseURL = "https://" + strings.Replace(upstream.Certificate().DNSNames[0], "*.", "model.", 1)
		l.Network, l.ProxyCredential = profile, credential
	})
	if response := request(t, g, strict.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusBadGateway || proxyCalls.Load() != 0 || modelCalls.Load() != 0 {
		t.Fatal("strict model profile accepted a self-signed HTTPS proxy")
	}
	custom := strict
	custom.Token = "explicit-https-proxy-ca"
	custom.Network, custom.ProxyCredential = authenticatedNetwork(proxy.URL, "https")
	custom.Network.Revision, custom.Network.TrustMode = "network-revision-2", domain.NetworkCustomCA
	custom.Network.CACertPath = gatewayCAFile(t, upstream, proxy)
	if _, err := g.Register(custom); err != nil {
		t.Fatal(err)
	}
	skip := strict
	skip.Token = "explicit-https-proxy-skip"
	skip.Network, skip.ProxyCredential = authenticatedNetwork(proxy.URL, "https")
	skip.Network.Revision, skip.Network.TLSMode, skip.Network.TLSRiskAccepted = "network-revision-3", domain.NetworkSkipTLS, true
	skip.Network.TrustMode, skip.Network.CACertPath = domain.NetworkCustomCA, custom.Network.CACertPath+".preserved-invalid"
	if _, err := g.Register(skip); err != nil || proxyCalls.Load() != 0 || modelCalls.Load() != 0 {
		t.Fatal("HTTPS proxy registration performed a network call or read a skipped CA", err)
	}
	for _, token := range []string{custom.Token, skip.Token} {
		if response := request(t, g, token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
			t.Fatal("explicit HTTPS proxy trust did not reach the loopback model", response.Code)
		}
	}
	if response := request(t, g, strict.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusBadGateway || proxyCalls.Load() != 2 || modelCalls.Load() != 2 || len(ledger.records()) != 4 {
		t.Fatal("private HTTPS proxy trust weakened another lease or replayed a model call")
	}
}

func TestModelNetworkCustomCAIsPrivateAndFrozen(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	t.Cleanup(upstream.Close)
	path := gatewayCAFile(t, upstream)
	profile := desktopNetwork()
	profile.TrustMode, profile.CACertPath = domain.NetworkCustomCA, path
	g, _, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model") }, func(l *gateway.Lease) {
		l.Provider.BaseURL, l.Network = upstream.URL, profile
	})
	if calls.Load() != 0 {
		t.Fatal("loading a custom CA performed a model call")
	}
	if err := os.WriteFile(path, []byte("preserved invalid CA"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
			t.Fatal("a caller's changed CA file altered a frozen custom-trust lease", response.Code)
		}
	}
	invalid := lease
	invalid.Token = "invalid-custom-ca"
	if _, err := g.Register(invalid); err == nil || strings.Contains(err.Error(), path) {
		t.Fatal("strict custom trust accepted invalid PEM or leaked a host CA path")
	}
	strict := lease
	strict.Token, strict.Network = "strict-after-custom-ca", desktopNetwork()
	if _, err := g.Register(strict); err != nil {
		t.Fatal(err)
	}
	if response := request(t, g, strict.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusBadGateway || calls.Load() != 2 {
		t.Fatal("custom trust leaked into an independent system-trust model lease")
	}
}

func TestModelNetworkSkipCannotBypassAgentObservation(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	t.Cleanup(upstream.Close)
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model") }, func(l *gateway.Lease) {
		l.Provider.BaseURL, l.RequireAgentObservation = upstream.URL, true
		l.Network.TLSMode, l.Network.TLSRiskAccepted = domain.NetworkSkipTLS, true
	})
	if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusConflict || calls.Load() != 0 {
		t.Fatal("model TLS risk acceptance bypassed the observed-agent admission gate")
	}
	if err := g.AdmitObservedAgent(lease.Token, lease.RunID, lease.TaskID, lease.AttemptID); err != nil {
		t.Fatal(err)
	}
	if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK || calls.Load() != 1 || len(ledger.records()) != 2 {
		t.Fatal("exact observed-agent admission changed with explicit TLS skip")
	}
}
