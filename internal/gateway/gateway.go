package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/zhoux77899/router-forge/internal/domain"
)

const (
	maxRequestBytes     = 8 << 20
	maxObservationBytes = 8 << 20
	finalizationTimeout = 5 * time.Second
)

// Ledger durably admits requests and replaces terminal records by request ID.
// EndRequest must support idempotent corrections for deferred Responses usage.
type Ledger interface {
	BeginRequest(context.Context, domain.RequestRecord) error
	EndRequest(context.Context, domain.RequestRecord) error
}

// Lease binds a temporary worker credential to one immutable attempt/model.
type Lease struct {
	Token                   string `json:"-"`
	RunID                   string
	TaskID                  string
	AttemptID               string
	Provider                domain.Provider
	Model                   domain.Model
	APIKey                  string                  `json:"-"`
	Network                 *domain.NetworkProfile  `json:"-"`
	ProxyCredential         *domain.ProxyCredential `json:"-"`
	MaxRequests             int
	MaxOutputTokens         int
	Timeout                 time.Duration
	AllowedModelNames       []string
	RequireAgentObservation bool
}

// Gateway implements the three native inference HTTP protocols without conversion.
type Gateway struct {
	ledger Ledger
	mu     sync.Mutex
	leases map[string]*leaseState
	owners map[responseKey]string
	broken bool
	closed bool
}

type leaseState struct {
	lease         Lease
	client        *http.Client
	base          *url.URL
	names         map[string]bool
	mu            sync.Mutex
	used          int
	revoked       bool
	agentObserved bool
	active        map[string]context.CancelFunc
	responses     map[string]*responseEntry
}

// New returns a fail-closed gateway backed by the supplied ledger.
func New(ledger Ledger) *Gateway {
	return &Gateway{
		ledger: ledger,
		leases: make(map[string]*leaseState),
		owners: make(map[responseKey]string),
	}
}

// Register snapshots a validated lease, including its explicit Desktop network
// profile and private proxy credential, without making an upstream request.
// An omitted token is generated from 256 bits of cryptographic randomness.
func (g *Gateway) Register(lease Lease) (string, error) {
	if lease.RunID == "" || lease.TaskID == "" || lease.AttemptID == "" || lease.Provider.ID == "" || lease.Model.ID == "" || !validModelName(lease.Model.Name) || lease.Model.ProviderID != lease.Provider.ID || !lease.Model.Protocol.Valid() {
		return "", errors.New("complete attempt, provider, and fixed model attribution is required")
	}
	if lease.MaxRequests < 1 || lease.MaxOutputTokens < 1 || lease.Timeout <= 0 {
		return "", errors.New("positive request, output token, and timeout limits are required")
	}
	if !validCredential(lease.APIKey) {
		return "", errors.New("invalid upstream credential")
	}
	base, err := providerURL(lease.Provider.BaseURL)
	if err != nil {
		return "", err
	}
	if lease.Token == "" {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", errors.New("unable to generate lease credential")
		}
		lease.Token = base64.RawURLEncoding.EncodeToString(token[:])
	}
	if !validCredential(lease.Token) {
		return "", errors.New("invalid lease credential")
	}
	lease.AllowedModelNames = append([]string(nil), lease.AllowedModelNames...)
	if lease.Model.Pricing != nil {
		pricing := *lease.Model.Pricing
		if pricing.CacheReadPerMillion != nil {
			value := *pricing.CacheReadPerMillion
			pricing.CacheReadPerMillion = &value
		}
		if pricing.CacheWritePerMillion != nil {
			value := *pricing.CacheWritePerMillion
			pricing.CacheWritePerMillion = &value
		}
		lease.Model.Pricing = &pricing
	}
	names := map[string]bool{lease.Model.Name: true}
	for _, name := range lease.AllowedModelNames {
		if !validModelName(name) {
			return "", errors.New("invalid model alias")
		}
		names[name] = true
	}
	client, err := freezeNetwork(&lease)
	if err != nil {
		return "", err
	}
	registered := false
	defer func() {
		if !registered {
			client.CloseIdleConnections()
		}
	}()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return "", errors.New("gateway is closed")
	}
	if g.broken {
		return "", errors.New("ledger finalization is unavailable")
	}
	if _, exists := g.leases[lease.Token]; exists {
		return "", errors.New("lease credential already registered")
	}
	g.leases[lease.Token] = &leaseState{lease: lease, client: client, base: base, names: names, agentObserved: !lease.RequireAgentObservation, active: make(map[string]context.CancelFunc), responses: make(map[string]*responseEntry)}
	registered = true
	return lease.Token, nil
}

