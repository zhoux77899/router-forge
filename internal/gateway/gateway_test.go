package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

type memoryLedger struct {
	mu              sync.Mutex
	begun           []domain.RequestRecord
	ended           []domain.RequestRecord
	beginErr        error
	endErr          error
	finalContextErr error
}

func (l *memoryLedger) BeginRequest(ctx context.Context, r domain.RequestRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.beginErr != nil {
		return l.beginErr
	}
	l.begun = append(l.begun, r)
	return nil
}

func (l *memoryLedger) EndRequest(ctx context.Context, r domain.RequestRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.finalContextErr = ctx.Err()
	l.ended = append(l.ended, r)
	return l.endErr
}

func (l *memoryLedger) records() []domain.RequestRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]domain.RequestRecord(nil), l.ended...)
}

func fixture(t *testing.T, protocol domain.Protocol, handler http.HandlerFunc, configure ...func(*gateway.Lease)) (*gateway.Gateway, *memoryLedger, gateway.Lease) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	ledger := &memoryLedger{}
	lease := gateway.Lease{
		Token: "attempt-token", RunID: "run-1", TaskID: "task-1", AttemptID: "attempt-1",
		Provider: domain.Provider{ID: "provider-1", BaseURL: upstream.URL + "/v1"},
		Model:    domain.Model{ID: "model-1", ProviderID: "provider-1", Name: "fixed-model", Protocol: protocol},
		APIKey:   "desktop-upstream-secret", MaxRequests: 20, MaxOutputTokens: 32, Timeout: 5 * time.Second,
	}
	for _, change := range configure {
		change(&lease)
	}
	g := gateway.New(ledger)
	if _, err := g.Register(lease); err != nil {
		t.Fatal(err)
	}
	return g, ledger, lease
}

func request(t *testing.T, g http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}

func tokens(t *testing.T, actual *int64, expected int64) {
	t.Helper()
	if actual == nil || *actual != expected {
		t.Fatalf("tokens = %v, want %d", actual, expected)
	}
}

func TestChatSyncPreservesNativePayloadAndRecordsAttempt(t *testing.T) {
	response := `{"id":"chatcmpl-native","model":"fixed-model-2026","choices":[{"message":{"role":"assistant","content":"not recorded"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`
	input := `{"model":"fixed-model","messages":[{"role":"user","content":"private prompt"}],"max_completion_tokens":16}`
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer desktop-upstream-secret" || r.Header.Get("X-Api-Key") != "" {
			t.Error("incorrect upstream credentials")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != input {
			t.Errorf("native request was changed: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-native")
		_, _ = io.WriteString(w, response)
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", input)
	if w.Code != http.StatusOK || w.Body.String() != response {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
	records := ledger.records()
	if len(records) != 1 {
		t.Fatalf("terminal records = %d", len(records))
	}
	r := records[0]
	if r.ID == "" || r.RunID != lease.RunID || r.TaskID != lease.TaskID || r.AttemptID != lease.AttemptID || r.ProviderID != lease.Provider.ID || r.ModelID != lease.Model.ID || r.Protocol != domain.ChatCompletions || r.Kind != "inference" || r.State != "completed" {
		t.Fatalf("attribution = %+v", r)
	}
	if r.RequestedModel != "fixed-model" || r.ActualModel != "fixed-model-2026" || r.UpstreamRequestID != "req-native" || r.UpstreamResponseID != "chatcmpl-native" {
		t.Fatalf("source metadata = %+v", r)
	}
	tokens(t, r.Usage.InputTokens, 7)
	tokens(t, r.Usage.OutputTokens, 3)
	tokens(t, r.Usage.CacheReadTokens, 2)
	tokens(t, r.Usage.ReasoningTokens, 1)
	tokens(t, r.Usage.TotalTokens, 10)
	if r.Usage.Completeness != "complete" || r.CostAmount != nil || r.DurationMS == nil || r.FirstByteMS == nil || r.FinishedAt == nil {
		t.Fatalf("accounting = %+v", r)
	}
	encoded, _ := json.Marshal(r)
	for _, secret := range []string{"private prompt", "not recorded", lease.Token, lease.APIKey} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("ledger contains private data %q", secret)
		}
	}
}
