package gateway_test

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestInferenceRequiresDurablyObservedAgentForExactLease(t *testing.T) {
	var calls atomic.Int32
	g, _, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"fixture","model":"fixed-model","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}, func(lease *gateway.Lease) { lease.RequireAgentObservation = true })
	input := `{"model":"fixed-model","messages":[{"role":"user","content":"fixture"}]}`
	if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", input); response.Code != http.StatusConflict || calls.Load() != 0 {
		t.Fatal("unobserved agent forwarded inference")
	}
	if err := g.AdmitObservedAgent(lease.Token, lease.RunID, "foreign-task", lease.AttemptID); err == nil {
		t.Fatal("foreign task observation admitted")
	}
	if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", input); response.Code != http.StatusConflict || calls.Load() != 0 {
		t.Fatal("foreign observation opened gate")
	}
	if err := g.AdmitObservedAgent(lease.Token, lease.RunID, lease.TaskID, lease.AttemptID); err != nil {
		t.Fatal(err)
	}
	if response := request(t, g, lease.Token, "POST", "/v1/chat/completions", input); response.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatal("durable observation did not admit exact lease", response.Code)
	}
	g.Revoke(lease.Token)
	if err := g.AdmitObservedAgent(lease.Token, lease.RunID, lease.TaskID, lease.AttemptID); err == nil {
		t.Fatal("revoked lease was reopened")
	}
}
