package netpolicy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
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
)

const (
	testUsername = "fixture-proxy-user"
	testPassword = "fixture-proxy-password"
	testTimeout  = 5 * time.Second
)

func directPolicy() Policy {
	return Policy{ProxyMode: "direct", TLSMode: "verify", TrustMode: "system"}
}

func upstreamPolicy(address string) Policy {
	policy := directPolicy()
	policy.ProxyMode = "proxy"
	policy.ProxyURL = address
	policy.ProxyTargets = []string{"all"}
	policy.Auth = &Credentials{Username: testUsername, Password: testPassword}
	return policy
}

func expectedProxyAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(testUsername+":"+testPassword))
}

func newTLSFixture(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "netpolicy loopback fixture"},
		DNSNames:              []string{"destination.test"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func writeFixture(t *testing.T, data []byte) string {
	t.Helper()
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		t.Fatal(err)
	}
	// Fixtures stay in the package working directory, never a system temp dir.
	path := filepath.Join(".", ".netpolicy-fixture-"+hex.EncodeToString(token)+".pem")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("cannot create local certificate fixture")
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("cannot write local certificate fixture")
	}
	return path
}

func certificateFile(t *testing.T, server *httptest.Server) string {
	t.Helper()
	return writeFixture(t, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}

func clientFor(t *testing.T, policy Policy) *http.Client {
	t.Helper()
	client, err := NewClient(policy)
	if err != nil {
		t.Fatal(err)
	}
	client.Timeout = testTimeout
	transport := client.Transport.(*Transport)
	transport.base.DialContext = loopbackDial(transport.base.DialContext)
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func loopbackDial(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("netpolicy fixture: non-loopback dial forbidden")
		}
		return dial(ctx, network, address)
	}
}

func assertNoCredentials(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, testUsername) || strings.Contains(text, testPassword) || strings.Contains(text, expectedProxyAuth()) {
		t.Fatal("proxy credentials escaped a private upstream header")
	}
}

func TestExplicitPolicyValidation(t *testing.T) {
	invalidCA := writeFixture(t, []byte("not a certificate"))
	cases := []struct {
		name   string
		change func(*Policy)
	}{
		{"missing proxy mode", func(p *Policy) { p.ProxyMode = "" }},
		{"invalid proxy mode", func(p *Policy) { p.ProxyMode = "auto" }},
		{"missing TLS mode", func(p *Policy) { p.TLSMode = "" }},
		{"invalid TLS mode", func(p *Policy) { p.TLSMode = "auto" }},
		{"missing trust mode", func(p *Policy) { p.TrustMode = "" }},
		{"invalid trust mode", func(p *Policy) { p.TrustMode = "auto" }},
		{"unconfirmed TLS skip", func(p *Policy) { p.TLSMode = "skip" }},
		{"custom trust without file", func(p *Policy) { p.TrustMode = "custom" }},
		{"custom trust invalid PEM", func(p *Policy) { p.TrustMode, p.CACertPath = "custom", invalidCA }},
		{"custom trust unavailable file", func(p *Policy) {
			p.TrustMode, p.CACertPath = "custom", testUsername+testPassword+".missing"
		}},
		{"proxy without URL", func(p *Policy) { p.ProxyMode, p.ProxyTargets = "proxy", []string{"all"} }},
		{"proxy without targets", func(p *Policy) { p.ProxyMode, p.ProxyURL = "proxy", "http://proxy.test:8080" }},
		{"invalid proxy target", func(p *Policy) {
			p.ProxyMode, p.ProxyURL, p.ProxyTargets = "proxy", "http://proxy.test:8080", []string{"ftp"}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy := directPolicy()
			test.change(&policy)
			if _, err := NewTransport(policy); err == nil {
				t.Fatal("incomplete or invalid policy accepted")
			} else {
				assertNoCredentials(t, err.Error())
			}
		})
	}
	for _, value := range []string{
		"http://" + testUsername + ":" + testPassword + "@proxy.test:8080",
		"http://proxy.test:8080/", "http://proxy.test:8080/path", "http://proxy.test:8080?secret=" + testPassword,
		"http://proxy.test:8080?", "http://proxy.test:8080#secret", "http://proxy.test:8080#",
		"socks5://proxy.test:8080", "http://proxy.test:0", "http://proxy.test:65536", "http://proxy.test:",
	} {
		if _, err := NewTransport(upstreamPolicy(value)); err == nil {
			t.Fatal("invalid upstream URL accepted")
		} else {
			assertNoCredentials(t, err.Error())
		}
	}
	policy := upstreamPolicy("http://proxy.test:8080")
	policy.Auth.Username = "invalid:username"
	if _, err := NewTransport(policy); err == nil {
		t.Fatal("ambiguous Basic username accepted")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		assertNoCredentials(t, fmt.Sprintf(format, Credentials{testUsername, testPassword}))
		assertNoCredentials(t, fmt.Sprintf(format, upstreamPolicy("http://proxy.test:8080")))
	}
	for _, value := range []any{Credentials{testUsername, testPassword}, upstreamPolicy("http://proxy.test:8080")} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		assertNoCredentials(t, string(encoded))
	}
	policy = upstreamPolicy("http://proxy.test:8080")
	policy.Auth.Password = strings.Repeat("a", maxAuthBytes+1)
	if _, err := NewTransport(policy); err == nil {
		t.Fatal("unbounded proxy credentials accepted")
	}
	policy = directPolicy()
	policy.TrustMode, policy.CACertPath = "custom", writeFixture(t, []byte(strings.Repeat("a", maxCABytes+1)))
	if _, err := NewTransport(policy); err == nil {
		t.Fatal("unbounded CA file accepted")
	}
}

