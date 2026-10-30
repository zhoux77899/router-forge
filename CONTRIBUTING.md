# Contributing

Router Forge is a Go/Bun application workspace. Discuss substantial behavior changes
in an issue and keep unrelated work in separate commits. Start with the application
workflow and platform constraints in [README.md](README.md).

## Prepare the repository tooling

Use Git, Go 1.27.3, Bun 1.3.14, and Node.js 22.12.0 or newer. From the repository root,
install the locked frontend and contribution dependencies:

```text
bun install --frozen-lockfile
go mod download
```

`bun.lock` is the workspace lockfile; do not create a second npm lockfile. The same
workspace includes the Desktop/Web applications, shared packages and commitlint.
Installing dependencies does not enable Git hooks or change your Git configuration.

### Enable the local commit-message hook

Check for an existing hooks configuration first:

```text
git config --show-origin --get core.hooksPath
```

No output means no hooks path is configured. If a path is already configured,
review and integrate its checks before changing it. Do not silently replace an
existing hook setup.

To enable this repository's hook for the current clone:

```text
git config --local core.hooksPath .githooks
```

On a POSIX system, also make the hook executable:

```sh
chmod +x .githooks/commit-msg
```

Git for Windows supplies the shell needed to run the hook. The targeted
`.gitattributes` rule keeps the hook's line endings as LF.

The hook, its Node.js message checker, and the commitlint configuration live in
`.githooks`. The checker removes Git template comments and verbose diffs, rejects
empty cleaned messages, and calls the installed commitlint CLI. Cleanup helpers
and commit rules are in `.githooks/commitlint.config.mjs`. Missing Node.js or
installed development dependencies cause a clear failure; the hook never downloads
a replacement tool or silently skips validation.

### Optional commit template

Enable the editing guide for this clone if you want it:

```text
git config --local commit.template .gitmessage
```

The template contains comments, not a ready-to-submit message. Write the title and
body yourself, and use `git commit -s` to add a sign-off from your configured Git
identity. Template comments use Git's default `#` comment marker.

## Commit messages

Use this structure for every commit:

```text
type(scope): imperative description

Explain the motivation or implementation details in prose.

Signed-off-by: Contributor Name <contributor@example.com>
```

For example:

```text
docs(repo): document the contribution process

Explains how contributors enable local checks and report their validation results.

Signed-off-by: Contributor Name <contributor@example.com>
```

The scope, description, explanatory body, and sign-off are all required. Separate
the title, body, and trailers with blank lines. Use English for the title and body,
with a concise imperative description. Scope names should be short and stable,
such as `policy`, `gateway`, `eval`, `api`, `docs`, or `repo`. They must not be blank
or padded with whitespace; they are not restricted to a fixed module list.

| Type | Use | PR tag |
| --- | --- | --- |
| `feat` | Introduce a feature or behavior. | `Feat` |
| `fix` | Correct a bug. | `Fix` |
| `docs` | Change documentation. | `Docs` |
| `style` | Change formatting without changing behavior. | `Style` |
| `refactor` | Restructure code without adding a feature or fixing a bug. | `Refactor` |
| `test` | Add or change tests. | `Test` |
| `chore` | Maintain supporting tools or repository housekeeping. | `Chore` |
| `perf` | Improve performance. | `Perf` |
| `build` | Change build tooling or external build dependencies. | `Build` |
| `revert` | Undo an earlier change. | `Revert` |
| `ci` | Change CI configuration or scripts. | `CI` |
| `deps` | Update dependencies. | `Deps` |

Commit types are declared in `.githooks/commitlint.config.mjs`; PR tags are declared
separately in `.github/workflows/pr-title.yml`. When changing a type, update both
lists and this table.

Comments and Git trailers are not an explanatory body. Use ordinary sentences or
paragraphs; a standalone `Key: value` block may be interpreted as Git trailers.
A title followed only by `Signed-off-by` fails validation, as does a body made only
of trailers. A sign-off must be a final trailer with a name and email address;
mentioning `Signed-off-by` in prose does not count. Additional trailers, including
`Co-authored-by`, are allowed but do not replace the sign-off.

