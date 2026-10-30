package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func requestValues(r domain.RequestRecord) (map[string]any, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": r.ID, "run_id": r.RunID, "task_id": r.TaskID, "attempt_id": r.AttemptID, "provider_id": r.ProviderID, "model_id": r.ModelID,
		"protocol": string(r.Protocol), "kind": r.Kind, "requested_model": r.RequestedModel, "actual_model": r.ActualModel, "upstream_request_id": r.UpstreamRequestID, "upstream_response_id": r.UpstreamResponseID,
		"started_at": r.StartedAt, "finished_at": r.FinishedAt, "duration_ms": r.DurationMS, "first_byte_ms": r.FirstByteMS, "http_status": r.HTTPStatus, "state": r.State, "error_class": r.ErrorClass,
		"input_tokens": r.Usage.InputTokens, "output_tokens": r.Usage.OutputTokens, "cache_read_tokens": r.Usage.CacheReadTokens, "cache_write_tokens": r.Usage.CacheWriteTokens, "reasoning_tokens": r.Usage.ReasoningTokens, "total_tokens": r.Usage.TotalTokens, "usage_source": r.Usage.Source, "usage_completeness": r.Usage.Completeness,
		"cost_amount": r.CostAmount, "currency": r.Currency, "cost_source": r.CostSource, "record_json": string(b),
	}, nil
}
func (s *Store) BeginRequest(ctx context.Context, r domain.RequestRecord) error {
	var valid int
	if err := s.DB.GetContext(ctx, &valid, `SELECT count(*) FROM evaluation_attempts a JOIN evaluation_tasks t ON t.id=a.task_id JOIN models m ON m.id=? WHERE a.id=? AND a.run_id=? AND a.task_id=? AND t.run_id=? AND m.provider_id=?`, r.ModelID, r.AttemptID, r.RunID, r.TaskID, r.RunID, r.ProviderID); err != nil {
		return err
	}
	if valid != 1 {
		return fmt.Errorf("request is not bound to a valid run, task, attempt and provider")
	}
	values, err := requestValues(r)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	placeholders := make([]string, len(keys))
	for i, key := range keys {
		placeholders[i] = ":" + key
	}
	_, err = s.DB.NamedExecContext(ctx, "INSERT INTO llm_requests("+strings.Join(keys, ",")+") VALUES("+strings.Join(placeholders, ",")+")", values)
	return err
}
func (s *Store) EndRequest(ctx context.Context, r domain.RequestRecord) error {
	values, err := requestValues(r)
	if err != nil {
		return err
	}
	keys := []string{}
	for key := range values {
		if key != "id" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	sets := make([]string, len(keys))
	for i, key := range keys {
		sets[i] = key + "=:" + key
	}
	result, err := s.DB.NamedExecContext(ctx, "UPDATE llm_requests SET "+strings.Join(sets, ",")+" WHERE id=:id", values)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrNotFound
	}
	return nil
}
func (s *Store) Requests(ctx context.Context, runID, attemptID string, limit, offset int) (domain.Page[domain.RequestRecord], error) {
	out := domain.Page[domain.RequestRecord]{Items: []domain.RequestRecord{}}
	where := "1=1"
	args := []any{}
	if runID != "" {
		where += " AND run_id=?"
		args = append(args, runID)
	}
	if attemptID != "" {
		where += " AND attempt_id=?"
		args = append(args, attemptID)
	}
	if err := s.DB.GetContext(ctx, &out.Total, "SELECT count(*) FROM llm_requests WHERE "+where, args...); err != nil {
		return out, err
	}
	var rows []string
	args = append(args, limit, offset)
	if err := s.DB.SelectContext(ctx, &rows, "SELECT record_json FROM llm_requests WHERE "+where+" ORDER BY started_at DESC,id DESC LIMIT ? OFFSET ?", args...); err != nil {
		return out, err
	}
	for _, raw := range rows {
		var r domain.RequestRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return out, err
		}
		out.Items = append(out.Items, r)
	}
	return out, nil
}
func (s *Store) RequestSummary(ctx context.Context, runID string) (domain.RequestSummary, error) {
	out := domain.RequestSummary{Costs: []domain.CurrencyTotal{}}
	var rows []string
	if err := s.DB.SelectContext(ctx, &rows, "SELECT record_json FROM llm_requests WHERE run_id=?", runID); err != nil {
		return out, err
	}
	costs := map[string]*big.Rat{}
	estimated := map[string]bool{}
	add := func(dst *int64, v *int64) {
		if v != nil {
			*dst += *v
		}
	}
	for _, raw := range rows {
		var r domain.RequestRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return out, err
		}
		out.RequestCount++
		if r.Kind != "inference" {
			continue
		}
		out.InferenceCount++
		if r.Usage.Completeness != "complete" || r.Usage.InputTokens == nil || r.Usage.OutputTokens == nil {
			out.UnknownUsageCount++
		}
		add(&out.InputTokens, r.Usage.InputTokens)
		add(&out.OutputTokens, r.Usage.OutputTokens)
		add(&out.CacheReadTokens, r.Usage.CacheReadTokens)
		add(&out.CacheWriteTokens, r.Usage.CacheWriteTokens)
		add(&out.ReasoningTokens, r.Usage.ReasoningTokens)
		if r.CostAmount == nil || r.Currency == nil || *r.Currency == "" {
			out.UnknownCostCount++
			continue
		}
		amount, ok := new(big.Rat).SetString(*r.CostAmount)
		if !ok || amount.Sign() < 0 {
			return out, fmt.Errorf("invalid persisted decimal cost")
		}
		if costs[*r.Currency] == nil {
			costs[*r.Currency] = new(big.Rat)
		}
		costs[*r.Currency].Add(costs[*r.Currency], amount)
		if r.CostSource != "provider_reported" {
			estimated[*r.Currency] = true
		}
	}
	currencies := []string{}
	for currency := range costs {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	for _, currency := range currencies {
		amount := strings.TrimRight(strings.TrimRight(costs[currency].FloatString(18), "0"), ".")
		if amount == "" {
			amount = "0"
		}
		out.Costs = append(out.Costs, domain.CurrencyTotal{Currency: currency, Amount: amount, Estimated: estimated[currency]})
	}
	return out, nil
}