func TestTLSStrictCustomAndConfirmedSkip(t *testing.T) {
	server := newTLSFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ProtoMajor != 1 {
			t.Error("policy transport negotiated HTTP/2")
		}
		_, _ = io.WriteString(writer, "verified fixture")
	}))
	caFile := certificateFile(t, server)
	cases := []struct {
		name   string
		policy Policy
		wantOK bool
	}{
		{"strict system rejects self signed", directPolicy(), false},
		{"custom adds explicit certificate", Policy{ProxyMode: "direct", TLSMode: "verify", TrustMode: "custom", CACertPath: caFile}, true},
		{"skip ignores missing CA", Policy{ProxyMode: "direct", TLSMode: "skip", TrustMode: "custom", CACertPath: "absent-ca.pem", TLSRiskAccepted: true}, true},
		{"skip ignores invalid CA", Policy{ProxyMode: "direct", TLSMode: "skip", TrustMode: "custom", CACertPath: writeFixture(t, []byte("invalid PEM")), TLSRiskAccepted: true}, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := clientFor(t, test.policy)
			transport := client.Transport.(*Transport)
			if transport.base.TLSClientConfig.MinVersion != tls.VersionTLS12 || transport.base.TLSClientConfig.RootCAs == nil {
				t.Fatal("transport did not explicitly establish secure TLS/private trust")
			}
			response, err := client.Get(server.URL)
			if !test.wantOK {
				if err == nil {
					_ = response.Body.Close()
					t.Fatal("untrusted TLS certificate accepted")
				}
				if errors.Is(err, context.Canceled) {
					t.Fatal("TLS failure incorrectly reported as caller cancellation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if readBody(t, response) != "verified fixture" {
				t.Fatal("TLS fixture response changed")
			}
		})
	}
}

func TestHostProxyAndGlobalTLSAreNotInherited(t *testing.T) {
	var inheritedHits atomic.Int32
	inherited := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		inheritedHits.Add(1)
		_, _ = io.WriteString(writer, "inherited proxy")
	}))
	defer inherited.Close()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, inherited.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	original := http.DefaultTransport
	proxyURL, _ := url.Parse(inherited.URL)
	http.DefaultTransport = &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(func() { http.DefaultTransport = original })

	client := clientFor(t, directPolicy())
	transport := client.Transport.(*Transport)
	remote, _ := url.Parse("https://destination.test:443")
	proxy, err := transport.config.proxyForURL(remote)
	if err != nil || proxy != nil {
		t.Fatal("host proxy environment inherited by explicit direct policy")
	}
	server := newTLSFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {}))
	response, err := client.Get(server.URL)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("global TLS skip setting inherited")
	}
	if inheritedHits.Load() != 0 {
		t.Fatal("inherited proxy received a request")
	}
}

func TestExplicitProxyTargetsAndNoProxy(t *testing.T) {
	cases := []struct {
		name      string
		targets   []string
		noProxy   string
		address   string
		wantProxy bool
	}{
		{"HTTP target", []string{"http"}, "", "http://destination.test", true},
		{"HTTP excludes HTTPS", []string{"http"}, "", "https://destination.test", false},
		{"HTTPS target", []string{"https"}, "", "https://destination.test", true},
		{"HTTPS excludes HTTP", []string{"https"}, "", "http://destination.test", false},
		{"all target", []string{"all"}, "", "https://destination.test", true},
		{"separate targets", []string{"http", "https"}, "", "http://destination.test", true},
		{"matching no proxy", []string{"all"}, "destination.test", "https://destination.test", false},
		{"matching subdomain", []string{"all"}, ".internal.test", "https://api.internal.test", false},
		{"matching port", []string{"all"}, "destination.test:8443", "https://destination.test:8443", false},
		{"nonmatching port", []string{"all"}, "destination.test:8443", "https://destination.test:443", true},
		{"explicit wildcard", []string{"all"}, "*", "http://destination.test", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy := upstreamPolicy("http://proxy.test:8080")
			policy.ProxyTargets, policy.NoProxy = test.targets, test.noProxy
			transport, err := NewTransport(policy)
			if err != nil {
				t.Fatal(err)
			}
			address, _ := url.Parse(test.address)
			proxy, err := transport.config.proxyForURL(address)
			if err != nil || (proxy != nil) != test.wantProxy {
				t.Fatal("explicit target/no_proxy selection was not applied")
			}
			if proxy != nil && proxy.User != nil {
				t.Fatal("credentials placed in compiled proxy URL")
			}
		})
	}
}