func (g *Gateway) AdmitObservedAgent(token, runID, taskID, attemptID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	lease := g.leases[token]
	if lease == nil || lease.lease.RunID != runID || lease.lease.TaskID != taskID || lease.lease.AttemptID != attemptID {
		return errors.New("observed agent does not belong to the active gateway lease")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.revoked {
		return errors.New("gateway lease is revoked")
	}
	lease.agentObserved = true
	return nil
}

// Revoke invalidates a lease token and cancels every active call using it.
func (g *Gateway) Revoke(token string) {
	g.mu.Lock()
	lease := g.leases[token]
	delete(g.leases, token)
	g.mu.Unlock()
	if lease == nil {
		return
	}
	lease.close()
}

// Close permanently stops admission, revokes all leases, and releases their
// private transports. Active requests still finalize their ledger records.
func (g *Gateway) Close() error {
	g.mu.Lock()
	g.closed = true
	leases := make([]*leaseState, 0, len(g.leases))
	for token, lease := range g.leases {
		leases = append(leases, lease)
		delete(g.leases, token)
	}
	g.mu.Unlock()
	for _, lease := range leases {
		lease.close()
	}
	return nil
}

func validCredential(value string) bool {
	return value != "" && len(value) <= 4096 && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func providerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawPath != "" {
		return nil, errors.New("provider requires an absolute HTTP(S) URL without credentials or fragment")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, errors.New("invalid provider URL path")
		}
	}
	if strings.Contains(strings.TrimRight(u.Path, "/"), "//") {
		return nil, errors.New("invalid provider URL path")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("invalid provider URL query")
	}
	for key, values := range query {
		if key != "api-version" || len(values) != 1 || !safeIdentifier(values[0], 128) {
			return nil, errors.New("only a safe api-version provider query is permitted")
		}
	}
	u.RawQuery = query.Encode()
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func safeIdentifier(value string, limit int) bool {
	if len(value) == 0 || len(value) > limit {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.:-", c)) {
			return false
		}
	}
	return true
}

