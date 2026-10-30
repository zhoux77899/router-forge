// Package gateway forwards attempt-scoped, fixed-model native inference calls.
//
// The Desktop runtime constructs New with its durable Ledger, registers a Lease
// only after the run's paid-inference approval, and passes the returned temporary
// token to the worker. This package performs no approval, discovery, model calls
// at registration, protocol conversion, retry, or fallback. The worker never
// receives Lease.APIKey. Revoke invalidates the token and cancels active calls,
// including requests still reading their input bodies.
//
// # Routing and authentication
//
// A lease permits POST /v1/chat/completions, POST /v1/responses, or
// POST /v1/messages according to Model.Protocol. The model in an inference body
// must exactly match Model.Name or an explicitly approved AllowedModelNames
// alias; it is never silently replaced. Models and aliases are bounded native
// identifiers. Incoming lease credentials use the Authorization Bearer scheme
// or the x-api-key header. Conflicting credentials are rejected. Outgoing
// authentication is freshly constructed using the upstream APIKey: Bearer for
// OpenAI protocols, x-api-key for Anthropic. Native feature headers are allowlisted;
// cookies, hop-by-hop headers, arbitrary routing headers, and incoming secrets
// are not forwarded. Response credentials and headers containing a lease token
// or upstream key are stripped. No management, model-list, or other API routes
// are exposed by this handler.
//
// Provider.BaseURL must be an absolute HTTP(S) URL without URL credentials,
// traversal, escaped path separators, or a fragment. An existing trailing /v1
// is joined without duplication. The only accepted provider query is a bounded
// api-version value. Responses retrieval additionally accepts include[], stream,
// and starting_after; client queries cannot change the provider or its version.
//
// # Limits and transparency
//
// MaxRequests, MaxOutputTokens, and Timeout must be positive. Accepted inference
// attempts reserve the request budget atomically; failed ledger admission rolls
// the reservation back. Denials are independently recorded as Kind "denied"
// and never forwarded. A missing native output ceiling is inserted as
// max_completion_tokens (Chat), max_output_tokens (Responses), or max_tokens
// (Anthropic). Supplied ceilings must be positive and within the lease cap.
// Chat's n choices share the per-request cap. Other request bytes, including
// tool payloads, stay unchanged. Chat streams receive native include_usage=true
// when stream_options is omitted; an explicit stream_options value is preserved.
//
// Upstream calls use fresh HTTP/1 connections without redirects, automatic
// decompression, or HTTP/2 replay; the worker transport may independently use
// HTTP/2. Request bodies and synchronous metadata observations are limited to
// 8 MiB. SSE lines/events are observed with 1 MiB bounds, supporting LF, CRLF, CR, BOM,
// and multiline data. Response bytes are forwarded unchanged, including native
// errors, tool deltas, and oversized events, with incremental SSE flushes.
// Observation overflow, malformed usage, missing terminal events, transport
// truncation, cancellation, and timeouts cannot become exact usage/cost. Body
// reads and network I/O are bounded; supported ResponseWriter deadlines include
// a five-second error-delivery grace and are reset before returning.
//
// # Ledger and Responses ownership
//
// BeginRequest must durably admit each request before any upstream call. Every
// admitted call receives a terminal EndRequest with a fresh Background context
// bounded to five seconds, independent of caller cancellation. Finalization
// failure latches the gateway closed and cancels active calls: persistence cannot
// be claimed when the ledger itself is unavailable. No bodies, prompts, outputs,
// credentials, or full provider error payloads are written to the ledger; only
// attribution, bounded native IDs/model names, timings, statuses, and accounting
// metadata are recorded. HTTPStatus is the upstream status when received,
// otherwise the local failure status; FirstByteMS measures the first body byte.
//
// Responses IDs learned from native responses belong only to their lease.
// A provider's response ID cannot be reassigned to another attempt, even if a
// later native response repeats it; ownership tombstones last for this gateway.
// GET /v1/responses/{owned-id} and POST /v1/responses/{owned-id}/cancel are allowed;
// previous_response_id must also be owned. External conversation resources are
// not authorized. Followups do not spend the inference request budget and have
// Kind "retrieve" or "cancel", null usage/cost, and not_applicable completeness.
// Deferred/background usage repairs the original inference record by ID instead
// of charging a poll. Ledger.EndRequest must therefore replace that ID's record
// idempotently, not append cost events. Concurrent/repeated polls cannot duplicate
// a charge. Corrections preserve the original call's timestamps and capture
// recovered actual-model metadata; usage source identifies provider_retrieve or
// provider_cancel. Ownership exists only for the current gateway/lease lifetime.
//
// # Accounting
//
// Missing optional token counters remain nil, not fabricated zero. Complete
// usage requires valid input/output/total counts. For Chat/Responses, optional
// cache/reasoning counts can remain unknown because input/output already include
// those subcounts; they are never added again. Missing totals may be derived
// from input+output. Anthropic input_tokens excludes its separately reported
// cache read/write tokens, so InputTokens adds reported cache counts exactly
// once. Missing additive cache counters leave that inclusive input partial and
// cost unknown; explicit zero cache counters can establish complete input.
// Anthropic message_delta output usage is cumulative; the last/maximum count is
// used, never the sum of deltas. Cache creation aggregates and TTL breakdowns
// represent the same tokens and are not both added. Partial streams retain
// observed counts but cannot claim exact cost.
//
// Configured Pricing is snapshotted and evaluated with math/big rational
// arithmetic using finite decimal rates per million. Cache counts replace their
// portion of inclusive input at the configured cache rate; reasoning is already
// part of output. An omitted cache rate uses the ordinary input rate. A distinct
// cache rate requires its counter to be known. Missing/invalid rates, currency,
// or required usage yield null CostAmount, never an invented zero. Explicit
// known zero rates can legitimately produce "0". Currency and pricing Source
// remain separate metadata; computed configured costs are not provider-reported
// charges. This package never combines currencies or invents a dollar spending
// limit; storage must aggregate exact decimal strings separately per currency.
package gateway
