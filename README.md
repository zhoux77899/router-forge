# Router Forge

Router Forge runs small agent benchmarks through a metered model gateway. Configure
execution environments, providers and a fixed model in the native Desktop console;
run an independent Harbor verifier; inspect outcomes and request costs in Desktop
or the local read-only Web dashboard.

## First-release scope

- MyGo Desktop with React/shadcn, a separate Go backend, and SQLite persistence.
- Local, WSL and SSH environment profiles, connection checks and tool/agent detection.
- Native Chat Completions, Responses and Anthropic Messages forwarding, including
  SSE and tool-call payloads. No protocol conversion, fallback or model substitution.
- One task from `swe-bench/swe-bench-verified`, resolved by the installed Harbor's
  native default routing: concurrency 1, one attempt and zero Harbor trial retries.
  Tools and datasets are not version-pinned. Harbor installs the agent using its
  default policy; its actual container CLI version is independently observed.
- Request/run/task/attempt attribution, nullable usage, per-currency cost estimates,
  latency, exceptions and independent verifier rewards.
- A backend that survives closing the Desktop window and reconnects on the next
  launch. A backend crash produces interrupted records, not an automatic retry.

Auto routing, multi-task campaigns, a public hosted dashboard and installer/release
signing are outside this first framework. Mock integration tests are not a benchmark
score or proof that every executor platform has passed a live evaluation.

## Prerequisites

### Build and run the console

- Git, Go **1.27.3**, Bun **1.3.14**, and Node.js **22.12.0 or newer** for contribution
  tooling. Go's default automatic toolchain selection can download the version in
  [go.mod](go.mod); no global Go configuration change is required.
