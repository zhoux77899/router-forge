package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestUpstreamUsesFreshHTTP1ConnectionsWithoutHTTP2Replay(t *testing.T) {
	connections := map[string]bool{}
	var mu sync.Mutex
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.ProtoMajor != 1 {
			t.Errorf("HTTP/2 permits implicit request replay: %s", r.Proto)
		}
		if connections[r.RemoteAddr] {
			t.Error("upstream connection reused")
		}
		connections[r.RemoteAddr] = true
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()

	caPath := gatewayCAFile(t, upstream)
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		t.Error("wrong mock provider used")
	}, func(lease *gateway.Lease) {
		lease.Provider.BaseURL = upstream.URL
		lease.Network.TrustMode, lease.Network.CACertPath = domain.NetworkCustomCA, caPath
	})
	for i := 0; i < 2; i++ {
		w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
		if w.Code != 200 {
			t.Fatalf("native TLS request failed: %d", w.Code)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ledger.records()) != 2 || len(connections) != 2 {
		t.Fatal("missing independent native requests")
	}
}

func TestUpstreamFailureNeverRetriesAnAdmittedInference(t *testing.T) {
	var calls atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}, func(l *gateway.Lease) { l.MaxRequests = 1 })
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixed-model"}`))
	r.Header.Set("Authorization", "Bearer "+lease.Token)
	r.Header.Set("Idempotency-Key", "native-idempotent-request")
	response := httptest.NewRecorder()
	g.ServeHTTP(response, r)
	if response.Code != http.StatusBadGateway || calls.Load() != 1 || len(ledger.records()) != 1 || ledger.records()[0].State != "failed" {
		t.Fatal("an admitted native request was replayed or lost its terminal record")
	}
	if denied := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); denied.Code != http.StatusTooManyRequests || calls.Load() != 1 {
		t.Fatal("a failed upstream call did not spend exactly one inference reservation")
	}
}