`Signed-off-by` is a contributor's sign-off, not a GPG signature. Use your own
configured identity; tooling cannot establish whether the declaration is true.
The hook checks structure and required content. Reviewers still check English
wording, imperative style, and whether the body explains the change adequately.
No additional title-length limit or subject-capitalization rule is enforced.

Merge, revert, and release-like messages are not exempt. Edit automatically
prepared messages to follow the same structure. CI does not check commit history,
and local hooks do not cover commits created through GitHub's web interface.
Contributors and maintainers remain responsible for those messages.

## Pull requests

Use `[Type] Description` for PR titles, with the case-sensitive tag from the table:

```text
[Docs] Add contribution guidance
```

The [PR template](.github/PULL_REQUEST_TEMPLATE.md) provides `Summary`, `Test`, and
`Checklist`. Explain what changed and why, and link a relevant issue when one
exists. In `Test`, report the commands or checks actually completed. If a check
was not run, state why and describe the remaining risk. Do not mark a check as
passed merely because it is listed in the template.

The `PR title` workflow checks only the title. It does not inspect commit messages,
PR body sections, or checklist completion. Opening, editing, reopening, updating,
or marking a PR ready for review triggers a title check.

The check is JavaScript inline in the trusted workflow definition. It runs with
read-only permissions, without checking out the repository or installing project
dependencies. It treats the `PR_TITLE` environment variable as data and does not
execute PR-head code.

The workflow becomes available after it is published to the default branch.
Maintainers can make the `PR title` check required in branch protection; this
change does not configure that protection automatically.

### Squash merges

The PR title is not a valid commit title. Before completing a squash merge, change
its generated commit message to `type(scope): description`, write an explanatory
body, and retain an appropriate `Signed-off-by` trailer. The local hook is not run
by GitHub's squash UI, so the maintainer must review the final message.

## Issues