func (g *Gateway) authenticate(r *http.Request) *leaseState {
	var bearer string
	if values := r.Header.Values("Authorization"); len(values) > 0 {
		if len(values) != 1 {
			return nil
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return nil
		}
		bearer = parts[1]
	}
	keys := r.Header.Values("X-Api-Key")
	if len(keys) > 1 {
		return nil
	}
	key := r.Header.Get("X-Api-Key")
	if bearer != "" && key != "" && subtle.ConstantTimeCompare([]byte(bearer), []byte(key)) != 1 {
		return nil
	}
	if bearer == "" {
		bearer = key
	}
	if !validCredential(bearer) {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.leases[bearer]
}

func newRecord(lease Lease, started time.Time) domain.RequestRecord {
	return domain.RequestRecord{
		ID: domain.NewID("req"), RunID: lease.RunID, TaskID: lease.TaskID, AttemptID: lease.AttemptID,
		ProviderID: lease.Provider.ID, ModelID: lease.Model.ID, Protocol: lease.Model.Protocol,
		Kind: "inference", StartedAt: started.UTC().Format(time.RFC3339Nano), State: "running",
		Usage: domain.Usage{Source: "unknown", Completeness: "unknown"}, CostSource: "unknown",
	}
}

// ServeHTTP authenticates, admits, and forwards only the lease's native routes.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lease := g.authenticate(r)
	if lease == nil {
		protocol := domain.ChatCompletions
		if target, _, _ := nativeRoute(r); target.protocol.Valid() {
			protocol = target.protocol
		}
		nativeError(w, protocol, http.StatusUnauthorized, "invalid_lease")
		return
	}
	started := time.Now()
	record := newRecord(lease.lease, started)
	deadline := started.Add(lease.lease.Timeout)
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	r = r.WithContext(ctx)
	defer boundRequestIO(ctx, cancel, w, r, deadline)()
	g.mu.Lock()
	if g.broken || g.closed {
		class := "ledger_unavailable"
		if g.closed {
			class = "gateway_closed"
		}
		g.mu.Unlock()
		nativeError(w, record.Protocol, http.StatusServiceUnavailable, class)
		return
	}
	lease.mu.Lock()
	if lease.revoked {
		lease.mu.Unlock()
		g.mu.Unlock()
		g.deny(w, r, lease, record, started, http.StatusForbidden, "lease_revoked")
		return
	}
	if !lease.agentObserved {
		lease.mu.Unlock()
		g.mu.Unlock()
		g.deny(w, r, lease, record, started, http.StatusConflict, "agent_version_not_observed")
		return
	}
	lease.active[record.ID] = cancel
	lease.mu.Unlock()
	g.mu.Unlock()
	defer func() { lease.mu.Lock(); delete(lease.active, record.ID); lease.mu.Unlock() }()
	target, status, class := nativeRoute(r)
	if target.kind != "" {
		record.Kind = target.kind
	}
	if status == 0 && target.protocol != lease.lease.Model.Protocol {
		status, class = http.StatusForbidden, "protocol_not_allowed"
	}
	if status != 0 {
		g.deny(w, r, lease, record, started, status, class)
		return
	}
	var followup *responseEntry
	if target.responseID != "" {
		lease.mu.Lock()
		followup = lease.responses[target.responseID]
		lease.mu.Unlock()
		if followup == nil {
			g.deny(w, r, lease, record, started, http.StatusForbidden, "response_not_owned")
			return
		}
		followup.mu.Lock()
		record.RequestedModel = followup.record.RequestedModel
		followup.mu.Unlock()
		record.UpstreamResponseID = target.responseID
	}
	body, fields, status, class := readNativeInput(r, target.kind)
	if status != 0 {
		g.deny(w, r, lease, record, started, status, class)
		return
	}
	if target.kind == "inference" {
		if err := json.Unmarshal(fields["model"], &record.RequestedModel); err != nil || !lease.names[record.RequestedModel] {
			record.RequestedModel = cleanModelMetadata(record.RequestedModel, lease.lease)
			g.deny(w, r, lease, record, started, http.StatusForbidden, "model_not_allowed")
			return
		}
		if record.Protocol == domain.Responses && !ownedResponseContext(lease, fields) {
			g.deny(w, r, lease, record, started, http.StatusForbidden, "response_not_owned")
			return
		}
		body, status, class = capOutput(body, fields, lease.lease)
		if status != 0 {
			g.deny(w, r, lease, record, started, status, class)
			return
		}
		record.RequestedModel = cleanModelMetadata(record.RequestedModel, lease.lease)
	}

	g.mu.Lock()
	if g.broken || g.closed {
		class := "ledger_unavailable"
		if g.closed {
			class = "gateway_closed"
		}
		g.mu.Unlock()
		nativeError(w, record.Protocol, http.StatusServiceUnavailable, class)
		return
	}
	lease.mu.Lock()
	if lease.revoked {
		lease.mu.Unlock()
		g.mu.Unlock()
		g.deny(w, r, lease, record, started, http.StatusForbidden, "lease_revoked")
		return
	}
	if target.kind == "inference" && lease.used >= lease.lease.MaxRequests {
		lease.mu.Unlock()
		g.mu.Unlock()
		g.deny(w, r, lease, record, started, http.StatusTooManyRequests, "request_budget_exceeded")
		return
	}
	if target.kind == "inference" {
		lease.used++
	}
	lease.mu.Unlock()
	g.mu.Unlock()
	if err := g.begin(ctx, record); err != nil {
		lease.mu.Lock()
		if target.kind == "inference" {
			lease.used--
		}
		lease.mu.Unlock()
		nativeError(w, record.Protocol, http.StatusServiceUnavailable, "ledger_unavailable")
		return
	}
	var origin *responseEntry
	if record.Protocol == domain.Responses && target.kind == "inference" {
		origin = &responseEntry{record: record}
	}
	defer func() {
		if origin != nil {
			g.finalizeOrigin(origin, record, lease.lease.Model.Pricing, started)
		} else if followup != nil {
			if successfulHTTP(record) && record.UpstreamResponseID == target.responseID {
				g.recoverOrigin(followup, record, lease.lease.Model.Pricing)
			}
			record.Usage, record.CostSource = domain.Usage{Source: "followup", Completeness: "not_applicable"}, "not_applicable"
			record.CostAmount, record.Currency = nil, nil
			g.finish(record, started)
		} else {
			priceRequest(&record, lease.lease.Model.Pricing)
			g.finish(record, started)
		}
	}()
	onResponse := func(observed *domain.RequestRecord) {
		if origin != nil {
			g.bindResponse(lease, origin, observed)
		}
		if followup != nil && observed.UpstreamResponseID != target.responseID {
			observed.State, observed.ErrorClass = "failed", "response_id_mismatch"
			if observed.Usage.Completeness != "unknown" {
				observed.Usage.Completeness = "partial"
			}
		}
	}

	upstreamURL := *lease.base
	if strings.HasSuffix(upstreamURL.Path, "/v1") {
		upstreamURL.Path += strings.TrimPrefix(target.path, "/v1")
	} else {
		upstreamURL.Path += target.path
	}
	query := upstreamURL.Query()
	for key, values := range target.query {
		query[key] = append([]string(nil), values...)
	}
	upstreamURL.RawQuery = query.Encode()
	upstream, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		record.State, record.ErrorClass = "failed", "invalid_upstream_request"
		nativeError(w, record.Protocol, http.StatusBadGateway, record.ErrorClass)
		return
	}
	upstream.GetBody = nil
	copyRequestHeaders(upstream.Header, r.Header, lease.lease)
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept-Encoding", "identity")
	if record.Protocol == domain.AnthropicMessages {
		upstream.Header.Set("X-Api-Key", lease.lease.APIKey)
		if upstream.Header.Get("Anthropic-Version") == "" {
			upstream.Header.Set("Anthropic-Version", "2023-06-01")
		}
	} else {
		upstream.Header.Set("Authorization", "Bearer "+lease.lease.APIKey)
	}
	response, err := lease.client.Do(upstream)
	if err != nil {
		record.State, record.ErrorClass, status = transportFailure(ctx)
		record.HTTPStatus = &status
		nativeError(w, record.Protocol, status, record.ErrorClass)
		return
	}
	defer response.Body.Close()
	record.HTTPStatus = &response.StatusCode
	record.UpstreamRequestID = cleanMetadata(response.Header.Get("X-Request-Id"), lease.lease)
	if record.UpstreamRequestID == "" {
		record.UpstreamRequestID = cleanMetadata(response.Header.Get("Request-Id"), lease.lease)
	}
	copyResponseHeaders(w.Header(), response.Header, lease.lease)
	w.WriteHeader(response.StatusCode)
	var readErr error
	var oversized bool
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaType == "text/event-stream" {
		readErr = relayStream(w, response.Body, &record, lease.lease, started, onResponse)
	} else {
		var data []byte
		data, readErr, oversized = relaySync(w, response.Body, &record, started)
		if !oversized {
			observeJSON(data, &record, lease.lease)
			onResponse(&record)
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		record.State, record.ErrorClass = "failed", "provider_http_error"
	} else if record.ErrorClass == "" {
		record.State = "completed"
	}
	if oversized && response.StatusCode >= 200 && response.StatusCode < 300 {
		record.State, record.ErrorClass = "failed", "observation_limit"
		record.Usage.Completeness = "partial"
	}
	if readErr != nil {
		record.State, record.ErrorClass, _ = transportFailure(ctx)
		if ctx.Err() == nil {
			record.ErrorClass = "upstream_truncated"
		}
		if record.Usage.Completeness != "unknown" {
			record.Usage.Completeness = "partial"
		}
	}
}

