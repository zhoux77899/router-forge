package gateway_test

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func desktopNetwork() *domain.NetworkProfile {
	return &domain.NetworkProfile{
		ID: "desktop-network", Revision: "network-revision-1", Name: "Fixture Desktop network",
		Scope: domain.NetworkDesktop, ProxyMode: domain.NetworkDirect,
		TLSMode: domain.NetworkVerifyTLS, TrustMode: domain.NetworkSystemCA,
		CreatedAt: "2026-10-29T00:00:00Z",
	}
}

func gatewayFile(t *testing.T, data []byte) string {
	t.Helper()
	path, err := filepath.Abs(".gateway-fixture-" + domain.NewID("ca") + ".pem")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	})
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("could not write the loopback CA fixture")
	}
	return path
}

func gatewayCAFile(t *testing.T, upstream *httptest.Server, additional ...*httptest.Server) string {
	t.Helper()
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})
	for _, server := range additional {
		data = append(data, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})...)
	}
	return gatewayFile(t, data)
}

func authenticatedNetwork(proxyURL string, targets ...string) (*domain.NetworkProfile, *domain.ProxyCredential) {
	profile := desktopNetwork()
	profile.ProxyMode, profile.ProxyURL, profile.ProxyTargets = domain.NetworkProxy, proxyURL, targets
	profile.ProxyAuth, profile.RelayMode = domain.NetworkBasicAuth, domain.NetworkLoopback
	profile.HasCredentials, profile.CredentialRevision = true, "credential-revision-1"
	return profile, &domain.ProxyCredential{Username: "proxy-private-user", Password: "proxy-private-password"}
}

func TestModelNetworkRegistrationRequiresExplicitSavedDesktopPolicy(t *testing.T) {
	var calls atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	changes := []struct {
		name string
		edit func(*gateway.Lease)
	}{
		{"missing profile", func(l *gateway.Lease) { l.Network = nil }},
		{"missing route", func(l *gateway.Lease) { l.Network.ProxyMode = "" }},
		{"missing TLS", func(l *gateway.Lease) { l.Network.TLSMode = "" }},
		{"missing trust", func(l *gateway.Lease) { l.Network.TrustMode = "" }},
		{"missing identity", func(l *gateway.Lease) { l.Network.ID = "" }},
		{"missing revision", func(l *gateway.Lease) { l.Network.Revision = "" }},
		{"missing save time", func(l *gateway.Lease) { l.Network.CreatedAt = "" }},
		{"environment borrowing", func(l *gateway.Lease) {
			l.Network.Scope, l.Network.EnvironmentID = domain.NetworkEnvironment, "execution-host"
			l.Network.Mirrors = &domain.NetworkMirrors{
				APT: domain.NetworkSource{Mode: domain.NetworkNative}, PyPI: domain.NetworkSource{Mode: domain.NetworkNative},
				NPM: domain.NetworkSource{Mode: domain.NetworkNative}, Node: domain.NetworkSource{Mode: domain.NetworkNative},
				UV: domain.NetworkSource{Mode: domain.NetworkNative}, UVPython: domain.NetworkSource{Mode: domain.NetworkNative},
			}
			if err := l.Network.ValidateSaved(); err != nil {
				t.Fatal("environment fixture itself must be valid", err)
			}
		}},
		{"Desktop with environment path", func(l *gateway.Lease) { l.Network.EnvironmentID = "execution-host" }},
		{"unconfirmed model skip", func(l *gateway.Lease) { l.Network.TLSMode = domain.NetworkSkipTLS }},
		{"missing custom CA", func(l *gateway.Lease) { l.Network.TrustMode = domain.NetworkCustomCA }},
		{"Linux mirrors on Desktop", func(l *gateway.Lease) { l.Network.Mirrors = &domain.NetworkMirrors{} }},
		{"direct with proxy", func(l *gateway.Lease) { l.Network.ProxyURL = "http://127.0.0.1:1" }},
		{"direct with credential", func(l *gateway.Lease) {
			l.ProxyCredential = &domain.ProxyCredential{Username: "proxy-private-user", Password: "proxy-private-password"}
		}},
		{"unused credential reference", func(l *gateway.Lease) {
			l.Network.HasCredentials, l.Network.CredentialRevision = true, "credential-revision-1"
		}},
		{"proxy missing auth mode", func(l *gateway.Lease) {
			l.Network.ProxyMode, l.Network.ProxyURL, l.Network.ProxyTargets = domain.NetworkProxy, "http://127.0.0.1:1", []string{"all"}
		}},
		{"proxy missing targets", func(l *gateway.Lease) { l.Network, l.ProxyCredential = authenticatedNetwork("http://127.0.0.1:1") }},
		{"proxy duplicate targets", func(l *gateway.Lease) {
			l.Network, l.ProxyCredential = authenticatedNetwork("http://127.0.0.1:1", "http", "http")
		}},
		{"proxy invalid targets", func(l *gateway.Lease) {
			l.Network, l.ProxyCredential = authenticatedNetwork("http://127.0.0.1:1", "ftp")
		}},
		{"proxy credential in URL", func(l *gateway.Lease) {
			l.Network, l.ProxyCredential = authenticatedNetwork("http://proxy-private-user:proxy-private-password@127.0.0.1:1", "all")
		}},
		{"proxy missing credential", func(l *gateway.Lease) { l.Network, _ = authenticatedNetwork("http://127.0.0.1:1", "all") }},
		{"proxy invalid credential", func(l *gateway.Lease) {
			l.Network, l.ProxyCredential = authenticatedNetwork("http://127.0.0.1:1", "all")
			l.ProxyCredential.Username += ":invalid"
		}},
		{"proxy missing credential revision", func(l *gateway.Lease) {
			l.Network, l.ProxyCredential = authenticatedNetwork("http://127.0.0.1:1", "all")
			l.Network.CredentialRevision = ""
		}},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			candidate := lease
			candidate.Token, candidate.Network = "rejected-lease", desktopNetwork()
			change.edit(&candidate)
			if _, err := g.Register(candidate); err == nil {
				t.Fatal("incomplete model network policy was admitted")
			} else if strings.Contains(err.Error(), "proxy-private-user") || strings.Contains(err.Error(), "proxy-private-password") {
				t.Fatal("registration diagnostic exposed proxy credentials")
			}
		})
	}
	if calls.Load() != 0 || len(ledger.records()) != 0 {
		t.Fatal("network registration performed a model call or created a request row")
	}
}

