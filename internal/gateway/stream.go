package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

const maxSSEEventBytes = 1 << 20

type streamObserver struct {
	record           *domain.RequestRecord
	lease            Lease
	line             []byte
	data             []byte
	event            string
	discardLine      bool
	discardEvent     bool
	limited          bool
	invalid          bool
	invalidUsage     bool
	terminal         bool
	usageTerminal    bool
	nativeUsageFinal bool
	onResponse       func(*domain.RequestRecord)
	skipLF           bool
	firstLineRead    bool
	anthropicInput   *int64
}

func (s *streamObserver) feed(chunk []byte) {
	for len(chunk) > 0 {
		if s.skipLF {
			s.skipLF = false
			if chunk[0] == '\n' {
				chunk = chunk[1:]
				continue
			}
		}
		end := bytes.IndexAny(chunk, "\r\n")
		part := chunk
		if end >= 0 {
			part = chunk[:end]
		}
		if !s.discardLine {
			if len(s.line)+len(part) > maxSSEEventBytes {
				s.line = nil
				s.discardLine, s.discardEvent, s.limited = true, true, true
			} else {
				s.line = append(s.line, part...)
			}
		}
		if end < 0 {
			return
		}
		if !s.firstLineRead {
			s.line = bytes.TrimPrefix(s.line, []byte{0xef, 0xbb, 0xbf})
			s.firstLineRead = true
		}
		if !s.discardLine {
			s.processLine(s.line)
		}
		s.line = s.line[:0]
		s.discardLine = false
		s.skipLF = chunk[end] == '\r'
		chunk = chunk[end+1:]
	}
}

func (s *streamObserver) processLine(line []byte) {
	if len(line) == 0 {
		if !s.discardEvent && len(s.data) > 0 {
			s.dispatch(bytes.TrimSuffix(s.data, []byte{'\n'}))
		}
		s.data = s.data[:0]
		s.event, s.discardEvent = "", false
		return
	}
	if line[0] == ':' || s.discardEvent {
		return
	}
	field, value, _ := bytes.Cut(line, []byte{':'})
	value = bytes.TrimPrefix(value, []byte{' '})
	switch string(field) {
	case "event":
		if len(value) > 256 {
			s.discardEvent, s.limited = true, true
			return
		}
		s.event = string(value)
	case "data":
		if len(s.data)+len(value)+1 > maxSSEEventBytes {
			s.data = nil
			s.discardEvent, s.limited = true, true
			return
		}
		s.data = append(s.data, value...)
		s.data = append(s.data, '\n')
	}
}

func (s *streamObserver) dispatch(data []byte) {
	if len(data) == 0 {
		return
	}
	if s.record.Protocol == domain.ChatCompletions && bytes.Equal(data, []byte("[DONE]")) {
		s.terminal, s.usageTerminal = true, true
		return
	}
	fields, err := objectFields(data)
	if err != nil {
		s.invalid = true
		return
	}
	var kind string
	_ = json.Unmarshal(fields["type"], &kind)
	if kind == "" {
		kind = s.event
	}
	if s.record.Protocol != domain.ChatCompletions && s.event != "" && kind != s.event {
		s.invalid = true
		return
	}
	if kind == "error" || nonNullError(fields) {
		s.record.State, s.record.ErrorClass, s.terminal = "failed", "provider_protocol_error", true
		return
	}
	switch s.record.Protocol {
	case domain.ChatCompletions:
		s.observeNative(data)
	case domain.Responses:
		if raw, exists := fields["response"]; exists && strings.HasPrefix(kind, "response.") {
			s.observeNative(raw)
		}
		switch kind {
		case "response.completed":
			s.terminal = true
		case "response.failed":
			s.terminal = true
			s.record.State, s.record.ErrorClass = "failed", "provider_protocol_error"
		case "response.incomplete":
			s.terminal = true
			s.record.State, s.record.ErrorClass = "failed", "provider_response_incomplete"
		case "response.cancelled", "response.canceled":
			s.terminal = true
			s.record.State, s.record.ErrorClass = "canceled", "provider_response_canceled"
		}
		if s.terminal {
			s.usageTerminal = len(fields["response"]) > 0 && s.nativeUsageFinal
		}
	case domain.AnthropicMessages:
		switch kind {
		case "message_start":
			message, err := objectFields(fields["message"])
			if err != nil {
				s.invalid = true
				return
			}
			s.observeNative(fields["message"])
			s.observeAnthropic(message["usage"])
		case "message_delta":
			s.observeAnthropic(fields["usage"])
		case "message_stop":
			s.terminal, s.usageTerminal = true, true
		}
	}
}

