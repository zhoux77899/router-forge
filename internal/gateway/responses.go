package gateway

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type responseKey struct {
	provider, id string
}

type responseEntry struct {
	mu           sync.Mutex
	id           string
	record       domain.RequestRecord
	finalized    bool
	pending      domain.Usage
	pendingModel string
}

func ownedResponseContext(lease *leaseState, fields map[string]json.RawMessage) bool {
	if raw, exists := fields["conversation"]; exists && string(raw) != "null" {
		return false
	}
	raw, exists := fields["previous_response_id"]
	if !exists || string(raw) == "null" {
		return true
	}
	var id string
	if json.Unmarshal(raw, &id) != nil || !validResponseID(id) {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.responses[id] != nil
}

func (g *Gateway) bindResponse(lease *leaseState, origin *responseEntry, record *domain.RequestRecord) {
	id := record.UpstreamResponseID
	if id == "" {
		return
	}
	key := responseKey{provider: lease.lease.Provider.ID, id: id}
	g.mu.Lock()
	defer g.mu.Unlock()
	lease.mu.Lock()
	defer lease.mu.Unlock()
	owner := g.owners[key]
	if !validResponseID(id) || origin.id != "" && origin.id != id || lease.responses[id] != nil && lease.responses[id] != origin || owner != "" && owner != record.ID {
		record.State, record.ErrorClass = "failed", "response_id_collision"
		if record.Usage.Completeness != "unknown" {
			record.Usage.Completeness = "partial"
		}
		return
	}
	origin.id = id
	lease.responses[id] = origin
	g.owners[key] = record.ID
}

func (g *Gateway) finalizeOrigin(origin *responseEntry, record domain.RequestRecord, pricing *domain.Pricing, started time.Time) {
	origin.mu.Lock()
	defer origin.mu.Unlock()
	stampRecord(&record, started)
	applyRecoveredUsage(&record.Usage, origin.pending)
	if origin.pendingModel != "" {
		record.ActualModel = origin.pendingModel
	}
	priceRequest(&record, pricing)
	origin.record, origin.finalized = record, true
	g.end(record)
}

func (g *Gateway) recoverOrigin(origin *responseEntry, observed domain.RequestRecord, pricing *domain.Pricing) {
	if observed.Usage.Source == "provider" {
		observed.Usage.Source = "provider_" + observed.Kind
	}
	origin.mu.Lock()
	defer origin.mu.Unlock()
	if !origin.finalized {
		applyRecoveredUsage(&origin.pending, observed.Usage)
		if observed.ActualModel != "" {
			origin.pendingModel = observed.ActualModel
		}
		return
	}
	record := origin.record
	changed := applyRecoveredUsage(&record.Usage, observed.Usage)
	if observed.ActualModel != "" && (record.ActualModel == "" || observed.Usage.Completeness == "complete") && record.ActualModel != observed.ActualModel {
		record.ActualModel = observed.ActualModel
		changed = true
	}
	if !changed {
		return
	}
	priceRequest(&record, pricing)
	if g.end(record) {
		origin.record = record
	}
}

func applyRecoveredUsage(usage *domain.Usage, observed domain.Usage) bool {
	if observed.Completeness == "unknown" || observed.Source == "" {
		return false
	}
	before := *usage
	if usage.Completeness == "complete" {
		if observed.Completeness != "complete" {
			return false
		}
		if usage.CacheReadTokens == nil {
			usage.CacheReadTokens = observed.CacheReadTokens
		}
		if usage.CacheWriteTokens == nil {
			usage.CacheWriteTokens = observed.CacheWriteTokens
		}
		if usage.ReasoningTokens == nil {
			usage.ReasoningTokens = observed.ReasoningTokens
		}
		if reflect.DeepEqual(before, *usage) {
			return false
		}
	} else if observed.Completeness == "complete" {
		*usage = observed
	} else {
		mergeUsage(usage, observed)
		if usage.Completeness != "unknown" {
			usage.Completeness = "partial"
		}
	}
	complete := usage.Completeness
	completeUsage(usage)
	if complete == "partial" {
		usage.Completeness = "partial"
	}
	if observed.Source == "provider_cancel" {
		usage.Source = "provider_cancel"
	} else {
		usage.Source = "provider_retrieve"
	}
	return !reflect.DeepEqual(before, *usage)
}

func successfulHTTP(record domain.RequestRecord) bool {
	return record.HTTPStatus != nil && *record.HTTPStatus >= http.StatusOK && *record.HTTPStatus < http.StatusMultipleChoices
}
