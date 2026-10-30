import { afterEach, describe, expect, test } from "bun:test";
import { APIClient, APIError, type ResolvedBenchmark, type RunSpec, type Suite } from "./index";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

describe("typed local API transport", () => {
  test("read-only web never adds control auth", async () => {
    let headers: Headers | undefined;
    globalThis.fetch = Object.assign(async (_url: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
      headers = new Headers(init?.headers);
      return new Response(JSON.stringify({ items: [], total: 0 }), { status: 200 });
    }, originalFetch);
    const runs = await new APIClient("").runs();
    expect(runs.items).toEqual([]);
    expect(headers?.has("Authorization")).toBe(false);
  });

  test("native transport keeps the control token out of browser fetch", async () => {
    globalThis.fetch = Object.assign(async () => { throw new Error("browser fetch must not be used"); }, originalFetch);
    const requests: string[] = [];
    const client = new APIClient("http://127.0.0.1:12345", "", async (method, path, body) => {
      requests.push(`${method} ${path} ${body}`);
      return { status: 200, body: JSON.stringify({ version: "0.1.0", pid: 1, running: true }) };
    });
    expect((await client.status()).running).toBe(true);
    expect(requests[0]).toBe("GET /api/status ");
  });

  test("start requires explicit native confirmation", async () => {
    let data = "";
    const client = new APIClient("", "", async (_method, _path, body) => { data = body; return { status: 200, body: "{}" }; });
    await client.startRun("run_1", { confirm_paid: false, config_hash: "approved-draft-hash", confirm_execution_tls_risk: false, confirm_model_tls_risk: false });
    expect(JSON.parse(data)).toEqual({ confirm_paid: false, config_hash: "approved-draft-hash", confirm_execution_tls_risk: false, confirm_model_tls_risk: false });
  });

  test("rejects paths outside the local control contract", async () => {
    const client = new APIClient();
    await expect(client.request("GET", "https://example.test/secrets")).rejects.toThrow("Invalid API path");
    await expect(client.request("GET", "/api/../secrets")).rejects.toThrow("Invalid API path");
  });

  test("turns native backend errors into structured errors", async () => {
    const client = new APIClient("", "", async () => ({ status: 409, body: JSON.stringify({ error: "unsupported agent protocol" }) }));
    try { await client.providers(); throw new Error("expected API error"); }
    catch (error) { expect(error).toBeInstanceOf(APIError); expect((error as APIError).status).toBe(409); }
  });
});

const stableSuite: Suite = { id: "swebench-verified", name: "SWE-bench Verified", dataset: "swe-bench/swe-bench-verified", version: "", registry_url: "", task_repository: "", task_commit: "", task_count: 0, suggested_task: "" };
const resolved: ResolvedBenchmark = {
  suite: { ...stableSuite, version: "hub-revision", registry_url: "https://hub.invalid", task_count: 2 },
  registry_kind: "package", registry_metadata_url: "https://hub.invalid/dataset-metadata", dataset_digest: `sha256:${"d".repeat(64)}`, dataset_revision: "hub-revision", resolved_at: "2026-01-01T00:00:00Z", resolution_hash: "preview-source-hash",
  tasks: [{ name: "swe-bench/exact-task", kind: "package", package_name: "swe-bench/exact-task", ref: `sha256:${"a".repeat(64)}`, revision: "task-revision" }, { name: "swe-bench/other-task", kind: "package", package_name: "swe-bench/other-task", ref: `sha256:${"b".repeat(64)}` }],
};
const runSpec: RunSpec = { environment_id: "env_test", model_id: "model_test", suite_id: "swebench-verified", task_names: ["swe-bench/exact-task"], agent: "test-agent", agent_kwargs: {}, max_requests: 5, max_output_tokens: 128, timeout_sec: 600, setup_timeout_sec: 300, concurrency: 1, attempts: 1, confirm_paid: false };

