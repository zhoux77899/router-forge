package netpolicy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func authorizeLoopback(remote net.IP) bool {
	return remote.Equal(net.ParseIP("127.0.0.1"))
}

func relayFor(t *testing.T, ctx context.Context, policy Policy, authorize func(net.IP) bool) *ProxyRelay {
	t.Helper()
	relay, err := StartProxyRelay(ctx, net.ParseIP("127.0.0.1"), policy, authorize)
	if err != nil {
		t.Fatal(err)
	}
	relay.dial = loopbackDial(relay.dial)
	t.Cleanup(func() { _ = relay.Close() })
	return relay
}

func downstreamClient(t *testing.T, relay *ProxyRelay) *http.Client {
	t.Helper()
	address, err := url.Parse(relay.URL)
	if err != nil {
		t.Fatal("invalid public relay URL")
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(address), DisableKeepAlives: true},
		Timeout:   testTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func rawConnect(t *testing.T, relay *ProxyRelay, target, earlyData string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	address, err := url.Parse(relay.URL)
	if err != nil {
		t.Fatal("invalid relay address")
	}
	conn, err := net.DialTimeout("tcp", address.Host, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(testTimeout))
	request := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target +
		"\r\nProxy-Authorization: downstream-only\r\nCookie: downstream-cookie\r\n\r\n" + earlyData
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	buffered := bufio.NewReader(conn)
	response, err := http.ReadResponse(buffered, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	return conn, buffered, response
}

func assertRead(t *testing.T, reader io.Reader, expected string) {
	t.Helper()
	data := make([]byte, len(expected))
	if _, err := io.ReadFull(reader, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatal("tunnel did not preserve bidirectional/read-ahead data")
	}
}

func echoProxy(t *testing.T, secure bool) (*httptest.Server, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	hits := &atomic.Int32{}
	exited := make(chan struct{}, 8)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		if request.Method != http.MethodConnect || request.RequestURI != "destination.test:443" {
			t.Error("unexpected upstream CONNECT authority")
		}
		if request.Header.Get("Proxy-Authorization") != expectedProxyAuth() {
			t.Error("upstream CONNECT missing private authentication")
		}
		if request.Header.Get("Cookie") != "" || request.Header.Get("Proxy-Connection") != "" {
			t.Error("downstream CONNECT hop headers reached upstream")
		}
		conn, buffered, err := http.NewResponseController(writer).Hijack()
		if err != nil {
			t.Error("cannot hijack loopback CONNECT fixture")
			return
		}
		defer func() {
			_ = conn.Close()
			exited <- struct{}{}
		}()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\nbanner:")
		if err := buffered.Flush(); err != nil {
			return
		}
		_, _ = io.Copy(conn, buffered.Reader)
	})
	if secure {
		return newTLSFixture(t, handler), hits, exited
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, hits, exited
}

func TestRelayRejectsMissingAuthorizationAndUnsafeBind(t *testing.T) {
	policy := upstreamPolicy("http://proxy.test:8080")
	if _, err := StartProxyRelay(context.Background(), net.ParseIP("127.0.0.1"), policy, nil); err == nil {
		t.Fatal("relay accepted a missing authorization callback")
	}
	for _, value := range []string{"", "0.0.0.0", "::", "192.0.2.1", "224.0.0.1", "169.254.1.1"} {
		if _, err := StartProxyRelay(context.Background(), net.ParseIP(value), policy, authorizeLoopback); err == nil {
			t.Fatal("relay accepted unspecified/public/unsafe binding")
		} else {
			assertNoCredentials(t, err.Error())
		}
	}
	if _, err := StartProxyRelay(context.Background(), net.ParseIP("127.0.0.1"), directPolicy(), authorizeLoopback); err == nil {
		t.Fatal("relay accepted an unconfigured upstream")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StartProxyRelay(ctx, net.ParseIP("127.0.0.1"), policy, authorizeLoopback); err == nil {
		t.Fatal("relay started for a canceled owner")
	}
}

func TestRelayAuthenticatedHTTPAndCredentialFreePublicURL(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "HTTP upstream"
		if secure {
			name = "HTTPS upstream with custom trust"
		}
		t.Run(name, func(t *testing.T) {
			var hits, authorizations atomic.Int32
			handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				hits.Add(1)
				if !request.URL.IsAbs() || request.RequestURI != "http://destination.test:8080/resource" {
					t.Error("relay did not forward absolute-form HTTP to upstream")
				}
				if request.Header.Get("Proxy-Authorization") != expectedProxyAuth() {
					t.Error("upstream did not receive private Basic auth")
				}
				if request.Header.Get("Cookie") != "" || request.Header.Get("X-Downstream-Hop") != "" || request.Header.Get("Proxy-Connection") != "" {
					t.Error("relay forwarded downstream cookies/hop headers")
				}
				if request.Header.Get("Authorization") != "Bearer destination-fixture" {
					t.Error("destination authorization was incorrectly discarded")
				}
				conn, buffered, err := http.NewResponseController(writer).Hijack()
				if err != nil {
					t.Error("cannot write upstream hop-header fixture")
					return
				}
				defer conn.Close()
				// net/http otherwise replaces Connection nominations on close requests.
				_, _ = buffered.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 15\r\nConnection: X-Upstream-Hop, close\r\nX-Upstream-Hop: hop value\r\nProxy-Authorization: " + expectedProxyAuth() + "\r\nSet-Cookie: upstream-cookie\r\n\r\nproxied fixture")
				_ = buffered.Flush()
			})
			var upstream *httptest.Server
			if secure {
				upstream = newTLSFixture(t, handler)
			} else {
				upstream = httptest.NewServer(handler)
				t.Cleanup(upstream.Close)
			}
			policy := upstreamPolicy(upstream.URL)
			if secure {
				policy.TrustMode, policy.CACertPath = "custom", certificateFile(t, upstream)
			}
			relay := relayFor(t, context.Background(), policy, func(remote net.IP) bool {
				authorizations.Add(1)
				return authorizeLoopback(remote)
			})
			address, _ := url.Parse(relay.URL)
			if address.Scheme != "http" || address.User != nil || address.Path != "" || address.RawQuery != "" || address.Fragment != "" || address.Port() == "" {
				t.Fatal("relay exposed a non-minimal downstream URL")
			}
			assertNoCredentials(t, relay.URL)
			request, _ := http.NewRequest(http.MethodGet, "http://destination.test:8080/resource", nil)
			request.Header.Set("Proxy-Authorization", "downstream-only")
			request.Header.Set("Cookie", "downstream-cookie")
			request.Header.Set("Connection", "X-Downstream-Hop")
			request.Header.Set("X-Downstream-Hop", "hop value")
			request.Header.Set("Proxy-Connection", "keep-alive")
			request.Header.Set("Authorization", "Bearer destination-fixture")
			response, err := downstreamClient(t, relay).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			if readBody(t, response) != "proxied fixture" || response.StatusCode != http.StatusOK || hits.Load() != 1 || authorizations.Load() != 1 {
				t.Fatal("authenticated HTTP relay did not execute exactly once")
			}
			for _, name := range []string{"Proxy-Authorization", "Proxy-Authenticate", "X-Upstream-Hop", "Set-Cookie"} {
				if response.Header.Get(name) != "" {
					t.Fatalf("upstream hop/cookie header %s exposed to downstream", name)
				}
			}
			if request.Header.Get("Cookie") != "downstream-cookie" {
				t.Fatal("relay path mutated its caller request")
			}
		})
	}
}