func TestAuthenticatedHTTPTransportIsFrozenAndCredentialPrivate(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		if !request.URL.IsAbs() || request.Header.Get("Proxy-Authorization") != expectedProxyAuth() {
			t.Error("upstream did not receive absolute-form authenticated HTTP")
		}
		_, _ = io.WriteString(writer, "upstream response")
	}))
	defer upstream.Close()
	policy := upstreamPolicy(upstream.URL)
	client := clientFor(t, policy)
	policy.Auth.Username, policy.Auth.Password = "changed", "changed"
	policy.ProxyTargets[0], policy.NoProxy, policy.ProxyURL = "https", "*", "http://changed.test:8080"

	request, _ := http.NewRequest(http.MethodGet, "http://destination.test:8080/resource", nil)
	request.Header.Set("Proxy-Authorization", "downstream supplied auth")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if readBody(t, response) != "upstream response" || hits.Load() != 1 {
		t.Fatal("frozen transport changed its selected upstream")
	}
	if request.Header.Get("Proxy-Authorization") != "downstream supplied auth" {
		t.Fatal("transport mutated its caller's request")
	}
	assertNoCredentials(t, fmt.Sprint(response.Request.Header))
	if response.Request.Header.Get("Proxy-Authorization") != "" {
		t.Fatal("proxy authentication exposed through response.Request")
	}
}

func TestCustomCAPoolIsFrozen(t *testing.T) {
	server := newTLSFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, "frozen trust")
	}))
	path := certificateFile(t, server)
	policy := directPolicy()
	policy.TrustMode, policy.CACertPath = "custom", path
	client := clientFor(t, policy)
	if err := os.WriteFile(path, []byte("now invalid"), 0600); err != nil {
		t.Fatal("cannot replace local CA fixture")
	}
	if _, err := NewTransport(policy); err == nil {
		t.Fatal("replacement CA fixture was not invalidated")
	}
	policy.TLSMode, policy.TrustMode, policy.CACertPath = "skip", "system", "changed"
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if readBody(t, response) != "frozen trust" {
		t.Fatal("existing transport reloaded changed CA policy")
	}
}

func TestClientForbidsRedirectsAndReplay(t *testing.T) {
	t.Run("redirects", func(t *testing.T) {
		var redirected atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/redirect" {
				http.Redirect(writer, request, "/destination", http.StatusTemporaryRedirect)
				return
			}
			redirected.Add(1)
		}))
		defer server.Close()
		response, err := clientFor(t, directPolicy()).Get(server.URL + "/redirect")
		if err != nil {
			t.Fatal(err)
		}
		_ = readBody(t, response)
		if response.StatusCode != http.StatusTemporaryRedirect || redirected.Load() != 0 {
			t.Fatal("client followed a redirect")
		}
	})
	t.Run("no replay or fallback", func(t *testing.T) {
		var hits, replays atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			hits.Add(1)
			conn, _, err := http.NewResponseController(writer).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}))
		defer upstream.Close()
		client := clientFor(t, upstreamPolicy(upstream.URL))
		request, _ := http.NewRequest(http.MethodPost, "http://destination.test:8080", strings.NewReader("payload"))
		request.Header.Set("Idempotency-Key", "fixture-idempotent")
		request.GetBody = func() (io.ReadCloser, error) {
			replays.Add(1)
			return io.NopCloser(strings.NewReader("payload")), nil
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			t.Fatal("closed upstream did not fail")
		}
		assertNoCredentials(t, err.Error())
		if hits.Load() != 1 || replays.Load() != 0 {
			t.Fatal("request was replayed or retried")
		}
	})
}

func TestProxyRejectionDoesNotExposeReasonOrBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Proxy-Authenticate", "Basic realm="+testUsername)
		writer.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = io.WriteString(writer, testUsername+testPassword)
	}))
	defer upstream.Close()
	response, err := clientFor(t, upstreamPolicy(upstream.URL)).Get("http://destination.test:8080")
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("upstream authentication rejection exposed as a normal response")
	}
	assertNoCredentials(t, err.Error())
}
