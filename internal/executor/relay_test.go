package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestModelRelayPreservesNativeStreamingAndRejectsManagementAndWrongLease(t *testing.T) {
	called := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called++
		if r.URL.Host != "router-forge" || r.Header.Get("x-api-key") != "fixture-lease" {
			t.Error("wrong private relay authority or lease")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("event: message\ndata: fixture\n\n"))}, nil
	})}
	handler := NewModelRelay(client, "fixture-lease", domain.AnthropicMessages)
	for _, path := range []string{"/commands", "/results", "/control/runs", "/v1/chat/completions"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		r.Header.Set("x-api-key", "fixture-lease")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("management/wrong protocol accepted: %s %d", path, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"fixture"}`))
	r.Header.Set("x-api-key", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 || called != 0 {
		t.Fatal("invalid lease reached gateway")
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"fixture"}`))
	r.Header.Set("x-api-key", "fixture-lease")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if called != 1 || w.Code != 200 || w.Body.String() != "event: message\ndata: fixture\n\n" || !w.Flushed {
		t.Fatalf("stream changed: %d %q", w.Code, w.Body.String())
	}
}
func TestModelRelayAbortCancelsPrivateGatewayStream(t *testing.T) {
	aborted := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		close(aborted)
		return nil, r.Context().Err()
	})}
	handler := NewModelRelay(client, "lease", domain.ChatCompletions)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer lease")
	done := make(chan struct{})
	go func() { handler.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	cancel()
	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("abort was not propagated")
	}
	<-done
}
func TestModelRelayBoundsInputBeforeContactingGateway(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { called = true; return nil, context.Canceled })}
	handler := NewModelRelay(client, "lease", domain.ChatCompletions)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", maxModelBody+1)))
	r.Header.Set("Authorization", "Bearer lease")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || called {
		t.Fatal("oversized body reached gateway")
	}
}
