package gateway_test

import (
	"encoding/json"
	"errors"
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

func TestFixedProtocolModelAliasesAndBudgetDenials(t *testing.T) {
	var hits atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		var model string
		_ = json.Unmarshal(body["model"], &model)
		if model != "explicit-alias" {
			t.Errorf("model was silently switched to %s", model)
		}
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}, func(lease *gateway.Lease) { lease.AllowedModelNames = []string{"explicit-alias"} })
	for _, tc := range []struct {
		method, path, body string
		status             int
		class              string
	}{
		{"POST", "/v1/chat/completions", `{"model":"unapproved","max_tokens":1}`, 403, "model_not_allowed"},
		{"POST", "/v1/responses", `{"model":"fixed-model"}`, 403, "protocol_not_allowed"},
		{"POST", "/api/runs", `{}`, 404, "unsupported_path"},
		{"GET", "/v1/chat/completions", `{}`, 405, "unsupported_method"},
		{"POST", "/v1/chat/completions?model=elsewhere", `{"model":"fixed-model"}`, 400, "unsupported_query"},
		{"POST", "/v1/chat/completions", `{"model":"fixed-model","max_tokens":33}`, 429, "output_budget_exceeded"},
		{"POST", "/v1/chat/completions", `{"model":"fixed-model","max_completion_tokens":16,"max_tokens":33}`, 429, "output_budget_exceeded"},
		{"POST", "/v1/chat/completions", `{"model":"fixed-model","n":2,"max_tokens":17}`, 429, "output_budget_exceeded"},
		{"POST", "/v1/chat/completions", `{"model":"fixed-model","max_tokens":null}`, 400, "invalid_output_limit"},
		{"POST", "/v1/chat/completions", `{"model":"unapproved","model":"fixed-model"}`, 400, "invalid_json"},
		{"POST", "/v1/chat/completions", `{"model":"fixed-model"} {}`, 400, "invalid_json"},
	} {
		w := request(t, g, lease.Token, tc.method, tc.path, tc.body)
		if w.Code != tc.status {
			t.Fatalf("%s %s %s: status=%d body=%s", tc.method, tc.path, tc.body, w.Code, w.Body.String())
		}
		records := ledger.records()
		r := records[len(records)-1]
		if r.State != "denied" || r.ErrorClass != tc.class || r.FinishedAt == nil {
			t.Fatalf("denial not audited: %+v", r)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("denied requests reached provider")
	}
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"explicit-alias","max_tokens":4}`)
	if w.Code != 200 || hits.Load() != 1 {
		t.Fatalf("authorized alias rejected: %d", w.Code)
	}
}

func TestNativeOutputCeilingsAreInsertedAndEnforced(t *testing.T) {
	for _, tc := range []struct {
		protocol    domain.Protocol
		path, field string
	}{
		{domain.ChatCompletions, "/v1/chat/completions", "max_completion_tokens"},
		{domain.Responses, "/v1/responses", "max_output_tokens"},
		{domain.AnthropicMessages, "/v1/messages", "max_tokens"},
	} {
		t.Run(string(tc.protocol), func(t *testing.T) {
			var hits atomic.Int64
			g, ledger, lease := fixture(t, tc.protocol, func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				if string(body[tc.field]) != "32" {
					t.Errorf("native cap %s = %s", tc.field, body[tc.field])
				}
				_, _ = io.WriteString(w, `{"id":"native","type":"message","choices":[],"output":[],"content":[]}`)
			})
			if w := request(t, g, lease.Token, "POST", tc.path, `{"model":"fixed-model"}`); w.Code != 200 {
				t.Fatalf("missing cap = %d", w.Code)
			}
			if w := request(t, g, lease.Token, "POST", tc.path, `{"model":"fixed-model","`+tc.field+`":33}`); w.Code != 429 {
				t.Fatalf("over cap = %d", w.Code)
			}
			if hits.Load() != 1 || ledger.records()[1].State != "denied" {
				t.Fatal("native output denial was not isolated/audited")
			}
		})
	}
}

func TestRequestBudgetIsAtomicUnderConcurrency(t *testing.T) {
	var hits atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}, func(lease *gateway.Lease) { lease.MaxRequests = 3 })
	var work sync.WaitGroup
	var accepted atomic.Int64
	for i := 0; i < 30; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","max_tokens":2}`)
			if w.Code == 200 {
				accepted.Add(1)
			} else if w.Code != 429 {
				t.Errorf("unexpected concurrent status %d", w.Code)
			}
		}()
	}
	work.Wait()
	if hits.Load() != 3 || accepted.Load() != 3 || len(ledger.records()) != 30 {
		t.Fatalf("atomic budget: forwarded=%d accepted=%d ledger=%d", hits.Load(), accepted.Load(), len(ledger.records()))
	}
}

func TestLedgerAdmissionFailsClosedAndDoesNotSpendReservation(t *testing.T) {
	var hits atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = io.WriteString(w, `{"choices":[]}`) }, func(lease *gateway.Lease) { lease.MaxRequests = 1 })
	ledger.beginErr = errors.New("private database error")
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	if w.Code != 503 || hits.Load() != 0 || len(ledger.records()) != 0 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("admission failure forwarded or leaked")
	}
	ledger.beginErr = nil
	if w = request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`); w.Code != 200 || hits.Load() != 1 {
		t.Fatal("failed admission spent request reservation")
	}
}

func TestLedgerFinalizationFailureBlocksFurtherInference(t *testing.T) {
	var hits atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = io.WriteString(w, `{"choices":[]}`) })
	ledger.endErr = errors.New("private finalization error")
	request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
	if w.Code != 503 || hits.Load() != 1 || ledger.finalContextErr != nil {
		t.Fatal("failed finalization allowed new inference or used canceled context")
	}
}

func TestBodyLimitAndCredentialConflictNeverReachUpstream(t *testing.T) {
	var hits atomic.Int64
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","messages":[{"content":"`+strings.Repeat("x", (8<<20)+1)+`"}]}`)
	if w.Code != 413 || hits.Load() != 0 || len(ledger.records()) != 1 {
		t.Fatal("oversized body was forwarded or not audited")
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixed-model"}`))
	r.Header.Set("Authorization", "Bearer "+lease.Token)
	r.Header.Set("X-Api-Key", "other-attempt-token")
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 401 || hits.Load() != 0 || len(ledger.records()) != 1 {
		t.Fatal("conflicting credentials accepted or assigned to an attempt")
	}
}
