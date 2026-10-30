import { expect, test } from "bun:test";
import { APIClient } from "@router-forge/api";
import { runFixture } from "@router-forge/ui/lib/fixtures.test-support";
import { boundedDraft, legacyDraft, startConfirmedRun } from "./run-controls";

test("draft creation sends confirm_paid=false and never follows with start", async () => {
  const calls: string[] = [];
  const draft = runFixture();
  const client = new APIClient("", "", async (method, path, body) => { calls.push(`${method} ${path}`); expect(JSON.parse(body).confirm_paid).toBe(false); return { status: 200, body: JSON.stringify(draft) }; });
  await client.createRun(draft.snapshot.spec);
  expect(calls).toEqual(["POST /api/runs"]);
});

test("start requires separate explicit paid confirmation and uses the saved run ID", async () => {
  const calls: { id: string; confirmed: boolean }[] = [];
  const draft = runFixture();
  const api = { startRun: async (id: string, confirmed: boolean) => { calls.push({ id, confirmed }); return { ...draft, state: "preparing" }; } };
  expect(draft.snapshot.runtime).toBeUndefined();
  await expect(startConfirmedRun(api, draft, false)).rejects.toThrow("明确确认"); expect(calls).toHaveLength(0);
  await startConfirmedRun(api, draft, true); expect(calls).toEqual([{ id: "run_test", confirmed: true }]);
});

test("every legacy draft must be recreated, never implicitly migrated or started", async () => {
  let calls = 0;
  const api = { startRun: async () => { calls++; return runFixture(); } };
  for (const format of [undefined, 0, 1, 3]) {
    const draft = runFixture(); draft.snapshot.format_version = format;
    expect(legacyDraft(draft)).toBe(true); expect(boundedDraft(draft)).toBe(false);
    await expect(startConfirmedRun(api, draft, true)).rejects.toThrow("重新创建草稿");
  }
  expect(calls).toBe(0);
});

test("new snapshots use default container installation, without any requested version gate", async () => {
  let calls = 0;
  const api = { startRun: async () => { calls++; return runFixture(); } };
  for (const version of ["1.2.3", "1.2.3-beta.1+build.5", "latest", " "]) {
    const draft = runFixture(); draft.snapshot.spec.agent_version = version;
    expect(boundedDraft(draft)).toBe(false);
    await expect(startConfirmedRun(api, draft, true)).rejects.toThrow();
  }
  const draft = runFixture(); draft.snapshot.harbor_version = "Harbor custom build"; draft.snapshot.uv_version = "uv development";
  expect(boundedDraft(draft)).toBe(true);
  for (const key of ["version", "agentVersion"]) {
    const overridden = runFixture(); overridden.snapshot.spec.agent_kwargs = { [key]: "1.2.3" };
    expect(boundedDraft(overridden)).toBe(false);
    await expect(startConfirmedRun(api, overridden, true)).rejects.toThrow();
  }
  expect(calls).toBe(0);
});

test("the approved source must contain only the exact selected task", async () => {
  let calls = 0;
  const api = { startRun: async () => { calls++; return runFixture(); } };
  for (const kind of ["missing", "all-tasks", "other-namespace", "other-dataset", "missing-hash"] as const) {
    const draft = runFixture();
    if (kind === "missing") delete draft.snapshot.resolved_tasks;
    if (kind === "all-tasks") draft.snapshot.resolved_tasks?.push({ name: "swe-bench/second-task" });
    if (kind === "other-namespace") draft.snapshot.resolved_tasks = [{ name: "other/offline-task" }];
    if (kind === "other-dataset") draft.snapshot.suite.dataset = "other/swe-bench-verified";
    if (kind === "missing-hash") delete draft.snapshot.resolution_hash;
    expect(boundedDraft(draft)).toBe(false);
    await expect(startConfirmedRun(api, draft, true)).rejects.toThrow();
  }
  expect(calls).toBe(0);
});

test("reconnect/completed/invalid-boundary records cannot start through control helper", async () => {
  const draft = runFixture();
  let calls = 0;
  const api = { startRun: async () => { calls++; return draft; } };
  expect(boundedDraft({ ...draft, state: "running" })).toBe(false);
  const invalid = runFixture(); invalid.snapshot.spec.task_names.push("second-task");
  await expect(startConfirmedRun(api, invalid, true)).rejects.toThrow();
  await expect(startConfirmedRun(api, { ...draft, state: "completed" }, true)).rejects.toThrow(); expect(calls).toBe(0);
});