func TestRelayCONNECTBidirectionalAndHTTPSProxyTrust(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
		skip   bool
	}{
		{"HTTP proxy", false, false},
		{"HTTPS custom CA", true, false},
		{"HTTPS confirmed skip ignores invalid CA", true, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			upstream, hits, exited := echoProxy(t, test.secure)
			policy := upstreamPolicy(upstream.URL)
			if test.secure {
				policy.TrustMode, policy.CACertPath = "custom", certificateFile(t, upstream)
			}
			if test.skip {
				policy.TLSMode, policy.TLSRiskAccepted, policy.CACertPath = "skip", true, "invalid-missing-ca.pem"
			}
			relay := relayFor(t, context.Background(), policy, authorizeLoopback)
			conn, buffered, response := rawConnect(t, relay, "destination.test:443", "early")
			if response.StatusCode != http.StatusOK {
				t.Fatal("relay CONNECT failed")
			}
			assertRead(t, buffered, "banner:early")
			if _, err := io.WriteString(conn, "later"); err != nil {
				t.Fatal(err)
			}
			assertRead(t, buffered, "later")
			if tcp, ok := conn.(*net.TCPConn); ok {
				if err := tcp.CloseWrite(); err != nil {
					t.Fatal(err)
				}
			}
			if data, err := io.ReadAll(buffered); err != nil || len(data) != 0 {
				t.Fatal("CONNECT half-close failed to terminate both stream directions")
			}
			select {
			case <-exited:
			case <-time.After(testTimeout):
				t.Fatal("upstream tunnel remained active")
			}
			if hits.Load() != 1 {
				t.Fatal("CONNECT was retried")
			}
		})
	}
}

