package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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

	original := http.DefaultTransport
	http.DefaultTransport = upstream.Client().Transport
	defer func() { http.DefaultTransport = original }()
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		t.Error("wrong mock provider used")
	}, func(lease *gateway.Lease) {
		lease.Provider.BaseURL = upstream.URL
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
