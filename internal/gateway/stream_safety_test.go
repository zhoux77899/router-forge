package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestSSEArrivesBeforeUpstreamCompletes(t *testing.T) {
	prefix := `data: {"id":"chatcmpl-live","model":"fixed-model","choices":[{"delta":{"content":"first"}}]}` + "\n\n"
	release := make(chan struct{})
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	server := httptest.NewServer(g)
	defer server.Close()
	defer close(release)
	r, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"fixed-model","stream":true}`))
	r.Header.Set("Authorization", "Bearer "+lease.Token)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatalf("first stream bytes were buffered: %v", err)
	}
	defer response.Body.Close()
	first := make([]byte, len(prefix))
	if _, err := io.ReadFull(response.Body, first); err != nil || string(first) != prefix {
		t.Fatalf("incremental bytes=%q err=%v", first, err)
	}
	if len(ledger.records()) != 0 {
		t.Fatal("inference finalized before native stream completion")
	}
}

func TestSSESupportsNativeLineEndingsBOMAndMultilineData(t *testing.T) {
	payload := "\xef\xbb\xbf: hello\r\revent: message\rdata: {\"id\":\"chatcmpl-lines\",\rdata: \"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\r\rdata: [DONE]\r\r"
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","stream":true}`)
	if w.Code != 200 || w.Body.String() != payload {
		t.Fatal("SSE framing bytes changed")
	}
	r := ledger.records()[0]
	if r.State != "completed" || r.Usage.Completeness != "complete" {
		t.Fatalf("valid native framing not recognized: %+v", r)
	}
	tokens(t, r.Usage.TotalTokens, 5)
}

func TestSSEUsageOmissionIsUnknownEvenWithNativeTerminal(t *testing.T) {
	payload := `data: {"id":"chatcmpl-no-usage","choices":[]}` + "\n\ndata: [DONE]\n\n"
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), `"stream_options":{"include_usage":true}`) {
			t.Errorf("native usage instrumentation missing: %s", data)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	})
	request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","stream":true}`)
	r := ledger.records()[0]
	if r.State != "completed" || r.Usage.Completeness != "unknown" || r.Usage.TotalTokens != nil || r.CostAmount != nil {
		t.Fatalf("missing streaming usage invented: %+v", r)
	}
}

func TestSSEParserBufferIsBoundedWithoutDroppingNativeBytes(t *testing.T) {
	payload := `data: {"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" + `data: {"choices":[{"delta":{"content":"` + strings.Repeat("x", 2<<20) + `"}}]}` + "\n\ndata: [DONE]\n\n"
	g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	})
	w := request(t, g, lease.Token, "POST", "/v1/chat/completions", `{"model":"fixed-model","stream":true}`)
	if w.Code != 200 || w.Body.String() != payload {
		t.Fatal("large native SSE was dropped or reserialized")
	}
	r := ledger.records()[0]
	if r.State != "failed" || r.ErrorClass != "observation_limit" || r.Usage.Completeness != "partial" || r.CostAmount != nil {
		t.Fatalf("oversized usage observation became exact: %+v", r)
	}
}

func TestNativeAnthropicStreamErrorsArePreservedAndRedactedFromLedger(t *testing.T) {
	payload := "event: error\ndata: " + `{"type":"error","error":{"type":"overloaded_error","message":"private provider detail"}}` + "\n\n"
	g, ledger, lease := fixture(t, domain.AnthropicMessages, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	})
	w := request(t, g, lease.Token, "POST", "/v1/messages", `{"model":"fixed-model","stream":true}`)
	if w.Code != 200 || w.Body.String() != payload {
		t.Fatal("native stream error changed")
	}
	r := ledger.records()[0]
	if r.State != "failed" || r.ErrorClass != "provider_protocol_error" || strings.Contains(r.ErrorClass, "private") || r.FinishedAt == nil {
		t.Fatalf("native stream error not finalized safely: %+v", r)
	}
}