func TestRelayStrictTLSRejectsUntrustedHTTPSProxy(t *testing.T) {
	upstream, hits, _ := echoProxy(t, true)
	relay := relayFor(t, context.Background(), upstreamPolicy(upstream.URL), authorizeLoopback)
	_, _, response := rawConnect(t, relay, "destination.test:443", "")
	if response.StatusCode != http.StatusBadGateway {
		t.Fatal("strict relay TLS trusted an unconfigured HTTPS proxy certificate")
	}
	assertNoCredentials(t, readBody(t, response))
	if hits.Load() != 0 {
		t.Fatal("proxy credentials sent before HTTPS proxy verification succeeded")
	}
}

func TestRelayDeniesEveryHTTPRequestAndCONNECT(t *testing.T) {
	var upstreamHits, authorizations atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamHits.Add(1)
	}))
	t.Cleanup(upstream.Close)
	relay := relayFor(t, context.Background(), upstreamPolicy(upstream.URL), func(remote net.IP) bool {
		authorizations.Add(1)
		if !authorizeLoopback(remote) {
			t.Error("authorization was not based on the actual TCP peer")
		}
		return false
	})
	request, _ := http.NewRequest(http.MethodGet, "http://destination.test:8080", nil)
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	response, err := downstreamClient(t, relay).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("unauthorized HTTP request accepted")
	}
	assertNoCredentials(t, readBody(t, response))
	_, _, response = rawConnect(t, relay, "destination.test:443", "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("unauthorized CONNECT accepted")
	}
	assertNoCredentials(t, readBody(t, response))
	if upstreamHits.Load() != 0 || authorizations.Load() != 2 {
		t.Fatal("request-level remote authorization was bypassed")
	}
}

func TestRelayConfiguredDirectRouteStripsProxyCredentialsAndCookies(t *testing.T) {
	var destinationHits, proxyHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		destinationHits.Add(1)
		if request.Header.Get("Proxy-Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Error("proxy credentials/cookies reached a direct destination")
		}
		_, _ = io.WriteString(writer, "direct fixture")
	}))
	t.Cleanup(destination.Close)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxyHits.Add(1)
	}))
	t.Cleanup(upstream.Close)
	policy := upstreamPolicy(upstream.URL)
	policy.NoProxy = "destination.test"
	relay := relayFor(t, context.Background(), policy, authorizeLoopback)
	ownedDial := relay.transport.base.DialContext
	relay.transport.base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "destination.test:8080" {
			return ownedDial(ctx, network, destination.Listener.Addr().String())
		}
		return ownedDial(ctx, network, address)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://destination.test:8080", nil)
	request.Header.Set("Proxy-Authorization", "downstream-only")
	request.Header.Set("Cookie", "downstream-cookie")
	response, err := downstreamClient(t, relay).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if readBody(t, response) != "direct fixture" || destinationHits.Load() != 1 || proxyHits.Load() != 0 {
		t.Fatal("explicit no_proxy did not select only the direct route")
	}
}

func TestRelayCancelAndCloseOwnActiveTunnels(t *testing.T) {
	for _, explicitClose := range []bool{false, true} {
		name := "context cancel"
		if explicitClose {
			name = "explicit Close"
		}
		t.Run(name, func(t *testing.T) {
			upstream, _, exited := echoProxy(t, false)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			relay := relayFor(t, ctx, upstreamPolicy(upstream.URL), authorizeLoopback)
			conn, buffered, response := rawConnect(t, relay, "destination.test:443", "")
			if response.StatusCode != http.StatusOK {
				t.Fatal("cannot establish cleanup fixture tunnel")
			}
			assertRead(t, buffered, "banner:")
			if explicitClose {
				if err := relay.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case <-relay.Done():
			case <-time.After(testTimeout):
				t.Fatal("relay owner cancellation did not finish cleanup")
			}
			_ = conn.SetReadDeadline(time.Now().Add(testTimeout))
			if _, err := buffered.ReadByte(); err == nil {
				t.Fatal("owned downstream tunnel remained open")
			}
			select {
			case <-exited:
			case <-time.After(testTimeout):
				t.Fatal("owned upstream tunnel remained open")
			}
			address, _ := url.Parse(relay.URL)
			if probe, err := net.DialTimeout("tcp", address.Host, testTimeout); err == nil {
				_ = probe.Close()
				t.Fatal("relay listener remained open")
			}
			relay.mu.Lock()
			connections := len(relay.conns)
			relay.mu.Unlock()
			if connections != 0 || len(relay.streams) != 0 {
				t.Fatal("relay retained owned connections or streams")
			}
			if err := relay.Close(); err != nil {
				t.Fatal("repeated Close was not idempotent")
			}
		})
	}
}