func readNativeInput(r *http.Request, kind string) ([]byte, map[string]json.RawMessage, int, string) {
	if r.ContentLength > maxRequestBytes {
		return nil, nil, http.StatusRequestEntityTooLarge, "body_too_large"
	}
	if encoding := r.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil, nil, http.StatusUnsupportedMediaType, "unsupported_encoding"
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			return nil, nil, http.StatusUnsupportedMediaType, "invalid_content_type"
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return nil, nil, http.StatusBadRequest, "invalid_body"
	}
	if len(body) > maxRequestBytes {
		return nil, nil, http.StatusRequestEntityTooLarge, "body_too_large"
	}
	if kind != "inference" && len(body) == 0 {
		return body, map[string]json.RawMessage{}, 0, ""
	}
	if kind == "retrieve" {
		return nil, nil, http.StatusBadRequest, "unexpected_body"
	}
	fields, err := objectFields(body)
	if err != nil {
		return nil, nil, http.StatusBadRequest, "invalid_json"
	}
	if kind != "inference" && len(fields) > 0 {
		return nil, nil, http.StatusBadRequest, "unexpected_body"
	}
	return body, fields, 0, ""
}

func objectFields(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid JSON key")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate JSON field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}
	return fields, nil
}

func positiveInteger(raw json.RawMessage) (int64, bool) {
	var value int64
	err := json.Unmarshal(raw, &value)
	return value, err == nil && value > 0
}

