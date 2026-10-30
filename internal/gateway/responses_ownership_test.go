package gateway_test

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestResponseIDCannotBeReassignedAcrossLeasesForOneProvider(t *testing.T) {
	var calls atomic.Int32
	payload := `{"id":"resp-shared","output":[],"status":"completed","usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`
	g, ledger, first := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, payload)
	})
	second := first
	second.Token, second.TaskID, second.AttemptID = "second-token", "task-2", "attempt-2"
	if _, err := g.Register(second); err != nil {
		t.Fatal(err)
	}
	request(t, g, first.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`)
	w := request(t, g, second.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`)
	if w.Code != 200 || w.Body.String() != payload {
		t.Fatal("native collision response changed")
	}
	record := ledger.records()[1]
	if record.State != "failed" || record.ErrorClass != "response_id_collision" || record.Usage.Completeness != "partial" || record.CostAmount != nil {
		t.Fatalf("provider response ID reassigned to another lease: %+v", record)
	}
	w = request(t, g, second.Token, "GET", "/v1/responses/resp-shared", "")
	if w.Code != 403 || calls.Load() != 2 {
		t.Fatal("second lease retrieved the first lease's resource")
	}
	w = request(t, g, first.Token, "GET", "/v1/responses/resp-shared", "")
	if w.Code != 200 {
		t.Fatal("original response ownership lost")
	}
}
