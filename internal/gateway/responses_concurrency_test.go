package gateway_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestConcurrentResponsePollsUpdateTheOriginalChargeOnlyOnce(t *testing.T) {
	g, ledger, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"id":"resp-concurrent","status":"queued","output":[],"usage":null}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"resp-concurrent","model":"fixed-model-final","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
	}, func(lease *gateway.Lease) {
		lease.MaxRequests = 1
		lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1", Source: "configured-estimate"}
	})
	request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model","background":true}`)
	original := ledger.records()[0]
	var work sync.WaitGroup
	for i := 0; i < 20; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			if w := request(t, g, lease.Token, "GET", "/v1/responses/resp-concurrent", ""); w.Code != 200 {
				t.Errorf("poll status=%d", w.Code)
			}
		}()
	}
	work.Wait()
	var originalUpdates, charges int
	for _, r := range ledger.records() {
		if r.ID == original.ID {
			originalUpdates++
		}
	}
	latest := latestRecords(ledger)
	for _, r := range latest {
		if r.CostAmount != nil {
			charges++
			if *r.CostAmount != "0.000005" {
				t.Fatalf("single response charge=%s", *r.CostAmount)
			}
		}
	}
	if originalUpdates != 2 || charges != 1 || len(latest) != 21 || latest[original.ID].ActualModel != "fixed-model-final" {
		t.Fatalf("concurrent recovery duplicated/dropped attribution: updates=%d charges=%d rows=%d origin=%+v", originalUpdates, charges, len(latest), latest[original.ID])
	}
}

func TestResponseRetrievalCanStreamWithoutDuplicatingUsage(t *testing.T) {
	stream := "event: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"resp-retrieval-stream","status":"completed","output":[],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}` + "\n\n"
	g, ledger, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"id":"resp-retrieval-stream","status":"queued","output":[],"usage":null}`)
			return
		}
		if r.URL.Query().Get("stream") != "true" || r.URL.Query().Get("starting_after") != "2" {
			t.Error("native streaming retrieval query lost")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}, func(lease *gateway.Lease) {
		lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1", Source: "configured-estimate"}
	})
	request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model","background":true}`)
	original := ledger.records()[0]
	w := request(t, g, lease.Token, "GET", "/v1/responses/resp-retrieval-stream?stream=true&starting_after=2", "")
	if w.Code != 200 || !w.Flushed || w.Body.String() != stream {
		t.Fatal("native retrieval stream changed")
	}
	latest := latestRecords(ledger)
	r := latest[original.ID]
	if r.CostAmount == nil || *r.CostAmount != "0.000006" || r.Usage.Completeness != "complete" {
		t.Fatalf("streamed retrieval was not attributed: %+v", r)
	}
	for id, r := range latest {
		if id != original.ID && (r.CostAmount != nil || r.Usage.TotalTokens != nil) {
			t.Fatal("retrieval stream duplicate charge")
		}
	}
}

func TestResponseIDCollisionsAndForeignPayloadIDsDoNotReassignOrigin(t *testing.T) {
	g, ledger, lease := fixture(t, domain.Responses, func(w http.ResponseWriter, r *http.Request) {
		id := "resp-shared"
		if r.Method == "GET" {
			id = "resp-foreign"
		}
		_, _ = io.WriteString(w, `{"id":"`+id+`","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
	}, func(lease *gateway.Lease) {
		lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1", Source: "configured-estimate"}
	})
	request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`)
	request(t, g, lease.Token, "POST", "/v1/responses", `{"model":"fixed-model"}`)
	collision := ledger.records()[1]
	if collision.ErrorClass != "response_id_collision" || collision.CostAmount != nil {
		t.Fatalf("native ID reassigned to another inference: %+v", collision)
	}
	request(t, g, lease.Token, "GET", "/v1/responses/resp-shared", "")
	followup := ledger.records()[2]
	if followup.ErrorClass != "response_id_mismatch" || followup.CostAmount != nil {
		t.Fatalf("foreign response payload was attributed: %+v", followup)
	}
	if w := request(t, g, lease.Token, "GET", "/v1/responses/resp-foreign", ""); w.Code != 403 {
		t.Fatal("foreign payload ID became authorized")
	}
	if strings.Contains(followup.ErrorClass, "resp-") {
		t.Fatal("raw payload logged as an error")
	}
}

func TestMalformedCumulativeSSEUsageNeverBecomesAnExactCost(t *testing.T) {
	for _, tc := range []struct {
		protocol      domain.Protocol
		path, payload string
	}{
		{domain.ChatCompletions, "/v1/chat/completions", `data: {"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":"invalid"}}` + "\n\ndata: [DONE]\n\n"},
		{domain.AnthropicMessages, "/v1/messages", "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg-invalid-usage","type":"message","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\nevent: message_delta\ndata: " + `{"type":"message_delta","usage":{"output_tokens":"invalid"}}` + "\n\nevent: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"},
	} {
		g, ledger, lease := fixture(t, tc.protocol, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, tc.payload)
		}, func(lease *gateway.Lease) {
			lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1", Source: "configured-estimate"}
		})
		request(t, g, lease.Token, "POST", tc.path, `{"model":"fixed-model","stream":true}`)
		r := ledger.records()[0]
		if r.Usage.Completeness != "partial" || r.CostAmount != nil {
			t.Fatalf("invalid cumulative native usage became exact: %+v", r)
		}
	}
}
