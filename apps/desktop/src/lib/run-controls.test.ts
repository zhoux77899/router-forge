import { expect, test } from "bun:test";
import { APIClient, type NetworkProfile, type Run, type StartInput } from "@router-forge/api";
import { runFixture as historicalFixture } from "@router-forge/ui/lib/fixtures.test-support";
import { executionNetworkStages } from "./forms";
import { boundedDraft, emptyRunApprovals, legacyDraft, runConfirmationKey, runTLSRisks, startApprovalInput, startConfirmedRun } from "./run-controls";

function profile(id: string, scope: "desktop" | "environment" = "environment"): NetworkProfile {
  return { id, name: id, scope, ...(scope === "environment" ? { environment_id: "env_test" } : {}), revision: "revision-1", created_at: "2026-01-01T00:00:00Z", proxy_mode: "direct", proxy_auth: "none", tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false, mirrors: { apt: { mode: "native" }, pypi: { mode: "native" }, npm: { mode: "native" }, node: { mode: "native" }, uv: { mode: "native" }, uv_python: { mode: "native" } } };
}
function runFixture(): Run {
  const draft = structuredClone(historicalFixture());
  const network = { source: profile("network_source"), build: profile("network_build"), agent: profile("network_agent"), verifier: profile("network_verifier"), model: profile("network_model", "desktop") };
  draft.snapshot.format_version = 3; draft.snapshot.network = network;
  draft.snapshot.environment.networks = { source_profile_id: network.source.id, build_profile_id: network.build.id, agent_profile_id: network.agent.id, verifier_profile_id: network.verifier.id };
  draft.snapshot.environment.docker_gateway_mode = "detect";
  draft.snapshot.provider.network_profile_id = network.model.id;
  return draft;
}
const paidInput = (run: Run, overrides: Partial<StartInput> = {}): StartInput => ({ confirm_paid: true, config_hash: run.snapshot.config_hash, ...overrides });

test("paid consent is bound to the exact saved run, configuration and source hashes", () => {
  const draft = runFixture(); const key = runConfirmationKey(draft);
  expect(runConfirmationKey(runFixture())).toBe(key);
  expect(runConfirmationKey({ ...draft, id: "other-run" })).not.toBe(key);
  expect(runConfirmationKey({ ...draft, snapshot: { ...draft.snapshot, config_hash: "edited-config" } })).not.toBe(key);
  expect(runConfirmationKey({ ...draft, snapshot: { ...draft.snapshot, resolution_hash: "edited-source" } })).not.toBe(key);
});

test("draft creation sends confirm_paid=false and never follows with start", async () => {
  const calls: string[] = []; const draft = runFixture();
  const client = new APIClient("", "", async (method, path, body) => { calls.push(`${method} ${path}`); expect(JSON.parse(body).confirm_paid).toBe(false); return { status: 200, body: JSON.stringify(draft) }; });
  await client.createRun(draft.snapshot.spec); expect(calls).toEqual(["POST /api/runs"]);
});

test("start requires explicit payment and the exact immutable configuration hash", async () => {
  const calls: { id: string; input: StartInput }[] = []; const draft = runFixture();
  const api = { startRun: async (id: string, input: StartInput) => { calls.push({ id, input }); return { ...draft, state: "preparing" }; } };
  expect(draft.snapshot.runtime).toBeUndefined();
  await expect(startConfirmedRun(api, draft, paidInput(draft, { confirm_paid: false }))).rejects.toThrow("Explicitly approve payment");
  await expect(startConfirmedRun(api, draft, paidInput(draft, { config_hash: "another-draft-hash" }))).rejects.toThrow("configuration hash");
  expect(calls).toHaveLength(0);
  await startConfirmedRun(api, draft, paidInput(draft));
  expect(calls).toEqual([{ id: "run_test", input: { confirm_paid: true, config_hash: "offline-test-hash" } }]);
});

test("every legacy or networkless draft must be recreated, never migrated or started", async () => {
  let calls = 0; const api = { startRun: async () => { calls++; return runFixture(); } };
  for (const format of [undefined, 0, 1, 2, 4]) {
    const draft = runFixture(); draft.snapshot.format_version = format;
    expect(legacyDraft(draft)).toBe(true); expect(boundedDraft(draft)).toBe(false);
    await expect(startConfirmedRun(api, draft, paidInput(draft))).rejects.toThrow("Recreate this historical draft");
  }
  for (const network of [undefined, null]) {
    const draft = runFixture(); draft.snapshot.network = network;
    expect(legacyDraft(draft)).toBe(true); expect(boundedDraft(draft)).toBe(false);
    await expect(startConfirmedRun(api, draft, paidInput(draft))).rejects.toThrow("Recreate this historical draft");
  }
  expect(calls).toBe(0);
});

