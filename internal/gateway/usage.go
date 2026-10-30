package gateway

import (
	"encoding/json"
	"math"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func observeJSON(data []byte, record *domain.RequestRecord, lease Lease) {
	fields, err := objectFields(data)
	if err != nil {
		if record.HTTPStatus != nil && *record.HTTPStatus >= 200 && *record.HTTPStatus < 300 {
			record.State, record.ErrorClass = "failed", "provider_protocol_error"
		}
		return
	}
	var value string
	if json.Unmarshal(fields["id"], &value) == nil {
		record.UpstreamResponseID = cleanMetadata(value, lease)
	}
	if json.Unmarshal(fields["model"], &value) == nil {
		record.ActualModel = cleanModelMetadata(value, lease)
	}
	if raw, exists := fields["error"]; exists && string(raw) != "null" {
		record.State, record.ErrorClass = "failed", "provider_protocol_error"
	}
	switch record.Protocol {
	case domain.ChatCompletions:
		if _, exists := fields["choices"]; !exists && record.ErrorClass == "" {
			record.State, record.ErrorClass = "failed", "provider_protocol_error"
		}
		observeChatUsage(fields["usage"], &record.Usage)
	case domain.Responses:
		if _, exists := fields["output"]; !exists && record.ErrorClass == "" {
			record.State, record.ErrorClass = "failed", "provider_protocol_error"
		}
		observeResponsesUsage(fields["usage"], &record.Usage)
		var status string
		_ = json.Unmarshal(fields["status"], &status)
		switch status {
		case "failed":
			record.State, record.ErrorClass = "failed", "provider_protocol_error"
		case "cancelled", "canceled":
			record.State, record.ErrorClass = "canceled", "provider_response_canceled"
		case "incomplete":
			record.State, record.ErrorClass = "failed", "provider_response_incomplete"
		case "queued", "in_progress":
			if record.Usage.Completeness != "unknown" {
				record.Usage.Completeness = "partial"
			}
		}
	case domain.AnthropicMessages:
		var kind string
		_ = json.Unmarshal(fields["type"], &kind)
		if kind != "message" && record.ErrorClass == "" {
			record.State, record.ErrorClass = "failed", "provider_protocol_error"
		}
		observeAnthropicUsage(fields["usage"], &record.Usage)
	}
}

func tokenValue(raw json.RawMessage) *int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < 0 {
		return nil
	}
	return &value
}

func sumTokens(a, b *int64) *int64 {
	if a == nil || b == nil || *a > math.MaxInt64-*b {
		return nil
	}
	value := *a + *b
	return &value
}

func observeChatUsage(raw json.RawMessage, usage *domain.Usage) {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return
	}
	usage.Source = "provider"
	usage.InputTokens = tokenValue(values["prompt_tokens"])
	usage.OutputTokens = tokenValue(values["completion_tokens"])
	usage.TotalTokens = tokenValue(values["total_tokens"])
	var inputDetails, outputDetails map[string]json.RawMessage
	_ = json.Unmarshal(values["prompt_tokens_details"], &inputDetails)
	_ = json.Unmarshal(values["completion_tokens_details"], &outputDetails)
	usage.CacheReadTokens = tokenValue(inputDetails["cached_tokens"])
	usage.ReasoningTokens = tokenValue(outputDetails["reasoning_tokens"])
	completeUsage(usage)
	if invalidUsagePayload(raw) {
		usage.Completeness = "partial"
	}
}

func observeResponsesUsage(raw json.RawMessage, usage *domain.Usage) {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return
	}
	usage.Source = "provider"
	usage.InputTokens = tokenValue(values["input_tokens"])
	usage.OutputTokens = tokenValue(values["output_tokens"])
	usage.TotalTokens = tokenValue(values["total_tokens"])
	var inputDetails, outputDetails map[string]json.RawMessage
	_ = json.Unmarshal(values["input_tokens_details"], &inputDetails)
	_ = json.Unmarshal(values["output_tokens_details"], &outputDetails)
	usage.CacheReadTokens = tokenValue(inputDetails["cached_tokens"])
	usage.ReasoningTokens = tokenValue(outputDetails["reasoning_tokens"])
	completeUsage(usage)
	if invalidUsagePayload(raw) {
		usage.Completeness = "partial"
	}
}