describe("dynamic benchmark and snapshot contracts", () => {
  test("stable descriptor stays unresolved until actual Harbor resolution", async () => {
    const client = new APIClient("", "", async () => ({ status: 200, body: JSON.stringify([stableSuite]) }));
    expect(await client.benchmarks()).toEqual([stableSuite]);
    expect((await client.benchmarks())[0]?.task_count).toBe(0);
    expect((await client.benchmarks())[0]?.version).toBe("");
  });

  test("protected resolution uses native GET, encodes IDs, and preserves canonical package identities", async () => {
    globalThis.fetch = Object.assign(async () => { throw new Error("browser fetch must not be used for native resolution"); }, originalFetch);
    const calls: string[] = [];
    const client = new APIClient("", "", async (method, path, body) => { calls.push(`${method} ${path} ${body}`); return { status: 200, body: JSON.stringify(resolved) }; });
    const result = await client.resolveBenchmark("swebench-verified", "env /?&");
    expect(calls).toEqual(["GET /api/benchmarks/swebench-verified/resolve?environment_id=env%20%2F%3F%26 "]);
    expect(result).toEqual(resolved);
    expect(result.tasks[0]?.name).toBe("swe-bench/exact-task");
    expect(result.tasks[0]?.git_commit).toBeUndefined();
    expect(result.tasks[0]?.git_url).toBeUndefined();
  });

  test("resolution errors are surfaced without falling back to an old task list", async () => {
    const calls: string[] = [];
    const client = new APIClient("", "", async (_method, path) => { calls.push(path); return { status: 409, body: JSON.stringify({ error: "installed Harbor could not resolve the canonical dataset" }) }; });
    await expect(client.resolveBenchmark("swebench-verified", "env_test")).rejects.toThrow("could not resolve");
    expect(calls).toHaveLength(1);
    expect(calls[0]).toContain("/resolve?");
  });

  test("legacy benchmarkTasks array transport remains available with real Git metadata", async () => {
    const tasks = [{ name: "exact-git-task", kind: "git", path: "tasks/exact-git-task", git_url: "https://git.invalid/tasks.git", git_commit: "a".repeat(40) }];
    const client = new APIClient("", "", async (method, path, body) => { expect(method).toBe("GET"); expect(path).toBe("/api/benchmarks/swebench-verified/tasks?environment_id=env_test"); expect(body).toBe(""); return { status: 200, body: JSON.stringify(tasks) }; });
    expect(await client.benchmarkTasks("swebench-verified", "env_test")).toEqual(tasks);
  });

  test("draft creation omits requested agent version, never approves preview source or starts automatically", async () => {
    const calls: { method: string; path: string; body: string }[] = [];
    const client = new APIClient("", "", async (method, path, body) => {
      calls.push({ method, path, body });
      if (path.includes("/resolve?")) return { status: 200, body: JSON.stringify(resolved) };
      return { status: 200, body: JSON.stringify({ id: "run_saved", snapshot: { spec: runSpec, format_version: 2, resolution_hash: "draft-source-hash", resolved_tasks: [resolved.tasks[0]] } }) };
    });
    const preview = await client.resolveBenchmark("swebench-verified", "env_test");
    const draft = await client.createRun(runSpec);
    expect(draft.snapshot.resolution_hash).not.toBe(preview.resolution_hash);
    expect(draft.snapshot.resolved_tasks).toHaveLength(1);
    expect(calls.map(call => `${call.method} ${call.path}`)).toEqual(["GET /api/benchmarks/swebench-verified/resolve?environment_id=env_test", "POST /api/runs"]);
    expect(JSON.parse(calls[1]?.body ?? "{}")).toEqual(runSpec);
    expect(JSON.parse(calls[1]?.body ?? "{}")).not.toHaveProperty("agent_version");
    expect(JSON.parse(calls[1]?.body ?? "{}")).not.toHaveProperty("resolution_hash");
  });

  test("run reads preserve actual nonnumeric tool output and independent container observation", async () => {
    const runtime = { tools: [{ name: "harbor", available: true, version: "", version_output: "Harbor development build" }], observed_at: "2026-01-01T01:00:00Z", agent_version: "container-cli-dev", agent_version_output: "agent container-cli-dev", agent_observed_at: "2026-01-01T01:01:00Z" };
    const client = new APIClient("", "", async () => ({ status: 200, body: JSON.stringify({ id: "run_actual", state: "failed", snapshot: { format_version: 2, runtime, config_hash: "same-approved-hash", resolution_hash: "same-source-hash" } }) }));
    expect((await client.run("run_actual")).snapshot.runtime).toEqual(runtime);
  });

  test("old report reads do not fabricate runtime or rewrite historical requested versions", async () => {
    const client = new APIClient("", "", async () => ({ status: 200, body: JSON.stringify({ id: "run_history", state: "completed", snapshot: { spec: { ...runSpec, agent_version: "1.2.3" }, harbor_version: "0.25.0" } }) }));
    const report = await client.run("run_history");
    expect(report.snapshot.spec.agent_version).toBe("1.2.3");
    expect(report.snapshot.format_version).toBeUndefined();
    expect(report.snapshot.runtime).toBeUndefined();
    expect(report.snapshot.resolved_tasks).toBeUndefined();
  });
});
