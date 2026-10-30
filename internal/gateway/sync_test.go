package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestOtherSyncProtocolsPreserveNativeToolsAndUsage(t *testing.T) {
	cases := []struct {
		name                                   string
		protocol                               domain.Protocol
		path, input, output                    string
		inputTokens, outputTokens, totalTokens int64
	}{
		{"responses", domain.Responses, "/v1/responses",
			`{"model":"fixed-model","input":[{"type":"function_call_output","call_id":"call_1","output":"private tool result"}],"tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}],"max_output_tokens":32}`,
			`{"object":"response","id":"resp-native","model":"fixed-model","status":"completed","output":[{"type":"function_call","call_id":"call_2","name":"echo","arguments":"{\"text\":\"raw\"}"}],"usage":{"input_tokens":11,"output_tokens":9,"total_tokens":20,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":6}}}`,
			11, 9, 20},
		{"anthropic", domain.AnthropicMessages, "/v1/messages",
			`{"model":"fixed-model","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"private tool result"}]}],"tools":[{"name":"echo","input_schema":{"type":"object"}}],"max_tokens":32}`,
			`{"type":"message","id":"msg-native","model":"fixed-model","content":[{"type":"tool_use","id":"tool_2","name":"echo","input":{"text":"raw"}}],"stop_reason":"tool_use","usage":{"input_tokens":3,"output_tokens":7,"cache_creation_input_tokens":4,"cache_read_input_tokens":6}}`,
			13, 7, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, ledger, lease := fixture(t, tc.protocol, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("wrong native path: %s", r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != tc.input {
					t.Errorf("native tool request changed: %s", body)
				}
				if tc.protocol == domain.AnthropicMessages {
					if r.Header.Get("X-Api-Key") != "desktop-upstream-secret" || r.Header.Get("Authorization") != "" || r.Header.Get("Anthropic-Version") != "2023-06-01" {
						t.Error("incorrect Anthropic upstream headers")
					}
				} else if r.Header.Get("Authorization") != "Bearer desktop-upstream-secret" || r.Header.Get("X-Api-Key") != "" {
					t.Error("incorrect Responses upstream headers")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.output)
			})
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.input))
			r.Header.Set("X-Api-Key", lease.Token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != tc.output {
				t.Fatalf("native response = %d %s", w.Code, w.Body.String())
			}
			records := ledger.records()
			if len(records) != 1 {
				t.Fatalf("records = %d", len(records))
			}
			record := records[0]
			if record.State != "completed" || record.Protocol != tc.protocol {
				t.Fatalf("terminal attribution: %+v", record)
			}
			tokens(t, record.Usage.InputTokens, tc.inputTokens)
			tokens(t, record.Usage.OutputTokens, tc.outputTokens)
			tokens(t, record.Usage.TotalTokens, tc.totalTokens)
			if tc.protocol == domain.Responses {
				tokens(t, record.Usage.CacheReadTokens, 4)
				tokens(t, record.Usage.ReasoningTokens, 6)
			} else {
				tokens(t, record.Usage.CacheReadTokens, 6)
				tokens(t, record.Usage.CacheWriteTokens, 4)
			}
			if record.Usage.Completeness != "complete" {
				t.Fatalf("usage completeness: %+v", record.Usage)
			}
		})
	}
}

func TestMissingUsageStaysUnknownAndNeverFree(t *testing.T) {
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"chatcmpl-unknown","model":"fixed-model","choices":[]}`)
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","messages":[]}`)
	if w.Code != 200 {
		t.Fatalf("response = %d", w.Code)
	}
	record := ledger.records()[0]
	if record.Usage.InputTokens != nil || record.Usage.OutputTokens != nil || record.Usage.TotalTokens != nil || record.Usage.Completeness != "unknown" || record.CostAmount != nil {
		t.Fatalf("missing usage became known/free: %+v", record)
	}
}

func TestNativeProviderAndProtocolErrorsArePassedThroughWithoutPayloadInLedger(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			payload := `{"error":{"type":"rate_limit_error","message":"private provider error payload","code":"native-code"}}`
			g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, payload)
			})
			w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","messages":[],"max_tokens":4}`)
			if w.Code != status || w.Body.String() != payload || w.Header().Get("Retry-After") != "7" {
				t.Fatalf("native error changed: %d %s", w.Code, w.Body.String())
			}
			record := ledger.records()[0]
			if record.State != "failed" || record.ErrorClass == "" || strings.Contains(record.ErrorClass, "private") || record.CostAmount != nil {
				t.Fatalf("error ledger = %+v", record)
			}
		})
	}
}