test("execution and model TLS risks are independent of one another and of payment", async () => {
  for (const scope of ["none", "execution", "model", "both"]) {
    const draft = runFixture(); const network = draft.snapshot.network!;
    if (scope === "execution" || scope === "both") network.source = { ...network.source, tls_mode: "skip", tls_risk_accepted: true };
    if (scope === "model" || scope === "both") network.model = { ...network.model, tls_mode: "skip", tls_risk_accepted: true };
    const risks = runTLSRisks(draft); const calls: StartInput[] = [];
    const api = { startRun: async (_id: string, input: StartInput) => { calls.push(input); return draft; } };
    expect(risks).toEqual({ execution: scope === "execution" || scope === "both", model: scope === "model" || scope === "both" });
    const fresh = startApprovalInput(draft, emptyRunApprovals());
    expect(fresh.confirm_paid).toBe(false);
    if (risks.execution) expect(fresh.confirm_execution_tls_risk).toBe(false);
    if (risks.model) expect(fresh.confirm_model_tls_risk).toBe(false);
    await expect(startConfirmedRun(api, draft, paidInput(draft, { confirm_paid: false, confirm_execution_tls_risk: true, confirm_model_tls_risk: true }))).rejects.toThrow("payment");
    if (risks.execution) await expect(startConfirmedRun(api, draft, paidInput(draft, { confirm_model_tls_risk: true }))).rejects.toThrow("execution TLS risk");
    if (risks.model) await expect(startConfirmedRun(api, draft, paidInput(draft, { confirm_execution_tls_risk: true }))).rejects.toThrow("model TLS risk");
    expect(calls).toHaveLength(0);
    await startConfirmedRun(api, draft, paidInput(draft, { confirm_execution_tls_risk: true, confirm_model_tls_risk: true }));
    expect(calls).toEqual([{ confirm_paid: true, config_hash: draft.snapshot.config_hash, ...(risks.execution ? { confirm_execution_tls_risk: true } : {}), ...(risks.model ? { confirm_model_tls_risk: true } : {}) }]);
  }
});

test("every execution stage contributes risk, but historical flags and model profiles never supply execution approval", () => {
  for (const stage of executionNetworkStages) {
    const draft = runFixture(); draft.snapshot.network![stage] = { ...draft.snapshot.network![stage], tls_mode: "skip" };
    expect(runTLSRisks(draft)).toEqual({ execution: true, model: false });
  }
  const legacyFlag = runFixture(); legacyFlag.snapshot.environment.skip_tls_verification = true;
  expect(runTLSRisks(legacyFlag)).toEqual({ execution: false, model: false }); expect(boundedDraft(legacyFlag)).toBe(true);
  const modelOnly = runFixture(); modelOnly.snapshot.network!.model.tls_mode = "skip";
  expect(runTLSRisks(modelOnly)).toEqual({ execution: false, model: true });
});

test("all approvals reset immediately on run, hash, target, revision, credentials or configuration changes", () => {
  const draft = runFixture(); draft.snapshot.network!.source.tls_mode = "skip"; draft.snapshot.network!.model.tls_mode = "skip";
  const approvals = { key: runConfirmationKey(draft), paid: true, executionTLSRisk: true, modelTLSRisk: true };
  expect(startApprovalInput(draft, approvals)).toEqual({ confirm_paid: true, config_hash: draft.snapshot.config_hash, confirm_execution_tls_risk: true, confirm_model_tls_risk: true });
  const changes: ((run: Run) => void)[] = [run => { run.id = "other-run"; }, run => { run.state = "preparing"; }, run => { run.snapshot.config_hash = "new-hash"; }, run => { run.snapshot.resolution_hash = "new-source"; }, run => { run.snapshot.provider.base_url = "https://changed-provider.invalid/v1"; }, run => { run.snapshot.provider.key_revision = "changed-key-reference"; }, run => { run.snapshot.network!.source.revision = "new-profile-revision"; }, run => { run.snapshot.network!.model.revision = "new-model-revision"; }, run => { run.snapshot.network!.model.credential_revision = "new-proxy-credential-reference"; }, run => { run.snapshot.network!.source.proxy_url = "https://changed-proxy.invalid"; }, run => { run.snapshot.network!.source.ca_cert_path = "/changed/ca.pem"; }, run => { run.snapshot.environment.docker_gateway_mode = "manual"; run.snapshot.environment.docker_host_gateway = "changed.gateway"; }, run => { run.snapshot.spec.max_requests = 4; }];
  for (const change of changes) {
    const changed = structuredClone(draft); change(changed);
    expect(runConfirmationKey(changed)).not.toBe(approvals.key);
    const input = startApprovalInput(changed, approvals);
    expect(input.confirm_paid).toBe(false); expect(input.confirm_execution_tls_risk).toBe(false); expect(input.confirm_model_tls_risk).toBe(false);
  }
  expect(emptyRunApprovals()).toEqual({ key: null, paid: false, executionTLSRisk: false, modelTLSRisk: false });
});