// Synthetic proxy targets are remapped only to this fixed loopback origin; the
// test proxy never resolves or dials a host supplied by an HTTP request.
func modelProxy(t *testing.T, upstream *httptest.Server, secure bool, observe func(*http.Request)) *httptest.Server {
	t.Helper()
	origin, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe(r)
		if r.Method == http.MethodConnect {
			remote, err := net.DialTimeout("tcp", upstream.Listener.Addr().String(), time.Second)
			if err != nil {
				http.Error(w, "loopback origin unavailable", http.StatusBadGateway)
				return
			}
			defer remote.Close()
			connection, buffered, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error("loopback proxy could not accept a tunnel")
				return
			}
			defer connection.Close()
			_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			if err := buffered.Flush(); err != nil {
				return
			}
			finished := make(chan struct{})
			go func() {
				_, _ = io.Copy(remote, buffered)
				_ = remote.Close()
				close(finished)
			}()
			_, _ = io.Copy(connection, remote)
			_ = connection.Close()
			<-finished
			return
		}
		target := *origin
		target.Path, target.RawQuery = r.URL.Path, r.URL.RawQuery
		outgoing, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
		if err != nil {
			t.Error("invalid loopback proxy request")
			return
		}
		outgoing.Header = r.Header.Clone()
		outgoing.Header.Del("Proxy-Authorization")
		outgoing.Header.Del("Proxy-Connection")
		response, err := upstream.Client().Do(outgoing)
		if err != nil {
			http.Error(w, "loopback origin unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
	var proxy *httptest.Server
	if secure {
		proxy = httptest.NewTLSServer(handler)
	} else {
		proxy = httptest.NewServer(handler)
	}
	t.Cleanup(proxy.Close)
	return proxy
}

func TestModelNetworkProxyTargetsAndNoProxy(t *testing.T) {
	cases := []struct {
		name         string
		targets      []string
		TLS, proxied bool
		noProxy      string
	}{
		{"HTTP", []string{"http"}, false, true, ""},
		{"HTTPS", []string{"https"}, true, true, ""},
		{"ALL HTTP", []string{"all"}, false, true, ""},
		{"ALL HTTPS", []string{"all"}, true, true, ""},
		{"HTTP excluded", []string{"https"}, false, false, ""},
		{"HTTPS excluded", []string{"http"}, true, false, ""},
		{"NO_PROXY wildcard", []string{"all"}, false, false, "*"},
		{"NO_PROXY CIDR", []string{"all"}, true, false, "127.0.0.0/8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var originCalls, proxyCalls atomic.Int64
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originCalls.Add(1)
				if r.ProtoMajor != 1 || r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Authorization") != "Bearer desktop-upstream-secret" {
					t.Error("model protocol or private authentication boundary changed")
				}
				_, _ = io.WriteString(w, `{"model":"fixed-model","choices":[]}`)
			})
			var upstream *httptest.Server
			if tc.TLS {
				upstream = httptest.NewTLSServer(handler)
			} else {
				upstream = httptest.NewServer(handler)
			}
			t.Cleanup(upstream.Close)
			credential := &domain.ProxyCredential{Username: "proxy-private-user", Password: "proxy-private-password"}
			wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credential.Username+":"+credential.Password))
			proxy := modelProxy(t, upstream, false, func(r *http.Request) {
				proxyCalls.Add(1)
				if r.Header.Get("Proxy-Authorization") != wantAuth || tc.TLS && r.Header.Get("Authorization") != "" {
					t.Error("Basic authentication did not stay on the explicit upstream proxy connection")
				}
			})
			profile, _ := authenticatedNetwork(proxy.URL, tc.targets...)
			profile.NoProxy = tc.noProxy
			base := upstream.URL
			if tc.proxied {
				base = "http://model.fixture.invalid"
				if tc.TLS {
					if len(upstream.Certificate().DNSNames) == 0 {
						t.Fatal("loopback certificate needs a DNS name for the remapped proxy target")
					}
					base = "https://" + strings.Replace(upstream.Certificate().DNSNames[0], "*.", "model.", 1)
				}
			}
			if tc.TLS {
				profile.TrustMode, profile.CACertPath = domain.NetworkCustomCA, gatewayCAFile(t, upstream)
			}
			g, ledger, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong loopback model") }, func(l *gateway.Lease) {
				l.Provider.BaseURL, l.Network, l.ProxyCredential = base+"/v1", profile, credential
			})
			if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
				t.Fatal("explicit model route failed", response.Code, response.Body.String())
			}
			wantProxy := int64(0)
			if tc.proxied {
				wantProxy = 1
			}
			if proxyCalls.Load() != wantProxy || originCalls.Load() != 1 || len(ledger.records()) != 1 {
				t.Fatal("explicit model route changed or replayed", proxyCalls.Load(), originCalls.Load())
			}
		})
	}
}