func capOutput(body []byte, fields map[string]json.RawMessage, lease Lease) ([]byte, int, string) {
	if raw, exists := fields["stream"]; exists {
		var stream bool
		if string(raw) == "null" || json.Unmarshal(raw, &stream) != nil {
			return nil, http.StatusBadRequest, "invalid_stream"
		}
	}
	keys := []string{"max_completion_tokens", "max_tokens"}
	defaultKey := "max_completion_tokens"
	choices := int64(1)
	switch lease.Model.Protocol {
	case domain.Responses:
		keys, defaultKey = []string{"max_output_tokens"}, "max_output_tokens"
	case domain.AnthropicMessages:
		keys, defaultKey = []string{"max_tokens"}, "max_tokens"
	case domain.ChatCompletions:
		if raw, exists := fields["n"]; exists {
			var ok bool
			choices, ok = positiveInteger(raw)
			if !ok {
				return nil, http.StatusBadRequest, "invalid_output_limit"
			}
		}
	}
	cap := int64(lease.MaxOutputTokens) / choices
	if cap < 1 {
		return nil, http.StatusTooManyRequests, "output_budget_exceeded"
	}
	present := false
	for _, key := range keys {
		if raw, exists := fields[key]; exists {
			present = true
			value, valid := positiveInteger(raw)
			if !valid {
				return nil, http.StatusBadRequest, "invalid_output_limit"
			}
			if value > cap {
				return nil, http.StatusTooManyRequests, "output_budget_exceeded"
			}
		}
	}
	if !present {
		body = addField(body, defaultKey, []byte(strconv.FormatInt(cap, 10)))
	}
	if lease.Model.Protocol == domain.ChatCompletions && bytes.Equal(bytes.TrimSpace(fields["stream"]), []byte("true")) {
		if _, exists := fields["stream_options"]; !exists {
			body = addField(body, "stream_options", []byte(`{"include_usage":true}`))
		}
	}
	return body, 0, ""
}

func addField(body []byte, name string, value []byte) []byte {
	end := bytes.LastIndexByte(body, '}')
	prefix := bytes.TrimSpace(body[:end])
	comma := ","
	if bytes.Equal(prefix, []byte("{")) {
		comma = ""
	}
	added := append([]byte(nil), body[:end]...)
	added = append(added, comma+`"`+name+`":`...)
	added = append(added, value...)
	return append(added, body[end:]...)
}

func (g *Gateway) begin(ctx context.Context, record domain.RequestRecord) error {
	if g.ledger == nil {
		return errors.New("ledger unavailable")
	}
	return g.ledger.BeginRequest(ctx, record)
}

func (g *Gateway) deny(w http.ResponseWriter, r *http.Request, lease *leaseState, record domain.RequestRecord, started time.Time, status int, class string) {
	record.Kind = "denied"
	state := "denied"
	if r.Context().Err() != nil {
		state, class, status = transportFailure(r.Context())
	}
	ctx, cancel := context.WithTimeout(context.Background(), finalizationTimeout)
	defer cancel()
	if err := g.begin(ctx, record); err != nil {
		nativeError(w, record.Protocol, http.StatusServiceUnavailable, "ledger_unavailable")
		return
	}
	record.State, record.ErrorClass, record.HTTPStatus = state, class, &status
	if !g.finish(record, started) {
		nativeError(w, record.Protocol, http.StatusServiceUnavailable, "ledger_unavailable")
		return
	}
	nativeError(w, record.Protocol, status, class)
}

func stampRecord(record *domain.RequestRecord, started time.Time) {
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	duration := time.Since(started).Milliseconds()
	record.FinishedAt, record.DurationMS = &finished, &duration
}

func (g *Gateway) finish(record domain.RequestRecord, started time.Time) bool {
	stampRecord(&record, started)
	return g.end(record)
}

func (g *Gateway) end(record domain.RequestRecord) bool {
	ctx, cancel := context.WithTimeout(context.Background(), finalizationTimeout)
	defer cancel()
	if err := g.ledger.EndRequest(ctx, record); err != nil {
		g.mu.Lock()
		g.broken = true
		leases := make([]*leaseState, 0, len(g.leases))
		for _, lease := range g.leases {
			leases = append(leases, lease)
		}
		g.mu.Unlock()
		for _, lease := range leases {
			lease.close()
		}
		return false
	}
	return true
}

