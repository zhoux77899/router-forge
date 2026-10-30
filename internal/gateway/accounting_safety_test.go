package gateway_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func TestAnthropicInclusiveInputNeedsReportedCacheCounters(t *testing.T) {
	for _, usage := range []string{
		`{"input_tokens":3,"output_tokens":5}`,
		`{"input_tokens":3,"output_tokens":5,"cache_read_input_tokens":0}`,
		`{"input_tokens":3,"output_tokens":5,"cache_creation_input_tokens":0}`,
		`{"input_tokens":3,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":2}}`,
	} {
		t.Run(usage, func(t *testing.T) {
			payload := `{"type":"message","id":"msg-cache-unknown","content":[],"usage":` + usage + `}`
			g, ledger, lease := fixture(t, domain.AnthropicMessages, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, payload)
			}, func(lease *gateway.Lease) {
				lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "2"}
			})
			w := request(t, g, lease.Token, "POST", "/v1/messages", `{"model":"fixed-model"}`)
			if w.Code != 200 || w.Body.String() != payload {
				t.Fatal("native payload changed")
			}
			record := ledger.records()[0]
			if record.State != "completed" || record.Usage.Completeness != "partial" || record.CostAmount != nil {
				t.Fatalf("unknown additive cache input became exact: %+v", record)
			}
			tokens(t, record.Usage.OutputTokens, 5)
		})
	}
}

func TestAnthropicCacheTTLBreakdownIsCountedOnceInSyncAndSSE(t *testing.T) {
	for _, aggregate := range []string{"", `,"cache_creation_input_tokens":6`} {
		for _, stream := range []bool{false, true} {
			name := "sync" + aggregate
			if stream {
				name = "stream" + aggregate
			}
			t.Run(name, func(t *testing.T) {
				usage := `{"input_tokens":3,"output_tokens":5,"cache_read_input_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":4}` + aggregate + `}`
				payload := `{"type":"message","id":"msg-ttl","content":[],"usage":` + usage + `}`
				if stream {
					payload = "event: message_start\ndata: " + `{"type":"message_start","message":{"type":"message","id":"msg-ttl","content":[],"usage":{"input_tokens":3,"output_tokens":0,"cache_read_input_tokens":7}}}` + "\n\n" +
						"event: message_delta\ndata: " + `{"type":"message_delta","usage":` + usage + `}` + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
				}
				g, ledger, lease := fixture(t, domain.AnthropicMessages, func(w http.ResponseWriter, r *http.Request) {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					_, _ = io.WriteString(w, payload)
				}, func(lease *gateway.Lease) {
					lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "2", CacheReadPerMillion: str("0.25"), CacheWritePerMillion: str("1.5")}
				})
				body := `{"model":"fixed-model"}`
				if stream {
					body = `{"model":"fixed-model","stream":true}`
				}
				w := request(t, g, lease.Token, "POST", "/v1/messages", body)
				if w.Code != 200 || w.Body.String() != payload {
					t.Fatal("native TTL payload changed")
				}
				record := ledger.records()[0]
				if record.State != "completed" || record.Usage.Completeness != "complete" || record.CostAmount == nil || *record.CostAmount != "0.00002375" {
					t.Fatalf("TTL cache accounting incorrect: %+v", record)
				}
				tokens(t, record.Usage.InputTokens, 16)
				tokens(t, record.Usage.CacheWriteTokens, 6)
				tokens(t, record.Usage.TotalTokens, 21)
			})
		}
	}
}

func TestMalformedSyncUsageCannotBecomeExactCost(t *testing.T) {
	for _, usage := range []string{
		`{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"prompt_tokens_details":[]}`,
		`{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":"invalid"}}`,
		`{"prompt_tokens":3,"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}`,
	} {
		t.Run(usage, func(t *testing.T) {
			g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"choices":[],"usage":`+usage+`}`)
			}, func(lease *gateway.Lease) {
				lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "2"}
			})
			request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model"}`)
			record := ledger.records()[0]
			if record.Usage.Completeness != "partial" || record.CostAmount != nil {
				t.Fatalf("malformed native usage became exact: %+v", record)
			}
		})
	}
}

func TestUnfinishedSSEAfterNativeTerminalRetainsOnlyPartialUsage(t *testing.T) {
	payload := `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}` + "\n\ndata: [DONE]\n\ndata: {"
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	}, func(lease *gateway.Lease) {
		lease.Model.Pricing = &domain.Pricing{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "2"}
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","stream":true}`)
	if w.Body.String() != payload {
		t.Fatal("unfinished bytes changed")
	}
	record := ledger.records()[0]
	if record.State != "failed" || record.ErrorClass != "upstream_truncated" || record.Usage.Completeness != "partial" || record.CostAmount != nil {
		t.Fatalf("unfinished event became exact: %+v", record)
	}
	tokens(t, record.Usage.TotalTokens, 8)
}