test("the generated start contract sends only hash-bound confirmations through the offline transport", async () => {
  const draft = runFixture(); draft.snapshot.network!.source.tls_mode = "skip"; draft.snapshot.network!.model.tls_mode = "skip";
  const requests: { method: string; path: string; input: unknown }[] = [];
  const client = new APIClient("", "", async (method, path, body) => { requests.push({ method, path, input: JSON.parse(body) }); return { status: 200, body: JSON.stringify({ ...draft, state: "preparing" }) }; });
  await startConfirmedRun(client, draft, paidInput(draft, { confirm_execution_tls_risk: true, confirm_model_tls_risk: true }));
  expect(requests).toEqual([{ method: "POST", path: "/api/runs/run_test/start", input: { confirm_paid: true, config_hash: draft.snapshot.config_hash, confirm_execution_tls_risk: true, confirm_model_tls_risk: true } }]);
});

test("frozen profiles must match their exact host stages and Desktop provider binding", async () => {
  let calls = 0; const api = { startRun: async () => { calls++; return runFixture(); } };
  const changes: ((run: Run) => void)[] = [run => { run.snapshot.environment.networks = null; }, run => { run.snapshot.environment.docker_gateway_mode = undefined; }, run => { run.snapshot.environment.docker_gateway_mode = "manual"; run.snapshot.environment.docker_host_gateway = ""; }, run => { run.snapshot.network!.source.environment_id = "other-host"; }, run => { run.snapshot.network!.source.id = "other-profile"; }, run => { run.snapshot.network!.agent.revision = ""; }, run => { run.snapshot.network!.model.scope = "environment"; }, run => { run.snapshot.provider.network_profile_id = ""; }];
  for (const change of changes) {
    const draft = runFixture(); change(draft); expect(boundedDraft(draft)).toBe(false);
    await expect(startConfirmedRun(api, draft, paidInput(draft))).rejects.toThrow();
  }
  expect(calls).toBe(0);
});

test("new snapshots use default container installation, without any requested version gate", async () => {
  let calls = 0; const api = { startRun: async () => { calls++; return runFixture(); } };
  for (const version of ["1.2.3", "1.2.3-beta.1+build.5", "latest", " "]) {
    const draft = runFixture(); draft.snapshot.spec.agent_version = version;
    expect(boundedDraft(draft)).toBe(false); await expect(startConfirmedRun(api, draft, paidInput(draft))).rejects.toThrow();
  }
  const draft = runFixture(); draft.snapshot.harbor_version = "Harbor custom build"; draft.snapshot.uv_version = "uv development"; expect(boundedDraft(draft)).toBe(true);
  for (const key of ["version", "agentVersion"]) {
    const overridden = runFixture(); overridden.snapshot.spec.agent_kwargs = { [key]: "1.2.3" };
    expect(boundedDraft(overridden)).toBe(false); await expect(startConfirmedRun(api, overridden, paidInput(overridden))).rejects.toThrow();
  }
  expect(calls).toBe(0);
});

test("the approved source must contain only the exact selected task", async () => {
  let calls = 0; const api = { startRun: async () => { calls++; return runFixture(); } };
  for (const kind of ["missing", "all-tasks", "other-namespace", "other-dataset", "missing-hash"] as const) {
    const draft = runFixture();
    if (kind === "missing") delete draft.snapshot.resolved_tasks;
    if (kind === "all-tasks") draft.snapshot.resolved_tasks?.push({ name: "swe-bench/second-task" });
    if (kind === "other-namespace") draft.snapshot.resolved_tasks = [{ name: "other/offline-task" }];
    if (kind === "other-dataset") draft.snapshot.suite.dataset = "other/swe-bench-verified";
    if (kind === "missing-hash") delete draft.snapshot.resolution_hash;
    expect(boundedDraft(draft)).toBe(false); await expect(startConfirmedRun(api, draft, paidInput(draft))).rejects.toThrow();
  }
  expect(calls).toBe(0);
});

test("reconnect/completed/invalid-boundary records cannot start through control helper", async () => {
  const draft = runFixture(); let calls = 0; const api = { startRun: async () => { calls++; return draft; } };
  expect(boundedDraft({ ...draft, state: "running" })).toBe(false);
  const invalid = runFixture(); invalid.snapshot.spec.task_names.push("second-task");
  await expect(startConfirmedRun(api, invalid, paidInput(invalid))).rejects.toThrow();
  await expect(startConfirmedRun(api, { ...draft, state: "completed" }, paidInput(draft))).rejects.toThrow(); expect(calls).toBe(0);
});
