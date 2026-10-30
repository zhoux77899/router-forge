package gateway_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestRevocationAndClientCancellationCancelEveryActiveCallAndFinalize(t *testing.T) {
	for _, revoke := range []bool{true, false} {
		name := "client cancellation"
		if revoke {
			name = "revocation"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{}, 2)
			upstreamCanceled := make(chan struct{}, 2)
			g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				started <- struct{}{}
				<-r.Context().Done()
				upstreamCanceled <- struct{}{}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{}, 2)
			for i := 0; i < 2; i++ {
				go func() {
					r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixed-model","max_tokens":1}`)).WithContext(ctx)
					r.Header.Set("Authorization", "Bearer "+lease.Token)
					g.ServeHTTP(httptest.NewRecorder(), r)
					done <- struct{}{}
				}()
			}
			for i := 0; i < 2; i++ {
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatal("mock did not start")
				}
			}
			if revoke {
				g.Revoke(lease.Token)
			} else {
				cancel()
			}
			for i := 0; i < 2; i++ {
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("active call survived cancellation")
				}
				select {
				case <-upstreamCanceled:
				case <-time.After(2 * time.Second):
					t.Fatal("upstream context survived cancellation")
				}
			}
			if len(ledger.records()) != 2 || ledger.finalContextErr != nil {
				t.Fatal("canceled calls not finalized independently")
			}
			for _, r := range ledger.records() {
				if r.State != "canceled" || r.FinishedAt == nil || r.CostAmount != nil {
					t.Fatalf("cancellation record = %+v", r)
				}
			}
			if revoke {
				w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
				if w.Code != 401 {
					t.Fatal("revoked lease still authorized")
				}
			}
		})
	}
}

func TestGatewayCloseCancelsAllLeasesAndPreventsNewAdmission(t *testing.T) {
	started, upstreamCanceled, finished := make(chan struct{}, 2), make(chan struct{}, 2), make(chan struct{}, 2)
	g, ledger, first := fixture(t, domain.ChatCompletions, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
		upstreamCanceled <- struct{}{}
	})
	second := first
	second.Token, second.AttemptID = "second-close-lease", "second-close-attempt"
	if _, err := g.Register(second); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{first.Token, second.Token} {
		go func(token string) {
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixed-model"}`))
			r.Header.Set("Authorization", "Bearer "+token)
			g.ServeHTTP(httptest.NewRecorder(), r)
			finished <- struct{}{}
		}(token)
	}
	wait := func(signal <-chan struct{}) {
		t.Helper()
		select {
		case <-signal:
		case <-time.After(2 * time.Second):
			t.Fatal("gateway close did not bound every active model request")
		}
	}
	wait(started)
	wait(started)
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		wait(finished)
		wait(upstreamCanceled)
	}
	if len(ledger.records()) != 2 {
		t.Fatal("closing the gateway lost terminal model request rows")
	}
	for _, row := range ledger.records() {
		if row.State != "canceled" || row.FinishedAt == nil {
			t.Fatal("gateway close changed request cancellation accounting")
		}
	}
	for _, token := range []string{first.Token, second.Token} {
		if response := request(t, g, token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); response.Code != http.StatusUnauthorized {
			t.Fatal("closed gateway still authorized a model lease")
		}
	}
	second.Token = "post-close-lease"
	if _, err := g.Register(second); err == nil {
		t.Fatal("closed gateway admitted a new model profile")
	}
}

func TestLeaseTimeoutFinalizesWithFreshBoundedContext(t *testing.T) {
	g, ledger, lease := fixture(t, domain.AnthropicMessages, func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }, func(lease *gateway.Lease) { lease.Timeout = 40 * time.Millisecond })
	w := request(t, g, lease.Token, "POST", "/v1/messages", `{"model":"fixed-model","max_tokens":1}`)
	if w.Code != 504 {
		t.Fatalf("timeout status=%d", w.Code)
	}
	r := ledger.records()[0]
	if r.State != "timed_out" || r.ErrorClass != "deadline_exceeded" || r.FinishedAt == nil || ledger.finalContextErr != nil {
		t.Fatalf("timeout terminal = %+v", r)
	}
}

func TestCredentialsAndHopHeadersStayOnDesktopBoundary(t *testing.T) {
	for _, protocol := range []domain.Protocol{domain.ChatCompletions, domain.AnthropicMessages} {
		t.Run(string(protocol), func(t *testing.T) {
			g, _, lease := fixture(t, protocol, func(w http.ResponseWriter, r *http.Request) {
				for _, key := range []string{"Cookie", "Proxy-Authorization", "X-Private-Hop", "X-Router-Forge-Control"} {
					if r.Header.Get(key) != "" {
						t.Errorf("unsafe request header %s forwarded", key)
					}
				}
				if r.Header.Get("Anthropic-Beta") != "tools-test" || r.Header.Get("OpenAI-Beta") != "responses-test" {
					t.Error("native feature headers were not preserved")
				}
				conn, writer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				payload := `{"type":"message","choices":[],"content":[]}`
				_, _ = fmt.Fprintf(writer, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: X-Upstream-Hop\r\nX-Upstream-Hop: private\r\nAuthorization: Bearer desktop-upstream-secret\r\nX-Api-Key: desktop-upstream-secret\r\nX-Echo-Key: prefix desktop-upstream-secret suffix\r\nX-Echo-Lease: attempt-token\r\nSet-Cookie: credential=desktop-upstream-secret\r\nX-Safe: native\r\n\r\n%s", len(payload), payload)
				_ = writer.Flush()
			})
			path := "/v1/chat/completions"
			if protocol == domain.AnthropicMessages {
				path = "/v1/messages"
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"fixed-model"}`))
			r.Header.Set("Authorization", "Bearer "+lease.Token)
			r.Header.Set("X-Api-Key", lease.Token)
			r.Header.Set("Cookie", "private=1")
			r.Header.Set("Proxy-Authorization", "private")
			r.Header.Set("Connection", "X-Private-Hop")
			r.Header.Set("X-Private-Hop", "private")
			r.Header.Set("X-Router-Forge-Control", "desktop-control-secret")
			r.Header.Set("Anthropic-Beta", "tools-test")
			r.Header.Set("OpenAI-Beta", "responses-test")
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("X-Safe") != "native" {
				t.Fatal("native response failed")
			}
			for key, values := range w.Header() {
				for _, value := range values {
					if strings.Contains(value, lease.Token) || strings.Contains(value, lease.APIKey) {
						t.Fatalf("credential escaped in response %s", key)
					}
				}
			}
			for _, key := range []string{"Authorization", "X-Api-Key", "Set-Cookie", "X-Upstream-Hop"} {
				if w.Header().Get(key) != "" {
					t.Fatalf("unsafe response header %s forwarded", key)
				}
			}
		})
	}
}

func TestProviderRedirectIsNeverFollowed(t *testing.T) {
	redirected := make(chan struct{}, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected <- struct{}{} }))
	defer destination.Close()
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = io.WriteString(w, "native redirect")
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	if w.Code != 307 || w.Body.String() != "native redirect" || ledger.records()[0].State != "failed" {
		t.Fatal("provider redirect changed")
	}
	select {
	case <-redirected:
		t.Fatal("provider redirect sent a hidden request")
	default:
	}
}