func observeAnthropicUsage(raw json.RawMessage, usage *domain.Usage) {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return
	}
	usage.Source = "provider"
	usage.CacheReadTokens = tokenValue(values["cache_read_input_tokens"])
	usage.CacheWriteTokens = anthropicCacheWrite(values)
	usage.OutputTokens = tokenValue(values["output_tokens"])
	completeAnthropicUsage(tokenValue(values["input_tokens"]), usage)
	if invalidUsagePayload(raw) {
		usage.Completeness = "partial"
	}
}

func anthropicCacheWrite(values map[string]json.RawMessage) *int64 {
	if aggregate := tokenValue(values["cache_creation_input_tokens"]); aggregate != nil {
		return aggregate
	}
	var creation map[string]json.RawMessage
	_ = json.Unmarshal(values["cache_creation"], &creation)
	return sumTokens(tokenValue(creation["ephemeral_5m_input_tokens"]), tokenValue(creation["ephemeral_1h_input_tokens"]))
}

func completeAnthropicUsage(uncached *int64, usage *domain.Usage) {
	// Anthropic excludes cache reads/writes from input_tokens; missing additive
	// counters leave inclusive input partial rather than inventing zero cache use.
	usage.InputTokens = uncached
	for _, cached := range []*int64{usage.CacheReadTokens, usage.CacheWriteTokens} {
		if cached != nil {
			usage.InputTokens = sumTokens(usage.InputTokens, cached)
		}
	}
	usage.TotalTokens = sumTokens(usage.InputTokens, usage.OutputTokens)
	completeUsage(usage)
	if usage.Completeness == "complete" && (usage.CacheReadTokens == nil || usage.CacheWriteTokens == nil) {
		usage.Completeness = "partial"
	}
}

func invalidUsagePayload(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	values, err := objectFields(raw)
	if err != nil {
		return true
	}
	if invalidTokens(values, "prompt_tokens", "completion_tokens", "total_tokens", "input_tokens", "output_tokens", "cached_tokens", "reasoning_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens") {
		return true
	}
	for _, key := range []string{"prompt_tokens_details", "completion_tokens_details", "input_tokens_details", "output_tokens_details", "cache_creation"} {
		if invalidUsagePayload(values[key]) {
			return true
		}
	}
	return false
}

func invalidTokens(values map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if raw, exists := values[key]; exists && string(raw) != "null" && tokenValue(raw) == nil {
			return true
		}
	}
	return false
}

func completeUsage(usage *domain.Usage) {
	if usage.TotalTokens == nil {
		usage.TotalTokens = sumTokens(usage.InputTokens, usage.OutputTokens)
	}
	if usage.InputTokens != nil && usage.OutputTokens != nil && usage.TotalTokens != nil {
		usage.Completeness = "complete"
	} else if usage.InputTokens != nil || usage.OutputTokens != nil || usage.CacheReadTokens != nil || usage.CacheWriteTokens != nil || usage.ReasoningTokens != nil || usage.TotalTokens != nil {
		usage.Completeness = "partial"
	} else {
		usage.Completeness = "unknown"
	}
	if inconsistentUsage(usage) {
		usage.Completeness = "partial"
	}
}

func inconsistentUsage(usage *domain.Usage) bool {
	if usage.InputTokens != nil && usage.OutputTokens != nil {
		sum := sumTokens(usage.InputTokens, usage.OutputTokens)
		if sum == nil || usage.TotalTokens != nil && *usage.TotalTokens != *sum {
			return true
		}
	}
	if usage.InputTokens != nil {
		var read, write int64
		if usage.CacheReadTokens != nil {
			read = *usage.CacheReadTokens
		}
		if usage.CacheWriteTokens != nil {
			write = *usage.CacheWriteTokens
		}
		if read > *usage.InputTokens || write > *usage.InputTokens-read {
			return true
		}
	}
	return usage.ReasoningTokens != nil && usage.OutputTokens != nil && *usage.ReasoningTokens > *usage.OutputTokens
}