Use the [issue chooser](https://github.com/zhoux77899/router-forge/issues/new/choose)
for bugs and feature requests. Bug reports require reproduction steps, expected
results, and actual results. Feature requests require the problem and proposed
solution. Optional fields provide room for context; blank issues remain enabled.

Remove credentials, private prompts, sensitive repository content, and private
logs before submitting an issue. The templates do not create labels or assign
reviewers. Existing tracker conventions are in
[docs/agents/issue-tracker.md](docs/agents/issue-tracker.md).

## Local files and shared instructions

The first three `.gitignore` groups exclude IDE, editor, and agent-harness
directories. Harness directories such as `.claude`, `.cursor`, `.copilot`,
`.codex`, and `.agents` are ignored in full, including their rules, skills, and
configuration. The remaining groups cover worktrees and job artifacts, environment
files, dependencies, caches and test output, logs, and operating-system files.

Keep shared project guidance in root `AGENTS.md` or `CLAUDE.md`, or under `.github`.
Root `.editorconfig`, `.github`, `.githooks`, and `docs/agents` remain eligible for
version control. These locations are distinct from ignored harness directories.

Only `.env.example` is exempt from the environment-file exclusions. Any such
example must use placeholders, never real credentials. Ignore rules do not remove
already tracked files or provide secret scanning. Lockfiles, product configuration,
documentation, and benchmark inputs outside ignored harness directories remain
eligible for version control.

## Network and UI changes

Follow the explicit [Network workflow](apps/desktop/README.md#network-配置), not
legacy Environment proxy/TLS fields. Keep these boundaries when changing contracts,
storage, transports or views:

- First use is blank. Direct/Proxy, protocol/authentication, `no_proxy`, System/Custom
  CA, Strict/Skip and each apt/pypi/npm/node/uv/uv_python Native/Custom source are
  explicit user choices, never host-environment inheritance or product defaults.
- Source/build/agent/verifier bindings target one exact Environment; the provider's
  model profile targets Desktop only. Old records remain readable without assumed
  policies. Recreate old drafts; do not silently migrate them.
- Hash format 3 freezes five public profiles and independent credential revisions.
  Profile revisions are immutable. Private proxy secrets are write-only, stored in
  restricted local plaintext SQLite, and excluded from public contracts/snapshots.
  Retain/Replace/Clear are distinct; Clear blanks old revisions, not SQLite/WAL or
  backup media. Never send model-profile proxy credentials to an execution worker.
- Saving TLS risk, checking an explicit profile/destination, start-time execution
  and model TLS acknowledgements, and paid execution confirmation are independent.
  Start binds the exact `config_hash`, `confirm_paid` and applicable
  `confirm_execution_tls_risk` / `confirm_model_tls_risk`. A credential-free HEAD
  check sends no provider key or inference and follows no redirect; configuration,
  CA paths or a check are not per-connection or trial-success evidence.
- Build allows only strict/system/native and unauthenticated proxy build args.
  Reject custom CA, mirrors, TLS skip and authenticated relay rather than modifying
  the original Dockerfile. Verifier is strict and isolated from Agent; preserve
  original `test.sh`, solution files and verifier semantics.
- Docker gateway detect/manual is explicitly selected. Observe daemon proxy/mirror
  settings read-only; do not reconfigure/restart the daemon or pull an image to
  check networking. Do not change system trust or shared host package configuration.
- Use React/shadcn composition, default Neutral black/white light/dark tokens and
  HugeIcons. No ring/shadow effects, including focus and overlays; retain visible
  outline/border focus. Remove redundant secondary labels/descriptions, not accessible
  names, errors, server-identity/MITM disclosure or cost confirmation.
- Web and shared results have only a GET-only snapshot facade. Do not expose Network
  management, protected configuration, native control tokens or private credentials.

## Validation

Run the smallest tests that cover the changed package. The complete offline checks
use local mock providers and benchmark fixtures, not paid model credentials:

```text
bun run typecheck
bun test
bun run test:go
bun run build:web
bun run build:backend
```

When Go DTOs or HTTP endpoints change, regenerate and review both contract layers:

```text
bun run generate:contracts
```

`internal/domain` defines the shared types. The Go generator writes OpenAPI and
`openapi-typescript` writes the SDK types; do not hand-edit generated schemas.
The native Desktop build additionally requires MyGo's platform prerequisites:

```text
bun run build:desktop
```

### Optional production-bundle checks

After `bun run build:desktop` on Windows amd64, run the production backend through
the actual Desktop Go bridge without opening a GUI:

```powershell
$env:ROUTER_FORGE_VERIFY_NATIVE_BUNDLE = "1"
$env:ROUTER_FORGE_NATIVE_BUNDLE_DIR = (Resolve-Path ".\build\desktop\windows-amd64").Path
go test .\apps\desktop\... -run TestBundledConsoleIntegration -count=1 -timeout 90s
Remove-Item Env:\ROUTER_FORGE_VERIFY_NATIVE_BUNDLE
Remove-Item Env:\ROUTER_FORGE_NATIVE_BUNDLE_DIR
```

The test uses a temporary private profile and a loopback-only dummy provider. Its
Network fixture now checks the blank initial state, explicit profile save/read and
exact Environment/Provider bindings; fixture choices are not product defaults. It
drives the actual bundled host worker with credential-free HEAD requests to an
owned self-signed TLS server: strict rejection, selected custom CA and its loaded
SHA-256, separately confirmed TLS skip with an ignored missing CA, and strict
restoration. It also checks resource resolution, authenticated configuration
writes, credential redaction, persistence, cancelled request contexts, same-PID
reattachment and the built read-only Web. It sends no provider request and stops
only its own backend.
Application CI enables this check after its Windows Desktop build.

The real WSL Docker relay test is separately opt-in. Set
`ROUTER_FORGE_VERIFY_MOCK_INTEGRATION=1`, `ROUTER_FORGE_MOCK_DISTRIBUTION` to the
existing distribution, and `ROUTER_FORGE_MOCK_IMAGE_ID` to an already-present
immutable image ID, then run:

```powershell
go test .\internal\executor -run TestWSLDockerMockIntegration -count=1 -timeout 300s -v
```

It uses `--pull=never`, does not launch Harbor or an agent, and does not produce a
benchmark score. Unset those variables afterwards. Do not reuse its networking
fixture image as a SWE-bench task environment.

### Optional real Harbor trial with a synthetic provider

Installed-Harbor strict/skip/restore checks and an exact Hub task download passed
separately. The last full real Harbor trial **failed** with `worker connection lost`,
zero provider requests, no acknowledged agent CLI version and no completed verifier.
The format-3 fixture has been migrated to explicit Network inputs and revision-bound
start confirmations, but **has not been run after migration**. Previous trial
failure evidence is unchanged; neither the migration nor the command below proves
a completed trial, paid model call or successful SWE-bench run.

The Windows/WSL fixture uses the built backend and actual Desktop Go bridge, the
installed Harbor and its default OpenCode container installation, and the current
registry's exact `swe-bench/django__django-13741` task source and original verifier.
Dataset resolution follows Harbor's native routing; no Harbor/agent version or
registry/task commit is pinned. Only the external model API is synthetic. It
requires the existing Ubuntu Docker/Harbor setup and can download the original SWE
image and container dependencies; it does not install host tools or use a real
provider key. Allow adequate disk space and setup time.

On a slow registry/CDN connection, pre-cache only the unchanged image declared by
the exact resolved task's environment. Its image may change with the current
registry source; never substitute an image from an old report or TB4's network
fixture. Image pulls use the daemon's existing network settings, not application
Network profiles. Do not restart/reconfigure shared services, change daemon TLS or
use a model TLS exception merely to solve an execution-network problem. The worker
records the actual immutable image identity.

Before enabling this trial, set these fixture-only inputs explicitly:

| Variable | Input |
| --- | --- |
| `ROUTER_FORGE_HARBOR_EXECUTION_NETWORKS` | Windows absolute path to a JSON object with `source`, `build`, `agent` and `verifier`, each a `NetworkProfileInput`. Each nested `profile.scope` is `environment`; `profile.id`, `profile.revision` and `profile.environment_id` are empty until the fixture creates and binds its exact host. |
| `ROUTER_FORGE_HARBOR_MODEL_NETWORK` | Windows absolute path to one `NetworkProfileInput` JSON object. Its `profile.scope` is `desktop`; `profile.id`, `profile.revision` and `profile.environment_id` are empty. The saved profile is bound only to the synthetic provider. |
| `ROUTER_FORGE_HARBOR_DOCKER_GATEWAY_MODE` | Explicit `detect` or `manual`; no fallback selection. |
| `ROUTER_FORGE_HARBOR_DOCKER_GATEWAY` | Explicit address required for `manual` mode. |
| `ROUTER_FORGE_HARBOR_CONFIRM_EXECUTION_TLS_RISK` | Set to `1` to acknowledge start-time risk when the frozen execution profiles skip TLS. |
| `ROUTER_FORGE_HARBOR_CONFIRM_MODEL_TLS_RISK` | Independently set to `1` when the frozen Desktop model profile skips TLS. |

Connection/proxy/authentication, `no_proxy`, System/Custom CA, Strict/Skip, all six
apt/pypi/npm/node/uv/uv_python Native/Custom sources, optional write-only
`proxy_username` / `proxy_password`, and save-time `confirm_tls_risk` come only from
the JSON inputs. JSON files live on Windows; any custom CA path inside a profile
belongs to that profile's selected execution host or Desktop. Keep files containing
credentials private and out of Git, CI, logs and screenshots. No network modes,
addresses or mirrors are inferred. The trial no longer reads the legacy
`ROUTER_FORGE_HARBOR_PROXY`, `ROUTER_FORGE_HARBOR_CA`,
`ROUTER_FORGE_HARBOR_NO_PROXY` or `ROUTER_FORGE_HARBOR_SKIP_TLS_VERIFICATION` inputs.

After preparing the required inputs, enable the test explicitly:

```powershell
$env:ROUTER_FORGE_VERIFY_HARBOR_MOCK = "1"
$env:ROUTER_FORGE_NATIVE_BUNDLE_DIR = (Resolve-Path ".\build\desktop\windows-amd64").Path
$env:ROUTER_FORGE_HARBOR_DISTRIBUTION = "Ubuntu"
go test .\apps\desktop\... -run TestWSLHarborMockTrial -count=1 -timeout 50m -v
Remove-Item Env:\ROUTER_FORGE_VERIFY_HARBOR_MOCK
Remove-Item Env:\ROUTER_FORGE_NATIVE_BUNDLE_DIR
Remove-Item Env:\ROUTER_FORGE_HARBOR_DISTRIBUTION
```

Restore or unset the fixture-only network inputs after the check.
`ROUTER_FORGE_HARBOR_TASK` can select another exact canonical task from the same
resolved dataset; absent that variable, this fixture requests
`swe-bench/django__django-13741` and fails explicitly if it is unavailable. This is
fixture input, not a product task default. It never substitutes a task or old
registry. The draft records the dataset version/digest and task SHA/digest once;
starting consumes that saved reference. The report requires actual uv/Harbor output
and an independently acknowledged container CLI version, not an expected version.

Saving `confirm_tls_risk` does not replace either start-time TLS acknowledgement.
Start sends the actual snapshot `config_hash`, the two independent risk values
above when applicable, and `confirm_paid=true` only for this synthetic mock fixture;
it does not authorize or demonstrate a real model call. Build accepts only
strict/system/native and unauthenticated proxy args; Verifier remains strict and
isolated from Agent. Do not change the original Dockerfile, `test.sh`, solution,
verifier semantics, SSH known-hosts, system trust or shared host tooling. Skip does
not fix proxy 407 or Docker subnet overlap. Saved policy and CA paths are not
independently observed connection results; unsupported clients fail explicitly.

The proxy must be reachable from both its selected native host and task-container
traffic. A host-only check can pass before a new Compose subnet shadows the route
or overlaps a corporate proxy address. Check the selected Docker gateway mode and
approved container-reachable endpoint without reconfiguring a shared daemon or
replacing the task image.

For source-only verification on Linux, enable
`ROUTER_FORGE_VERIFY_NATIVE_SOURCE=1` and run
`TestInstalledHarborCurrentSourceAndExactMaterialization` in `internal/executor`.
This test uses the installed Harbor, resolves the current official dataset once,
and downloads only the exact `ROUTER_FORGE_HARBOR_TASK` (the fixture task above).
It records actual source, native task/trial identities and original metadata hash
without Docker or model requests. `ROUTER_FORGE_NATIVE_SOURCE_EVIDENCE` optionally
saves redacted JSON at an absolute path. A missing task, registry/network failure or
unavailable native API is an explicit failure, not a cache fallback. Source-only
success is not a verifier, paid-model or complete-trial result.

The trial fixture is one task/attempt, concurrency one, zero trial retries, at most
five requests and 256 output tokens per request. It does not solve or modify the
task: an independently verified zero reward is an infrastructure outcome, not a
real model benchmark score. It checks request attribution/native synthetic usage
and public result reattachment, cancels only its own run on failure and retains
private failed-trial artifacts for diagnosis. Successful disposable profiles/jobs
are cleaned up. This heavier check stays opt-in and is not enabled in CI.
`ROUTER_FORGE_HARBOR_MOCK_EVIDENCE` optionally saves redacted JSON to an absolute
file path; do not publish raw Harbor logs or call fixture output a score.

Do not treat a Vite-only page, process exit code, synthetic-provider trial, or
historical benchmark result as native/live acceptance. A real evaluation requires
explicit authorization of the provider, agent, task and limits. Keep model secrets
and raw private Harbor artifacts out of CI, test fixtures, screenshots and pull
requests.

When changing contribution tooling, use temporary examples to check the affected
acceptance and rejection cases. Check syntax and whitespace:

```text
node --check .githooks/commitlint.config.mjs
node --check .githooks/check-commit-message.mjs
git diff --check
```

To check a message file directly:

```text
bun run lint:commit message.txt
```

For PR-title changes, exercise the inline JavaScript in
`.github/workflows/pr-title.yml` with valid and invalid `PR_TITLE` values. There is
no separate workspace command for PR-title validation. Report the workflow's check
result when it is available on the PR.

Use the smallest relevant checks for your change, and record each exact command and
its actual result in the PR. State clearly which platform or live checks were not run.

## Coding agents

Read [AGENTS.md](AGENTS.md) before editing. Do not include local harness state in a
change, forge a sign-off, or claim to have run checks that were not executed.

## References

Commit and PR conventions are adapted from Luma's
[contribution guide](https://raw.githubusercontent.com/zhoux77899/luma/refs/heads/main/CONTRIBUTING.md)
and [PR template](https://raw.githubusercontent.com/zhoux77899/luma/refs/heads/main/.github/PULL_REQUEST_TEMPLATE.md).