- Windows 10/11 with WebView2 for the initial native Desktop path. See
  [MyGo's platform requirements](https://mygo.egoist.dev/) for other hosts. MyGo's
  CLI and Go module are both pinned to **0.7.1**.

From the repository root:

```powershell
bun install --frozen-lockfile
go mod download
bun run build:backend
bun run dev:desktop
```

To build the local read-only dashboard and native Desktop bundle:

```powershell
bun run build:desktop
```

`build:desktop` builds Web assets and backend/worker resources before the MyGo
application. Native application packaging is distinct from a Vite-only preview.
Keep the executable beside its `server`, `workers` and `dist` resource directories.
On Windows amd64, open the built application from the repository root:

```powershell
& ".\apps\desktop\build\windows-amd64\Router Forge.exe"
```

To test without changing your normal backend profile, pass an absolute directory:

```powershell
& ".\apps\desktop\build\windows-amd64\Router Forge.exe" --data-dir "$env:LOCALAPPDATA\RouterForge-Test"
```

`--data-dir` overrides `ROUTER_FORGE_DATA_DIR` and also places the native renderer's
cache and logs inside that profile. If an existing app/backend is still using the
default bundle, build into a separate output directory rather than stopping it:

```powershell
bun run build:web
bun run build:backend
bun run --cwd .\apps\desktop build -o .\build\verified
```

The fresh bundle is under `apps\desktop\build\verified\windows-amd64`; pass that
absolute directory as `ROUTER_FORGE_NATIVE_BUNDLE_DIR` for the opt-in checks. The
local installer is unsigned; signing and installer acceptance are not covered by
the offline checks. Do not bypass an OS or browser security warning.

If a corporate proxy needs a private CA, use the organization's trusted CA bundle
with Bun's `--cafile` option. Keep TLS verification enabled; do not bypass certificate
errors. Do not commit a CA bundle, proxy credential or local dependency cache.

### Prepare an execution environment

Host tools are **detected, not automatically installed or upgraded**. Configure
existing paths for Docker, [uv](https://docs.astral.sh/uv/) and Harbor. A successful
`--version` probe is enough to detect uv/Harbor; versions are recorded, not compared
with a required release. Native config, source-resolution and adapter capabilities
must still be available. Host-agent discovery provides informational hints only.
Harbor uses its default agent installation **inside the task container**; no exact
agent version is requested. Inference stays blocked until an independent container
CLI probe has succeeded and its actual version has been durably acknowledged.

| Profile | Configuration and boundary |
| --- | --- |
| Local | The current OS and tool paths. Native Windows is a connection/inspection path; use WSL for the initial Linux-container SWE evaluation. |
| WSL | A distribution name, a private Linux work directory, tool paths and optional proxy/CA settings. Keep credentials/configs on the Linux filesystem, not an unprotected Windows mount. |
| SSH | An existing Linux SSH host/config, identity or SSH agent, and known-hosts file. Host-key checks stay enabled; no password prompt or permissive host-key mode is added. Linux amd64/arm64 worker resources are bundled. |

The worker relays container requests over its authenticated stdio connection to the
local backend. It does not publish the control API, require SSH `GatewayPorts`, or
send the real provider key to the task container. Docker-to-relay connectivity and
configured CA/proxy reachability must pass preflight. The profile proxy configures
worker/Harbor processes, not the Docker daemon's image downloader. A healthy daemon
does not guarantee that its registry/CDN connection can fetch the original task
image within the build budget. Prepare that unchanged image using the existing
Docker network configuration when necessary; do not replace it with a tool fixture.

Cold agent discovery and native registry/Hub resolution can take up to the control
API's three-minute preflight budget. Resolution uses the installed Harbor package,
not a separately installed SDK, hardcoded registry commit or Go-side version sorter.
Network, missing-task and capability failures are explicit; there is no old-source
fallback or automatic tool upgrade. A timeout or disconnection never authorizes a
rerun.

## Run one benchmark

1. Open Desktop and save a Local, WSL or SSH profile. Run its connection/tool check
   and resolve reported readiness issues before creating a run.
2. Add a provider's HTTPS base URL and key, then a model with its exact wire name,
   native protocol and optional documented per-million token prices. Loopback HTTP
   endpoints exist for local mock tests, not remote plaintext providers.
3. Resolve `swe-bench/swe-bench-verified` in that environment and select exactly one
   canonical task from the returned catalog. No task is automatically substituted
   or launched. The preview may change; the saved draft is the authoritative source.
4. Choose a detected, protocol-compatible Harbor agent. Its container installation
   uses Harbor's default policy, not an explicit version. Set setup/agent timeouts,
   a request-count limit and a per-request output-token ceiling.
5. Create a draft and review its recorded dataset version/digest and exact task
   commit/digest, model and limits. Starting uses those saved task references without
   re-resolving the floating dataset. Drafting does **not** authorize inference;
   starting requires separate confirmation. Downloads and container setup can take
   time, and model use can incur cost.
6. Inspect run progress, request usage/cost and verifier outcome. Cancel affects only
   that owned run. Closing Desktop does not cancel it; reopening reconnects to the
   same backend.

Only one run can execute at a time. Tools may be upgraded between drafting and
starting; the run records the versions actually used at start, while its approved
SHA/digests and model/credential/price references remain unchanged. Reports retain
preflight hints, actual tool output, registry/Hub identity, dataset version/digest,
exact task source and independent container agent version as a dynamic snapshot.
Hub sources without Git commits are reported with their actual digest/revision,
not a fabricated commit. Old reports remain readable; legacy frozen drafts must be
recreated, not silently migrated. Partial observations survive failure/cancellation.
Private `provenance.json` is finalized with acknowledged runtime observations;
failed final persistence is reported rather than treating preflight hints as actual
container versions.

Agent-internal SDK calls still count as individual metered gateway requests; the
gateway itself never retries a provider request.

An exit code of zero is not proof of solving a task. A reward of zero is different
from an infrastructure failure, and a reported agent/verifier exception is preserved
separately. A completed worker without an independent reward is a failed execution.
Missing/partial provider usage or undocumented prices remain unknown, not exact zero.
Cost estimates use the approved price source and currency; they are not provider invoices.

The backend URL shown by Desktop serves the built **read-only** Web dashboard on
loopback. It exposes results, events and request metadata, not configuration controls
or provider keys. Public deployment and browser-side administration are not supported.

For frontend development, Desktop uses `127.0.0.1:5174` and attaches through MyGo.
The separate `bun run dev:web` server uses `127.0.0.1:5173`; set
`ROUTER_FORGE_API_URL` to the actual backend URL shown in Desktop before starting
it. That development proxy remains read-only and does not require a control token.

## Data and credentials

The backend stores its SQLite database, readiness descriptor and private logs under
`os.UserConfigDir()/RouterForge` (normally `%APPDATA%\RouterForge` on Windows).
Executor jobs and raw Harbor results stay in the configured private execution work
directory. Back up both locations if you need to retain the original trial artifacts.

**Provider API keys are deliberately stored in plaintext SQLite.** They are not
vault-backed or encrypted. Directory/database/WAL/SHM permissions restrict ordinary
access to the current user (and Windows administrators/SYSTEM). These protections do
not protect against that account being compromised or an administrator reading files.
Do not sync the profile to a public folder or include it in screenshots, issues or CI.

Key updates create private credential revisions so an approved draft cannot silently
switch keys. Omitting the key field preserves it; explicitly clearing it invalidates
stored credential references. Clearing is **not** secure physical erasure of SQLite,
WAL or backups, and does not stop a lease already holding the key in memory; cancel
an active run explicitly if it must stop using that credential.

The request ledger stores metadata and token counts, not raw prompts or generated
content. Harbor's private artifacts can contain both. Treat those files as sensitive;
they are not served by the result dashboard or uploaded by CI.

## Workspace

| Location | Responsibility |
| --- | --- |
| `apps/server` | Independent loopback backend and readiness lifecycle. |
| `apps/worker`, `internal/executor` | Local/WSL/SSH transport, container relay and native Harbor execution. |
| `apps/desktop` | MyGo Go bindings and native React/shadcn console. |
| `apps/web` | Vite read-only results application. |
| `internal/domain`, `internal/store` | Shared records, migrations, permissions and request accounting. |
| `internal/gateway`, `internal/control` | Same-protocol metering and protected orchestration API. |
| `packages/api`, `packages/ui` | Generated OpenAPI/TypeScript contracts, SDK and shared UI. |
| `benchmarks/harbor` | Benchmark/adapter fixtures used without paid credentials. |
| `scripts` | Contract generation and platform resource builds. |

## Development and tests

```powershell
bun run generate:contracts
bun run typecheck
bun test
bun run test:go
bun run build:web
bun run build:backend
bun run build:desktop
```

The default tests use local mock providers and fixtures. They must not consume a
real model key. The Application workflow runs offline checks on Windows and Linux,
builds native Desktop on Windows, and verifies the production bundle through its
real Go bridge in an isolated temporary profile. That check covers configuration
persistence, same-PID attachment, write-only credentials and bundled Web assets;
it does not open a native window or launch a benchmark. CI has no paid evaluation job.

The observed [WSL Docker mock integration](benchmarks/harbor/fixtures/mock-integration-observed.json)
checks native JSON/SSE forwarding, cancellation and private relay boundaries using
an already-present immutable image. No Harbor trial, model inference or benchmark
score is produced. A separate opt-in `TestWSLHarborMockTrial` uses real Harbor and
the original one-task SWE verifier against a synthetic local provider. It can
pull/build the original task image and install dependencies inside that container;
it does not use paid credentials or produce a real model benchmark score. See
CONTRIBUTING for both opt-in commands. Native form/close-reopen checks and a
separately authorized real-provider SWE-bench run remain distinct acceptance steps.

See [CONTRIBUTING.md](CONTRIBUTING.md) for targeted validation, lockfile and commit/PR
rules. Coding agents should read [AGENTS.md](AGENTS.md), which links the shared
conventions in [docs/agents](docs/agents).

## Benchmark and design references

- [SWE-bench Verified](https://www.swebench.com/verified.html): the 500-task verified
  subset; original task environments and verifiers are retained.
- [Harbor](https://harborframework.com/): the installed runtime's native default
  registry/Hub resolution supplies current dataset and task references.
- [Harbor task repository](https://github.com/laude-institute/harbor-datasets): Git
  task references follow the resolved registry's recorded commit. Hub tasks retain
  their declared content digest/revision instead of inventing a Git commit.
- [Initial design discussion](https://chatgpt.com/share/6ae15193-8830-83ee-b320-d8237189a48b).

Report issues through the [issue chooser](https://github.com/zhoux77899/router-forge/issues/new/choose),
after removing credentials and private execution artifacts.

## License

No project license file is currently included. Upstream benchmark datasets, task
images and tools retain their own licenses; this framework does not relicense them.
