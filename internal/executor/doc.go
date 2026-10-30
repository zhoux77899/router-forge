// Package executor owns environment transports and native Harbor execution.
//
// New creates a backend-lifetime Manager. Inspect, Tasks and Execute reuse workers
// by public host connection settings and bindings, never private credentials.
// Frozen network revisions and credentials travel only with each operation/run;
// edits cannot change an already-queued private command. Close is for backend shutdown, not for
// closing a Desktop window or a progress subscription. Execute never reconnects
// and reruns an interrupted attempt. A new explicit request can reconnect, but
// native execution rejects an already-existing attempt directory.
//
// Workers are bundled as router-forge-worker-<goos>-<goarch>[.exe]. Local, Windows
// WSL and strict non-interactive system SSH transports use those resources, not
// Go on the execution host. Remote OS/architecture is detected and configured
// overrides must match. Deployment is private and version/hash-scoped. Default
// Linux work lives under ~/.local/share/router-forge/work; Windows work uses the
// local application directory. Unsafe WSL DrvFS permissions fail closed.
//
// Parent HTTP/2 server and worker HTTP/2 client communicate over child stdio.
// /commands, /results, /events and /observations exist only on this private channel. Model relay
// exposes only the selected native /v1 protocol and attempt lease, binds an
// execution-host private Docker-reachable address and uses native extra_hosts.
// Reachability uses a synthetic authenticated model-shaped probe handled locally,
// never real inference. Gateway streaming and aborts share the multiplexed pipe.
// Queues, headers, request bodies, callbacks, sessions and concurrent streams are
// bounded. Emit callbacks should return promptly; slow consumers may lose events
// but cannot block command results or gateway streams.
//
// uv and Harbor are detected by successful --version probes; their actual outputs
// are recorded without a required release. Installed Harbor's own interpreter,
// native dataset routing and config/task expansion resolve swe-bench/swe-bench-verified.
// No floating raw registry URL, Go-side version ordering or alternate SDK is used.
// A draft records the current dataset version/digest and one exact Git SHA or Hub
// task digest; execution downloads that saved source instead of re-resolving it.
// Native config and adapter capabilities, not version numbers, determine readiness.
//
// Agent protocols/options come from installed Harbor's auth/schema commands. Host
// installation/version is informational; container setup uses Harbor's default
// installation policy. OpenCode's native provider selector namespaces only adapter
// configuration; the gateway still requires the literal approved upstream model ID.
// Before forwarding the first authenticated model request,
// the worker independently probes the actual container CLI and immutable image ID.
// The observation has a separate private /observations RPC and must be durably
// acknowledged; event backpressure cannot lose it. Both relay and parent gateway
// fail closed until acknowledgment. Harbor's agent_info.version is never substituted
// for an independent probe, and early image CLIs cannot bypass default setup.
//
// Preflight hints and start-time tools are separate. Runtime observations are merged
// without changing approved sources, model, credential revision, prices or limits.
// Partial evidence survives failure/cancellation; old reports remain readable and
// old frozen drafts must be recreated. Native task names and physical trial prefixes
// come from Harbor rather than a guessed namespace mapping. Downloads and result
// identity checks retain exact source/digest and private-directory boundaries.
// Private provenance is atomically finalized with acknowledged runtime observations;
// final persistence failures are explicit and do not erase database evidence.
//
// Docker base digests and actual task image identities are recorded per execution.
// Native builds retain the original task context; only FROM references in a private
// Dockerfile overlay are made immutable. No tool-image or verifier replacement is
// used. Unsafe dynamic base references and unsupported separate-verifier factories
// fail explicitly without modifying the original verifier.
// Windows/macOS native SWE Harbor support is not claimed; Linux WSL/SSH is the
// first-release execution path.
//
// Each native process has an isolated private HOME and filtered environment, so
// host provider/auth/SSH credentials are not copied. The scoped attempt token is
// passed only as an environment value, not a CLI argument or JobConfig value.
// Format-3 runs require explicit frozen source/build/agent/verifier selections.
// Public snapshots include only saved profile settings, auth presence and credential
// revisions. Private credentials are transferred separately, execution profiles only;
// model credentials stay on Desktop. Source-only inspection/resolution needs only
// its selected source profile. Legacy environment transport fields remain readable
// in historical reports but cannot override new selections or execute old drafts.
// Source uses the installed Harbor launcher with isolated HOME, npm/pip/uv/Git/
// curl/wget configuration and target-host trust. Custom mirrors use only supported
// native variables and owned files; APT .list suites are supplied, never inferred.
// Native installer/original verifier commands are checked for incompatible options.
// Build permits only unauthenticated explicit proxy args with strict system trust;
// Docker daemon trust, pull settings, original Dockerfile and shared configs stay
// unchanged. Read-only Docker info observes actual mirrors/proxy, not connectivity.
//
// Native agent and original verifier have independent env, read-only config/CA
// mounts and Python shims. Lifecycle hooks clear both cases of proxy/no_proxy,
// ambient CA/TLS and client settings after image/native env merging. Verifier also
// unsets model/attempt auth and always verifies TLS; agent skip never reaches it.
// Selected source/agent skip ignores a retained CA path until verification returns.
// Supported Python/HTTPX/Requests clients select phase trust per request, including
// reused native clients. curl/wget/Git wrappers reject conflicting CLI/config flags.
// Unsupported lifecycle/sidecar/mirror behavior fails explicitly; arbitrary task
// clients and Docker daemon/pull/build connectivity are not claimed as verified.
// Model upstream TLS has its own Desktop policy, while SSH stays strict.
//
// Basic proxy auth is injected only upstream by an operation/run-owned relay.
// Source binds exact loopback; container stages use explicitly detected/manual
// Docker-reachable private bind plus current immutable owned container IDs, exact
// inspected IPs and readonly stage mounts. No subnet, LAN, NAT or unauthenticated
// public fallback exists. Context cancellation closes listener/tunnels; configs and
// relays are removed after the operation/run. Credentials never enter client env,
// Harbor JSON, public observations/errors or initial/final failure provenance.
// Provenance network_evidence_kind is frozen_configuration_not_connection_success;
// it preserves profile scopes/revisions and stage CA hashes without asserting every
// connection worked. CheckNetwork probes only the explicit target on the selected
// worker and cannot imply daemon/container/image-pull success.
//
// Cancellation closes owned contexts/process groups or Windows Job Objects and
// removes only revalidated project-owned immutable Docker IDs. EOF first gives
// the worker time to cancel and clean up; it never triggers a paid rerun. Raw
// prompts, stdout, trajectory and verifier logs remain at remote private paths.
// Singular non-atomic result.json writes are retried. Reward, timeout/exception,
// CLI exit, observed version/image and numeric Harbor diagnostics are separate.
// Diagnostic metrics do not represent additional gateway billing.
//
// Synthetic/public-schema fixtures live in benchmarks/harbor/fixtures. Tests use
// real helper child processes with private HTTP/2 pipes plus mock Docker/model
// boundaries. TestWSLDockerMockIntegration is explicitly opt-in on Windows:
// set ROUTER_FORGE_VERIFY_MOCK_INTEGRATION=1 and ROUTER_FORGE_MOCK_IMAGE_ID to an
// already-present immutable sha256 Docker image with python3, optionally set
// ROUTER_FORGE_MOCK_DISTRIBUTION (Ubuntu by default), set GOTMPDIR to an existing
// project-local build scratch directory, then run
// go test ./internal/executor -run TestWSLDockerMockIntegration -count=1 -v.
// It cross-builds the current production worker Windows-side, uses actual WSL
// Docker and the private bridge relay/stdio HTTP2 path, injects the real Gateway
// with a local synthetic provider/ledger, and asserts native JSON/SSE bytes,
// scoped attribution, fresh mock auth, abort propagation and owned cleanup.
// Explicit stream_options.include_usage avoids an intentional gateway usage
// option insertion in this byte-preservation fixture. No image pull, agent,
// Harbor task, upstream key or inference is used; even an existing tool image is
// only a network fixture and cannot be interpreted as a SWE task image/score.
// The private verify-mock RPC refuses paid confirmation, real model names,
// non-fixture attribution and non-loopback providers; it shares attempt limits.
// Observed proof is saved as fixtures/mock-integration-observed.json with
// fixture=true and benchmark_scores=null. Normal versioned worker deployment
// remains reusable; only owned fixture containers/processes/build files are
// cleaned. TestReadOnlyNativeWSLInspection performs only version, capability,
// Docker metadata and interface inspection; it executes no trial. Opt-in source/
// inspection fixtures require ROUTER_FORGE_SOURCE_PROFILE_JSON with a saved profile
// bound to the fixture environment, optional private
// ROUTER_FORGE_SOURCE_PROXY_CREDENTIAL_JSON, and explicit
// ROUTER_FORGE_DOCKER_GATEWAY_MODE/detected-or-manual address where applicable.
// ROUTER_FORGE_OFFLINE_NATIVE_NETWORK=1 enables installed Harbor schema/config
// expansion/lifecycle checks with sockets/process execution blocked except owned
// non-network shell/version fixtures. A separate owned-loopback TLS fixture verifies
// reused installed HTTPX/Requests/stdlib phase clients. Neither starts a Harbor
// trial, installs a package, invokes Docker, downloads a task or calls a model.
package executor