func transportFailure(ctx context.Context) (string, string, int) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed_out", "deadline_exceeded", http.StatusGatewayTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled", "canceled", http.StatusBadGateway
	}
	return "failed", "upstream_transport_error", http.StatusBadGateway
}

func nativeError(w http.ResponseWriter, protocol domain.Protocol, status int, class string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	errorType := "invalid_request_error"
	switch status {
	case http.StatusUnauthorized:
		errorType = "authentication_error"
	case http.StatusForbidden:
		errorType = "permission_error"
	case http.StatusNotFound:
		errorType = "not_found_error"
	case http.StatusTooManyRequests:
		errorType = "rate_limit_error"
	default:
		if status >= 500 {
			errorType = "api_error"
			if protocol != domain.AnthropicMessages {
				errorType = "server_error"
			}
		}
	}
	errorBody := map[string]any{"error": map[string]string{"type": errorType, "message": "Router Forge gateway: " + class}}
	if protocol == domain.AnthropicMessages {
		errorBody["type"] = "error"
	} else {
		errorBody["error"].(map[string]string)["code"] = class
	}
	_ = json.NewEncoder(w).Encode(errorBody)
}

func cleanMetadata(value string, lease Lease) string {
	if !safeIdentifier(value, 256) || containsLeaseSecret(value, lease, false) {
		return ""
	}
	return value
}

func validModelName(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if !unicode.IsLetter(c) && !unicode.IsNumber(c) && !strings.ContainsRune("_.:/@-", c) {
			return false
		}
	}
	return true
}

func cleanModelMetadata(value string, lease Lease) string {
	if !validModelName(value) || containsLeaseSecret(value, lease, false) {
		return ""
	}
	return value
}

var hopHeaders = map[string]bool{"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true}

func blockedHeaders(headers http.Header) map[string]bool {
	blocked := make(map[string]bool, len(hopHeaders)+12)
	for key := range hopHeaders {
		blocked[key] = true
	}
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			blocked[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
		}
	}
	for _, name := range []string{"Authorization", "X-Api-Key", "Api-Key", "Cookie", "Set-Cookie", "Host", "Content-Length", "Location"} {
		blocked[name] = true
	}
	return blocked
}

func copyRequestHeaders(destination, source http.Header, lease Lease) {
	blocked := blockedHeaders(source)
	for key, values := range source {
		key = http.CanonicalHeaderKey(key)
		if blocked[key] || !nativeRequestHeader(key) || secretHeaderName(key, lease) {
			continue
		}
		for _, value := range values {
			if containsLeaseSecret(value, lease, false) {
				continue
			}
			destination.Add(key, value)
		}
	}
}

func nativeRequestHeader(key string) bool {
	switch key {
	case "Content-Type", "Accept", "User-Agent", "Anthropic-Version", "Anthropic-Beta", "Anthropic-Dangerous-Direct-Browser-Access", "Openai-Beta", "Openai-Organization", "Openai-Project", "Idempotency-Key", "X-Request-Id", "X-Client-Request-Id", "Traceparent", "Tracestate":
		return true
	}
	return strings.HasPrefix(key, "X-Stainless-")
}

func secretHeaderName(key string, lease Lease) bool {
	return containsLeaseSecret(key, lease, true)
}

func copyResponseHeaders(destination, source http.Header, lease Lease) {
	blocked := blockedHeaders(source)
	for key, values := range source {
		key = http.CanonicalHeaderKey(key)
		if blocked[key] || secretHeaderName(key, lease) || strings.Contains(strings.ToLower(key), "api-key") || strings.Contains(strings.ToLower(key), "authorization") {
			continue
		}
		for _, value := range values {
			if containsLeaseSecret(value, lease, false) {
				continue
			}
			destination.Add(key, value)
		}
	}
}

func relaySync(w http.ResponseWriter, body io.Reader, record *domain.RequestRecord, started time.Time) ([]byte, error, bool) {
	var observed bytes.Buffer
	oversized := false
	buffer := make([]byte, 32<<10)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if record.FirstByteMS == nil {
				elapsed := time.Since(started).Milliseconds()
				record.FirstByteMS = &elapsed
			}
			if !oversized {
				if observed.Len()+n <= maxObservationBytes {
					_, _ = observed.Write(buffer[:n])
				} else {
					oversized = true
					observed.Reset()
				}
			}
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return observed.Bytes(), writeErr, oversized
			}
		}
		if err == io.EOF {
			return observed.Bytes(), nil, oversized
		}
		if err != nil {
			return observed.Bytes(), err, oversized
		}
	}
}