func TestModelNetworkUnauthenticatedProxyNeverBorrowsCredentials(t *testing.T) {
	var calls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("an explicitly unauthenticated proxy inherited credentials")
		}
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	t.Cleanup(proxy.Close)
	profile := desktopNetwork()
	profile.ProxyMode, profile.ProxyURL, profile.ProxyTargets, profile.ProxyAuth = domain.NetworkProxy, proxy.URL, []string{"all"}, domain.NetworkNoAuth
	g, _, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model") }, func(l *gateway.Lease) {
		l.Provider.BaseURL, l.Network = "http://model.fixture.invalid/v1", profile
	})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixed-model"}`))
	r.Header.Set("Authorization", "Bearer "+lease.Token)
	r.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("unapproved-user:unapproved-password")))
	response := httptest.NewRecorder()
	g.ServeHTTP(response, r)
	if response.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatal("explicit unauthenticated model proxy route failed")
	}
	lease.Token, lease.ProxyCredential = "unapproved-proxy-credential", &domain.ProxyCredential{Username: "unapproved-user", Password: "unapproved-password"}
	if _, err := g.Register(lease); err == nil || calls.Load() != 1 {
		t.Fatal("a model lease retained a credential without explicit Basic proxy authentication")
	}
}

func TestModelNetworkProfileRevisionsAndProxyCredentialsAreFrozenPerLease(t *testing.T) {
	var firstCalls, secondCalls atomic.Int64
	firstCredential := domain.ProxyCredential{Username: "first-proxy-user", Password: "first-proxy-password"}
	secondCredential := domain.ProxyCredential{Username: "second-proxy-user", Password: "second-proxy-password"}
	proxyFor := func(credential domain.ProxyCredential, calls *atomic.Int64) *httptest.Server {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte(credential.Username+":"+credential.Password)) {
				t.Error("lease proxy credentials changed after registration")
			}
			w.Header().Set("X-Request-Id", credential.Password)
			_, _ = fmt.Fprintf(w, `{"id":%q,"model":%q,"choices":[]}`, credential.Username, credential.Password)
		}))
		t.Cleanup(proxy.Close)
		return proxy
	}
	firstProxy, secondProxy := proxyFor(firstCredential, &firstCalls), proxyFor(secondCredential, &secondCalls)
	profile, credential := authenticatedNetwork(firstProxy.URL, "all")
	*credential = firstCredential
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model route") }, func(l *gateway.Lease) {
		l.Provider.BaseURL, l.Network, l.ProxyCredential = "http://model.fixture.invalid/v1", profile, credential
	})
	profile.Revision, profile.CredentialRevision, profile.ProxyURL = "network-revision-2", "credential-revision-2", secondProxy.URL
	profile.ProxyTargets[0], profile.NoProxy = "https", "*"
	*credential = secondCredential
	second := lease
	second.Token, second.AttemptID = "second-lease", "second-attempt"
	second.Network, second.ProxyCredential = authenticatedNetwork(secondProxy.URL, "all")
	second.Network.Revision, second.Network.CredentialRevision = profile.Revision, profile.CredentialRevision
	*second.ProxyCredential = secondCredential
	if _, err := g.Register(second); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{lease.Token, second.Token, lease.Token} {
		if response := request(t, g, token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusOK {
			t.Fatal("frozen model profile failed", response.Code)
		}
	}
	if firstCalls.Load() != 2 || secondCalls.Load() != 1 {
		t.Fatal("independent lease routes or revisions were mixed")
	}
	encoded, _ := json.Marshal(ledger.records())
	for _, secret := range []string{firstCredential.Username, firstCredential.Password, secondCredential.Username, secondCredential.Password} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("a mutated credential escaped redaction in a frozen lease request row")
		}
	}
}

