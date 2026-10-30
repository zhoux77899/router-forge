package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestProviderBasePathsAndSafeVersionQueryAreNormalized(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"", "/v1/responses"}, {"/v1", "/v1/responses"}, {"/v1///", "/v1/responses"},
		{"/proxy", "/proxy/v1/responses"}, {"/proxy/v1/", "/proxy/v1/responses"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			g, _, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.want || r.URL.Query().Get("api-version") != "2026-01-01-preview" {
					t.Errorf("normalized URL: %s", r.URL)
				}
				_, _ = io.WriteString(w, `{"id":"resp-version","status":"completed","output":[]}`)
			}, func(lease *gateway.Lease) {
				lease.Provider.BaseURL = strings.TrimSuffix(lease.Provider.BaseURL, "/v1") + tc.base + "?api-version=2026-01-01-preview"
			})
			if w := request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`); w.Code != 200 {
				t.Fatalf("normalized request=%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRegistrationRejectsUnsafeURLsAndIncompleteAttributionAndGeneratesTokens(t *testing.T) {
	g, _, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {})
	for _, base := range []string{"file:///private", "http://user:secret@localhost/v1", "http://localhost/v1?api-key=secret", "http://localhost/v1#private", "http://localhost/a/../v1", "http://localhost/a%2Fv1"} {
		candidate := lease
		candidate.Token, candidate.Provider.BaseURL = "new-attempt", base
		if _, err := g.Register(candidate); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe provider URL accepted or leaked: %s error=%v", base, err)
		}
	}
	for _, change := range []func(*gateway.Lease){
		func(l *gateway.Lease) { l.AttemptID = "" }, func(l *gateway.Lease) { l.Model.ProviderID = "elsewhere" },
		func(l *gateway.Lease) { l.MaxRequests = 0 }, func(l *gateway.Lease) { l.MaxOutputTokens = 0 },
		func(l *gateway.Lease) { l.Timeout = 0 }, func(l *gateway.Lease) { l.APIKey = "secret\r\nInjected: yes" },
	} {
		candidate := lease
		candidate.Token = "new-attempt"
		change(&candidate)
		if _, err := g.Register(candidate); err == nil {
			t.Fatal("unsafe/incomplete lease accepted")
		}
	}
	lease.Token = ""
	token, err := g.Register(lease)
	if err != nil || len(token) != 43 || token == lease.APIKey {
		t.Fatalf("cryptographic lease token=%q err=%v", token, err)
	}
	lease.Token = token
	if _, err := g.Register(lease); err == nil {
		t.Fatal("existing lease credential replaced")
	}
}

func TestModelNamesWithNativeNamespaceAreRecordedAndClientHeadersCannotOverrideRouting(t *testing.T) {
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Model-Override") != "" || r.Header.Get("X-Upstream-Url") != "" || r.Header.Get("X-Client-Request-Id") == "attempt-token" {
			t.Error("non-native routing/auth header forwarded")
		}
		_, _ = io.WriteString(w, `{"id":"chatcmpl-namespaced","model":"vendor/model-2026","choices":[]}`)
	}, func(lease *gateway.Lease) { lease.Model.Name = "vendor/model" })
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"vendor/model","max_tokens":1}`))
	r.Header.Set("Authorization", "Bearer "+lease.Token)
	r.Header.Set("X-Model-Override", "unapproved")
	r.Header.Set("X-Upstream-Url", "http://unapproved.invalid")
	r.Header.Set("X-Client-Request-Id", lease.Token)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 200 || ledger.records()[0].ActualModel != "vendor/model-2026" {
		t.Fatalf("native model metadata lost: %+v", ledger.records())
	}
}

func TestNativeAuthenticationErrorEnvelopeForAnthropic(t *testing.T) {
	g, _, _ := fixture(t, domain.AnthropicMessages, func(w http.ResponseWriter, r *http.Request) {})
	w := request(t, g, "unregistered", "POST", "/v1/messages", `{"model":"fixed-model"}`)
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 401 || body.Type != "error" || body.Error.Type != "authentication_error" {
		t.Fatalf("Anthropic authentication envelope=%d %s", w.Code, w.Body.String())
	}
}

func TestInvalidOrInconsistentUsageIsPartialAndCannotProduceAnExactCost(t *testing.T) {
	for _, usage := range []string{
		`{"prompt_tokens":4,"completion_tokens":2,"total_tokens":3}`,
		`{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6,"prompt_tokens_details":{"cached_tokens":5}}`,
		`{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6,"completion_tokens_details":{"reasoning_tokens":3}}`,
		`{"prompt_tokens":4,"completion_tokens":2,"total_tokens":"invalid"}`,
		`{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6,"prompt_tokens_details":{"cached_tokens":-1}}`,
		`{"prompt_tokens":9223372036854775807,"completion_tokens":2,"total_tokens":9223372036854775807}`,
	} {
		g, ledger, lease := pricedFixture(t, domain.ChatCompletions, &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1"}, `{"choices":[],"usage":`+usage+`}`)
		request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
		r := ledger.records()[0]
		if r.Usage.Completeness != "partial" || r.CostAmount != nil {
			t.Fatalf("invalid usage treated as exact: %s => %+v", usage, r)
		}
	}
}

func TestKnownIntegerCostAndRegistrationPricingSnapshotRemainExact(t *testing.T) {
	pricing := &domain.Pricing{Currency: "USD", InputPerMillion: "0", OutputPerMillion: "1000000000", Source: "configured-estimate"}
	g, ledger, lease := pricedFixture(t, domain.ChatCompletions, pricing, `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":1,"total_tokens":1}}`)
	pricing.OutputPerMillion = "0"
	pricing.Currency = "OTHER"
	request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	r := ledger.records()[0]
	if r.CostAmount == nil || *r.CostAmount != "1000" || r.Currency == nil || *r.Currency != "USD" {
		t.Fatalf("snapshot/exact integer cost=%+v", r)
	}
}

func TestFinalizationContextIsFreshAndBounded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"choices":[]}`) }))
	defer upstream.Close()
	lease := gateway.Lease{Token: "deadline-token", RunID: "run", TaskID: "task", AttemptID: "attempt", Provider: domain.Provider{ID: "provider", BaseURL: upstream.URL}, Model: domain.Model{ID: "model", ProviderID: "provider", Name: "fixed-model", Protocol: domain.ChatCompletions}, APIKey: "deadline-desktop-key", MaxRequests: 1, MaxOutputTokens: 1, Timeout: time.Second}
	ledger := &deadlineLedger{}
	g := gateway.New(ledger)
	if _, err := g.Register(lease); err != nil {
		t.Fatal(err)
	}
	request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	if !ledger.bounded || ledger.contextErr != nil {
		t.Fatal("ledger finalization was unbounded or inherited cancellation")
	}
}

type deadlineLedger struct {
	bounded    bool
	contextErr error
}

func (l *deadlineLedger) BeginRequest(context.Context, domain.RequestRecord) error { return nil }
func (l *deadlineLedger) EndRequest(ctx context.Context, r domain.RequestRecord) error {
	deadline, ok := ctx.Deadline()
	remaining := time.Until(deadline)
	l.bounded = ok && remaining > 4*time.Second && remaining <= 5*time.Second
	l.contextErr = ctx.Err()
	return nil
}