func nonNullError(fields map[string]json.RawMessage) bool {
	raw, exists := fields["error"]
	return exists && string(raw) != "null"
}

func (s *streamObserver) observeNative(data []byte) {
	observed := domain.RequestRecord{Protocol: s.record.Protocol, HTTPStatus: s.record.HTTPStatus, Usage: domain.Usage{Completeness: "unknown"}}
	observeJSON(data, &observed, s.lease)
	fields, _ := objectFields(data)
	if invalidUsagePayload(fields["usage"]) || inconsistentUsage(&observed.Usage) {
		s.invalidUsage = true
	}
	if observed.UpstreamResponseID != "" {
		s.record.UpstreamResponseID = observed.UpstreamResponseID
	}
	if observed.ActualModel != "" {
		s.record.ActualModel = observed.ActualModel
	}
	if observed.ErrorClass != "" {
		s.record.State, s.record.ErrorClass = observed.State, observed.ErrorClass
	}
	mergeUsage(&s.record.Usage, observed.Usage)
	s.nativeUsageFinal = observed.Usage.Completeness == "complete"
	if s.record.Protocol == domain.Responses && s.onResponse != nil {
		s.onResponse(s.record)
	}
}

func (s *streamObserver) observeAnthropic(raw json.RawMessage) {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		if len(raw) > 0 && string(raw) != "null" {
			s.invalidUsage = true
		}
		return
	}
	if invalidUsagePayload(raw) {
		s.invalidUsage = true
	}
	s.anthropicInput = maxToken(s.anthropicInput, tokenValue(values["input_tokens"]))
	usage := &s.record.Usage
	usage.Source = "provider"
	usage.CacheReadTokens = maxToken(usage.CacheReadTokens, tokenValue(values["cache_read_input_tokens"]))
	usage.CacheWriteTokens = maxToken(usage.CacheWriteTokens, anthropicCacheWrite(values))
	usage.OutputTokens = maxToken(usage.OutputTokens, tokenValue(values["output_tokens"]))
	completeAnthropicUsage(s.anthropicInput, usage)
}

func maxToken(a, b *int64) *int64 {
	if a == nil {
		return b
	}
	if b == nil || *a >= *b {
		return a
	}
	return b
}

func mergeUsage(usage *domain.Usage, observed domain.Usage) {
	usage.InputTokens = maxToken(usage.InputTokens, observed.InputTokens)
	usage.OutputTokens = maxToken(usage.OutputTokens, observed.OutputTokens)
	usage.CacheReadTokens = maxToken(usage.CacheReadTokens, observed.CacheReadTokens)
	usage.CacheWriteTokens = maxToken(usage.CacheWriteTokens, observed.CacheWriteTokens)
	usage.ReasoningTokens = maxToken(usage.ReasoningTokens, observed.ReasoningTokens)
	usage.TotalTokens = maxToken(usage.TotalTokens, observed.TotalTokens)
	if observed.Source != "" {
		usage.Source = observed.Source
	}
	completeUsage(usage)
}

func relayStream(w http.ResponseWriter, body io.Reader, record *domain.RequestRecord, lease Lease, started time.Time, onResponse func(*domain.RequestRecord)) error {
	observer := &streamObserver{record: record, lease: lease, onResponse: onResponse}
	buffer := make([]byte, 32<<10)
	var readErr error
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if record.FirstByteMS == nil {
				elapsed := time.Since(started).Milliseconds()
				record.FirstByteMS = &elapsed
			}
			observer.feed(buffer[:n])
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				readErr = writeErr
				break
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			readErr = err
			break
		}
	}
	if observer.limited {
		record.State, record.ErrorClass = "failed", "observation_limit"
	} else if observer.invalid {
		record.State, record.ErrorClass = "failed", "provider_protocol_error"
	} else if !observer.terminal || len(observer.line) > 0 || len(observer.data) > 0 {
		if record.ErrorClass == "" {
			record.State, record.ErrorClass = "failed", "upstream_truncated"
		}
	}
	if readErr != nil || observer.limited || observer.invalid || observer.invalidUsage || !observer.usageTerminal || len(observer.line) > 0 || len(observer.data) > 0 || record.ErrorClass == "response_id_collision" || record.ErrorClass == "response_id_mismatch" {
		if record.Usage.Completeness != "unknown" {
			record.Usage.Completeness = "partial"
		}
	}
	return readErr
}