func TestModelNetworkProxyCredentialsStayOutOfDiagnosticsAndRequestRows(t *testing.T) {
	profile, credential := authenticatedNetwork("http://127.0.0.1:1", "all")
	encodedCredential := base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password))
	secrets := []string{credential.Username, credential.Password, encodedCredential, strings.TrimRight(encodedCredential, "=")}
	type proxyReply struct {
		echo   string
		reject bool
	}
	var reply atomic.Pointer[proxyReply]
	reply.Store(&proxyReply{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := reply.Load()
		echo, reject := current.echo, current.reject
		if r.Header.Get("Proxy-Authorization") != "Basic "+encodedCredential || r.Header.Get("X-Request-Id") != "" {
			t.Error("private proxy authentication or request-header filtering failed")
		}
		w.Header().Set("Proxy-Authenticate", "Basic "+echo)
		w.Header().Set("X-Request-Id", echo)
		w.Header().Set("X-Echo-Private", echo)
		w.Header().Set("X-Echo-"+credential.Username, "private")
		if reject {
			w.WriteHeader(http.StatusProxyAuthRequired)
			_, _ = io.WriteString(w, echo)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"model":%q,"choices":[]}`, echo, echo)
	}))
	t.Cleanup(proxy.Close)
	profile.ProxyURL = proxy.URL
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(http.ResponseWriter, *http.Request) { t.Error("wrong model route") }, func(l *gateway.Lease) {
		l.Provider.BaseURL, l.Network, l.ProxyCredential = "http://model.fixture.invalid/v1", profile, credential
	})
	for _, secret := range secrets {
		for _, failure := range []bool{false, true} {
			reply.Store(&proxyReply{echo: secret, reject: failure})
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixed-model"}`))
			r.Header.Set("Authorization", "Bearer "+lease.Token)
			r.Header.Set("X-Request-Id", secret)
			response := httptest.NewRecorder()
			g.ServeHTTP(response, r)
			want := http.StatusOK
			if failure {
				want = http.StatusBadGateway
			}
			if response.Code != want {
				t.Fatal("proxy privacy case did not use the expected native error boundary", response.Code)
			}
			for key, values := range response.Header() {
				if strings.Contains(strings.ToLower(key), strings.ToLower(credential.Username)) || strings.Contains(strings.Join(values, " "), secret) {
					t.Fatal("proxy credential escaped in response headers")
				}
			}
			if failure && strings.Contains(response.Body.String(), secret) {
				t.Fatal("proxy credential escaped in a transport diagnostic")
			}
		}
	}
	rows, _ := json.Marshal(ledger.records())
	leaseJSON, _ := json.Marshal(lease)
	diagnostics := fmt.Sprintf("%v %+v %#v %v", lease, lease, lease, &lease)
	for _, secret := range append(secrets, lease.Token, lease.APIKey) {
		if strings.Contains(string(rows), secret) || strings.Contains(string(leaseJSON), secret) || strings.Contains(diagnostics, secret) {
			t.Fatal("private gateway credential escaped to public records or diagnostics")
		}
	}
}
