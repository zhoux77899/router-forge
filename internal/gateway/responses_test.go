package gateway_test

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func latestRecords(ledger *memoryLedger) map[string]domain.RequestRecord {
	latest := map[string]domain.RequestRecord{}
	for _, r := range ledger.records() {
		latest[r.ID] = r
	}
	return latest
}

func TestResponsesFollowupsAreLeaseScopedAndNeverChargedTwice(t *testing.T) {
	payload := `{"id":"resp-owned","object":"response","model":"fixed-model","status":"completed","output":[],"usage":{"input_tokens":6,"output_tokens":4,"total_tokens":10}}`
	var hits atomic.Int64
	g, ledger, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/v1/responses/resp-owned" && r.URL.Query().Get("include[]") != "reasoning.encrypted_content" {
			t.Error("safe native retrieval query not preserved")
		}
		_, _ = io.WriteString(w, payload)
	}, func(lease *gateway.Lease) {
		lease.MaxRequests = 1
		lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "2", Source: "configured-estimate"}
	})
	if w := request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`); w.Code != 200 {
		t.Fatal("create failed")
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/v1/responses/resp-owned?include%5B%5D=reasoning.encrypted_content", ""},
		{"POST", "/v1/responses/resp-owned/cancel", `{}`},
	} {
		w := request(t, g, lease.Token, tc.method, tc.path, tc.body)
		if w.Code != 200 || w.Body.String() != payload {
			t.Fatalf("authorized followup=%d %s", w.Code, w.Body.String())
		}
	}
	if w := request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`); w.Code != 429 {
		t.Fatal("followup reset inference budget")
	}
	other := lease
	other.Token, other.AttemptID = "other-attempt-token", "attempt-2"
	if _, err := g.Register(other); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{lease.Token, other.Token} {
		w := request(t, g, token, "GET", "/v1/responses/resp-unowned", "")
		if w.Code != 403 {
			t.Fatalf("unowned retrieval status=%d", w.Code)
		}
	}
	if w := request(t, g, other.Token, "GET", "/v1/responses/resp-owned", ""); w.Code != 403 {
		t.Fatal("response leaked across attempt lease")
	}
	if hits.Load() != 3 {
		t.Fatalf("unapproved followup forwarded; hits=%d", hits.Load())
	}
	var charged, followups int
	for _, r := range latestRecords(ledger) {
		if r.Kind == "inference" && r.State != "denied" {
			charged++
			if r.CostAmount == nil || *r.CostAmount != "0.000014" {
				t.Fatalf("origin charge=%+v", r)
			}
		}
		if r.Kind == "retrieve" || r.Kind == "cancel" {
			followups++
			if r.AttemptID != lease.AttemptID || r.Usage.InputTokens != nil || r.Usage.OutputTokens != nil || r.CostAmount != nil || r.Usage.Completeness != "not_applicable" || r.UpstreamResponseID != "resp-owned" {
				t.Fatalf("followup was re-billed or lost attribution: %+v", r)
			}
		}
	}
	if charged != 1 || followups != 2 {
		t.Fatalf("charged=%d followups=%d", charged, followups)
	}
}

func TestResponsesBackgroundUsageUpdatesOriginInsteadOfChargingPolls(t *testing.T) {
	var polls atomic.Int64
	g, ledger, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"id":"resp-background","model":"fixed-model","status":"queued","output":[],"usage":null}`)
			return
		}
		if polls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"id":"resp-background","model":"fixed-model","status":"in_progress","output":[],"usage":{"input_tokens":2,"output_tokens":0,"total_tokens":2}}`)
		} else {
			_, _ = io.WriteString(w, `{"id":"resp-background","model":"fixed-model","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
		}
	}, func(lease *gateway.Lease) {
		lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "2", Source: "configured-estimate"}
	})
	request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model","background":true}`)
	original := ledger.records()[0]
	if original.CostAmount != nil || original.Usage.Completeness != "unknown" {
		t.Fatal("queued response billed with unknown final usage")
	}
	for i := 0; i < 4; i++ {
		if w := request(t, g, lease.Token, "GET", "/v1/responses/resp-background", ""); w.Code != 200 {
			t.Fatalf("native poll=%d %s", w.Code, w.Body.String())
		}
		current := latestRecords(ledger)[original.ID]
		if i == 0 && (current.CostAmount != nil || current.Usage.Completeness != "partial") {
			t.Fatalf("in-progress counters treated as final: %+v", current)
		}
	}
	latest := latestRecords(ledger)
	origin := latest[original.ID]
	tokens(t, origin.Usage.InputTokens, 2)
	tokens(t, origin.Usage.OutputTokens, 3)
	if origin.CostAmount == nil || *origin.CostAmount != "0.000008" || origin.Usage.Completeness != "complete" || origin.Usage.Source != "provider_retrieve" || origin.StartedAt != original.StartedAt || *origin.FinishedAt != *original.FinishedAt || len(latest) != 5 {
		t.Fatalf("origin recovery was not attributed once: %+v", origin)
	}
	for id, r := range latest {
		if id != original.ID && (r.Kind != "retrieve" || r.CostAmount != nil || r.Usage.InputTokens != nil) {
			t.Fatalf("poll was charged: %+v", r)
		}
	}
}

func TestResponsesPreviousIDsAreScopedWithoutSilentlySwitchingModel(t *testing.T) {
	var hits atomic.Int64
	g, _, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		if hits.Load() == 2 && !strings.Contains(string(body), `"previous_response_id":"resp-previous"`) {
			t.Error("native previous response context changed")
		}
		id := "resp-previous"
		if hits.Load() == 2 {
			id = "resp-next"
		}
		_, _ = io.WriteString(w, `{"id":"`+id+`","model":"fixed-model","status":"completed","output":[],"usage":null}`)
	})
	if w := request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model","previous_response_id":"resp-other"}`); w.Code != 403 {
		t.Fatal("foreign previous response was accepted")
	}
	request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`)
	if w := request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model","previous_response_id":"resp-previous"}`); w.Code != 200 {
		t.Fatalf("owned previous response rejected %d", w.Code)
	}
	if hits.Load() != 2 {
		t.Fatal("foreign response created hidden inference")
	}
}
