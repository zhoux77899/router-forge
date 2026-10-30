package gateway_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestNativeSSEBytesFlushAndCumulativeUsage(t *testing.T) {
	cases := []struct {
		name                      string
		protocol                  domain.Protocol
		path, input, payload      string
		outputTokens, totalTokens int64
	}{
		{"chat", domain.ChatCompletions, "/v1/chat/completions", `{"model":"fixed-model","messages":[],"stream":true,"stream_options":{"include_usage":true},"max_completion_tokens":16}`,
			": keepalive\r\n\r\n" +
				`data: {"id":"chatcmpl-stream","model":"fixed-model","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"echo","arguments":"{\"x\":"}}]}}]}` + "\r\n\r\n" +
				`data: {"id":"chatcmpl-stream","model":"fixed-model","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":2}}}` + "\r\n\r\ndata: [DONE]\r\n\r\n", 5, 17},
		{"responses", domain.Responses, "/v1/responses", `{"model":"fixed-model","input":[],"stream":true,"max_output_tokens":16}`,
			"event: response.created\ndata: " + `{"type":"response.created","response":{"object":"response","id":"resp-stream","model":"fixed-model","status":"in_progress","output":[],"usage":null}}` + "\n\n" +
				"event: response.function_call_arguments.delta\ndata: " + `{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"x\":"}` + "\n\n" +
				"event: response.completed\ndata: " + `{"type":"response.completed","response":{"object":"response","id":"resp-stream","model":"fixed-model","status":"completed","output":[],"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":2}}}}` + "\n\n", 5, 17},
		{"anthropic", domain.AnthropicMessages, "/v1/messages", `{"model":"fixed-model","messages":[],"stream":true,"max_tokens":16}`,
			"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg-stream","type":"message","model":"fixed-model","content":[],"usage":{"input_tokens":2,"output_tokens":1,"cache_creation_input_tokens":3,"cache_read_input_tokens":7}}}` + "\n\n" +
				"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}` + "\n\n" +
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":null},"usage":{"output_tokens":4}}` + "\n\n" +
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":null},"usage":{"output_tokens":6}}` + "\n\n" +
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}` + "\n\n" +
				"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n", 6, 18},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, ledger, lease := fixture(t, tc.protocol, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for i := 0; i < len(tc.payload); i += 7 {
					end := i + 7
					if end > len(tc.payload) {
						end = len(tc.payload)
					}
					_, _ = io.WriteString(w, tc.payload[i:end])
					w.(http.Flusher).Flush()
				}
			})
			w := request(t, g, lease.Token, "POST", tc.path, tc.input)
			if w.Code != 200 || w.Body.String() != tc.payload || !w.Flushed {
				t.Fatalf("native SSE was changed/buffered: status=%d flushed=%v bytes=%d", w.Code, w.Flushed, w.Body.Len())
			}
			record := ledger.records()[0]
			if record.State != "completed" || record.UpstreamResponseID == "" || record.ActualModel != "fixed-model" {
				t.Fatalf("SSE terminal metadata = %+v", record)
			}
			tokens(t, record.Usage.InputTokens, 12)
			tokens(t, record.Usage.OutputTokens, tc.outputTokens)
			tokens(t, record.Usage.TotalTokens, tc.totalTokens)
			if record.Usage.Completeness != "complete" {
				t.Fatalf("SSE usage = %+v", record.Usage)
			}
			if tc.protocol == domain.AnthropicMessages {
				tokens(t, record.Usage.CacheWriteTokens, 3)
				tokens(t, record.Usage.CacheReadTokens, 7)
			} else {
				tokens(t, record.Usage.CacheReadTokens, 4)
				tokens(t, record.Usage.ReasoningTokens, 2)
			}
		})
	}
}

func TestTruncatedSSELeavesPartialTerminalLedger(t *testing.T) {
	payload := `data: {"id":"chatcmpl-partial","model":"fixed-model","choices":[{"delta":{"content":"private partial output"}}],"usage":{"prompt_tokens":9,"completion_tokens":1,"total_tokens":10}}` + "\n\n"
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","messages":[],"stream":true}`)
	if w.Code != 200 || w.Body.String() != payload {
		t.Fatal("truncated native bytes changed")
	}
	record := ledger.records()[0]
	if record.State != "failed" || record.ErrorClass != "upstream_truncated" || record.Usage.Completeness != "partial" || record.CostAmount != nil || record.FinishedAt == nil {
		t.Fatalf("partial stream was billed as complete: %+v", record)
	}
	tokens(t, record.Usage.InputTokens, 9)
	tokens(t, record.Usage.OutputTokens, 1)
}
