package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func str(value string) *string { return &value }

func pricedFixture(t *testing.T, protocol domain.Protocol, pricing *domain.Pricing, payload string) (*gateway.Gateway, *memoryLedger, gateway.Lease) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, payload) }))
	t.Cleanup(upstream.Close)
	ledger := &memoryLedger{}
	lease := gateway.Lease{Token: "priced-attempt", RunID: "run-priced", TaskID: "task-priced", AttemptID: "attempt-priced", Provider: domain.Provider{ID: "priced-provider", BaseURL: upstream.URL}, Model: domain.Model{ID: "priced-model", ProviderID: "priced-provider", Name: "fixed-model", Protocol: protocol, Pricing: pricing}, APIKey: "priced-desktop-key", Network: desktopNetwork(), MaxRequests: 10, MaxOutputTokens: 32, Timeout: time.Second}
	g := gateway.New(ledger)
	t.Cleanup(func() { _ = g.Close() })
	if _, err := g.Register(lease); err != nil {
		t.Fatal(err)
	}
	return g, ledger, lease
}

func TestCostIsExactAndDoesNotDoubleCountCacheOrReasoning(t *testing.T) {
	cases := []struct {
		name                    string
		protocol                domain.Protocol
		path, payload, expected string
		pricing                 *domain.Pricing
	}{
		{"chat precision", domain.ChatCompletions, "/v1/chat/completions", `{"id":"chatcmpl-cost","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":9,"total_tokens":20,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":6}}}`, "0.00000980246912858024691276", &domain.Pricing{Currency: "USD", InputPerMillion: "0.1234567890123456789", OutputPerMillion: "0.9876543210987654321", CacheReadPerMillion: str("0.01234567890123456789"), Source: "configured-estimate"}},
		{"responses precision", domain.Responses, "/v1/responses", `{"id":"resp-cost","output":[],"status":"completed","usage":{"input_tokens":11,"output_tokens":9,"total_tokens":20,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":6}}}`, "0.00000980246912858024691276", &domain.Pricing{Currency: "EUR", InputPerMillion: "0.1234567890123456789", OutputPerMillion: "0.9876543210987654321", CacheReadPerMillion: str("0.01234567890123456789"), Source: "provider-published"}},
		{"anthropic inclusive cache", domain.AnthropicMessages, "/v1/messages", `{"id":"msg-cost","type":"message","content":[],"usage":{"input_tokens":3,"output_tokens":7,"cache_read_input_tokens":6,"cache_creation_input_tokens":4}}`, "0.00000302", &domain.Pricing{Currency: "CNY", InputPerMillion: "0.10", OutputPerMillion: "0.30", CacheReadPerMillion: str("0.02"), CacheWritePerMillion: str("0.125"), Source: "manual-estimate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, ledger, lease := pricedFixture(t, tc.protocol, tc.pricing, tc.payload)
			w := request(t, g, lease.Token, "POST", tc.path, `{"model":"fixed-model"}`)
			if w.Code != 200 {
				t.Fatalf("response = %d %s", w.Code, w.Body.String())
			}
			record := ledger.records()[0]
			if record.CostAmount == nil || *record.CostAmount != tc.expected || record.Currency == nil || *record.Currency != tc.pricing.Currency || record.CostSource != tc.pricing.Source {
				t.Fatalf("exact currency/source cost = %+v, want %s %s (%s)", record, tc.expected, tc.pricing.Currency, tc.pricing.Source)
			}
		})
	}
}

func TestUnknownPriceUsageAndCacheNeverBecomeExactZero(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pricing *domain.Pricing
		usage   string
	}{
		{"no price", nil, `{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`},
		{"no usage", &domain.Pricing{Currency: "USD", InputPerMillion: "0", OutputPerMillion: "0"}, `null`},
		{"invalid price", &domain.Pricing{Currency: "USD", InputPerMillion: "unknown", OutputPerMillion: "1"}, `{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`},
		{"missing currency", &domain.Pricing{InputPerMillion: "0", OutputPerMillion: "0"}, `{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`},
		{"unknown discounted cache", &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1", CacheReadPerMillion: str("0.1")}, `{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ledger, lease := pricedFixture(t, domain.ChatCompletions, tc.pricing, `{"id":"chatcmpl-unknown-cost","choices":[],"usage":`+tc.usage+`}`)
			request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
			if cost := ledger.records()[0].CostAmount; cost != nil {
				t.Fatalf("unknown became cost=%s", *cost)
			}
		})
	}
}

func TestExplicitKnownZeroRatesCanBeExactlyFree(t *testing.T) {
	g, ledger, lease := pricedFixture(t, domain.ChatCompletions, &domain.Pricing{Currency: "USD", InputPerMillion: "0", OutputPerMillion: "0", Source: "explicit-free-rate"}, `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`)
	request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	r := ledger.records()[0]
	if r.CostAmount == nil || *r.CostAmount != "0" || r.CostSource != "explicit-free-rate" {
		t.Fatalf("known zero rate = %+v", r)
	}
}