func TestRelayCancelOwnsActiveHTTPStreams(t *testing.T) {
	exited := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, "chunk")
		_ = http.NewResponseController(writer).Flush()
		<-request.Context().Done()
		exited <- struct{}{}
	}))
	t.Cleanup(upstream.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	relay := relayFor(t, ctx, upstreamPolicy(upstream.URL), authorizeLoopback)
	response, err := downstreamClient(t, relay).Get("http://destination.test:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	assertRead(t, response.Body, "chunk")
	cancel()
	select {
	case <-relay.Done():
	case <-time.After(testTimeout):
		t.Fatal("active HTTP stream prevented owner cleanup")
	}
	select {
	case <-exited:
	case <-time.After(testTimeout):
		t.Fatal("active upstream HTTP connection not closed")
	}
}

func TestRelayBoundsStreamsAndDoesNotRetryOrExposeProxyErrors(t *testing.T) {
	t.Run("stream saturation", func(t *testing.T) {
		var hits atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { hits.Add(1) }))
		t.Cleanup(upstream.Close)
		relay := relayFor(t, context.Background(), upstreamPolicy(upstream.URL), authorizeLoopback)
		for range cap(relay.streams) {
			relay.streams <- struct{}{}
		}
		defer func() {
			for range cap(relay.streams) {
				<-relay.streams
			}
		}()
		response, err := downstreamClient(t, relay).Get("http://destination.test:8080")
		if err != nil {
			t.Fatal(err)
		}
		_ = readBody(t, response)
		if response.StatusCode != http.StatusServiceUnavailable || hits.Load() != 0 {
			t.Fatal("saturated relay queued or forwarded another stream")
		}
	})
	t.Run("no retry", func(t *testing.T) {
		var hits atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			hits.Add(1)
			conn, _, err := http.NewResponseController(writer).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}))
		t.Cleanup(upstream.Close)
		relay := relayFor(t, context.Background(), upstreamPolicy(upstream.URL), authorizeLoopback)
		response, err := downstreamClient(t, relay).Get("http://destination.test:8080")
		if err != nil {
			t.Fatal(err)
		}
		assertNoCredentials(t, readBody(t, response))
		if response.StatusCode != http.StatusBadGateway || hits.Load() != 1 {
			t.Fatal("relay retried or fell back after upstream failure")
		}
	})
	t.Run("bounded CONNECT headers and private error", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			conn, buffered, err := http.NewResponseController(writer).Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = buffered.WriteString("HTTP/1.1 200 " + testUsername + testPassword + "\r\nX-Huge: " + strings.Repeat("a", maxHeaderBytes+1) + "\r\n\r\n")
			_ = buffered.Flush()
		}))
		t.Cleanup(upstream.Close)
		relay := relayFor(t, context.Background(), upstreamPolicy(upstream.URL), authorizeLoopback)
		_, _, response := rawConnect(t, relay, "destination.test:443", "")
		if response.StatusCode != http.StatusBadGateway {
			t.Fatal("oversized upstream CONNECT headers accepted")
		}
		assertNoCredentials(t, readBody(t, response))
	})
}

func TestClientHTTPSCONNECTAuthenticatesOnlyUpstream(t *testing.T) {
	var destinationHits, proxyHits atomic.Int32
	destination := newTLSFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		destinationHits.Add(1)
		if request.Header.Get("Proxy-Authorization") != "" {
			t.Error("upstream proxy authentication reached HTTPS destination")
		}
		_, _ = io.WriteString(writer, "TLS destination")
	}))
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxyHits.Add(1)
		if request.Method != http.MethodConnect || request.Header.Get("Proxy-Authorization") != expectedProxyAuth() {
			t.Error("client CONNECT was not privately authenticated")
			return
		}
		target, err := net.DialTimeout("tcp", destination.Listener.Addr().String(), testTimeout)
		if err != nil {
			t.Error("cannot dial loopback TLS destination")
			return
		}
		defer target.Close()
		conn, buffered, err := http.NewResponseController(writer).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if buffered.Flush() != nil {
			return
		}
		copied := make(chan struct{})
		go func() {
			_, _ = io.Copy(target, buffered.Reader)
			closeWrite(target)
			close(copied)
		}()
		_, _ = io.Copy(conn, target)
		_ = conn.Close()
		_ = target.Close()
		<-copied
	}))
	t.Cleanup(upstream.Close)
	policy := upstreamPolicy(upstream.URL)
	policy.TrustMode, policy.CACertPath = "custom", certificateFile(t, destination)
	client := clientFor(t, policy)
	request, _ := http.NewRequest(http.MethodGet, "https://destination.test:443/resource", nil)
	request.Header.Set("Proxy-Authorization", "downstream-only")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if readBody(t, response) != "TLS destination" || destinationHits.Load() != 1 || proxyHits.Load() != 1 {
		t.Fatal("HTTPS client did not use exactly one authenticated upstream CONNECT")
	}
	if response.Request.Header.Get("Proxy-Authorization") != "" {
		t.Fatal("private CONNECT auth exposed in client response")
	}
}
