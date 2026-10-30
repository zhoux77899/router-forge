import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { RequestTable } from "./request-table";
import { MeteringSummary, RunOverview } from "./run-summary";
import { EventTimeline, RunDiagnostics } from "./run-events";
import { RunSnapshotDetails } from "./frozen-config";
import type { RuntimeSnapshot } from "@router-forge/api";
import { requestFixture, runFixture, testSummary } from "../lib/fixtures.test-support";

test("new draft nil collections render as empty, not zero cost or a crash", () => {
  const wire = JSON.parse(JSON.stringify(runFixture()));
  wire.tasks = null; wire.summary.costs = null;
  const html = renderToStaticMarkup(<RunOverview run={wire} />);
  expect(html).toContain("草稿尚未分配执行尝试"); expect(html).toContain("成本未知");
});

test("request rows preserve null vs observed zero and never render raw prompt/output", () => {
  const request = Object.assign(requestFixture(), { raw_prompt: "NEVER_RENDER_PROMPT", raw_output: "NEVER_RENDER_OUTPUT" });
  const html = renderToStaticMarkup(<RequestTable requests={[request, requestFixture({ id: "req_zero", usage: { ...request.usage, cache_read_tokens: 0, cache_write_tokens: 0 }, cost_amount: "0", currency: "CNY" })]} />);
  expect(html).toContain("未知 / 未知"); expect(html).toContain("0 / 0"); expect(html).toContain("CNY 0"); expect(html).not.toContain("NEVER_RENDER_PROMPT"); expect(html).not.toContain("NEVER_RENDER_OUTPUT");
});

test("currency totals remain separate and exact with source labels", () => {
  const html = renderToStaticMarkup(<MeteringSummary summary={{ ...testSummary, costs: [{ currency: "USD", amount: "0.00000980246912858024691276", estimated: true }, { currency: "CNY", amount: "1.2300", estimated: true }] }} />);
  expect(html).toContain("USD 0.00000980246912858024691276"); expect(html).toContain("CNY 1.2300"); expect(html).toContain("按已配置价格估算"); expect(html).toContain("不自动汇兑");
});

const observedRuntime: RuntimeSnapshot = {
  tools: [{ name: "harbor", available: true, path: "/test/actual-harbor", version: "", version_output: "Harbor development build actual-run" }, { name: "uv", available: true, version: "0.12.0", version_output: "uv 0.12.0 actual-run" }],
  observed_at: "2026-01-02T00:00:00Z", work_dir: "/test/actual-work", docker_gateway: "actual-gateway", registry_url: "https://hub.invalid/actual", task_name: "swe-bench/offline-task", task_path: "/test/actual-task",
  agent_version: "container-cli-development", agent_version_output: "agent container-cli-development", agent_observed_at: "2026-01-02T00:01:00Z",
};

test("draft snapshot shows selected canonical package digest and unobserved runtime, never a fabricated commit", () => {
  const html = renderToStaticMarkup(<RunSnapshotDetails run={runFixture()} />);
  expect(html).toContain("swe-bench/swe-bench-verified"); expect(html).toContain("swe-bench/offline-task"); expect(html).toContain(`sha256:${"a".repeat(64)}`); expect(html).toContain(`sha256:${"d".repeat(64)}`);
  expect(html).toContain("offline-source-hash"); expect(html).toContain("offline-test-hash"); expect(html).toContain("2026-01-01T00:00:00Z");
  expect(html).toContain("未提供；使用 Package Digest"); expect(html).toContain("<dt>实际 Harbor 版本 / 输出</dt><dd>尚未观测</dd>"); expect(html).toContain("<dt>实际容器 Agent CLI 版本</dt><dd>尚未观测</dd>");
  expect(html).toContain("Harbor 默认容器安装"); expect(html).not.toContain("0.25.0"); expect(html).not.toContain("冻结");
});

test("actual tool upgrades are distinct from retained preflight hints and approval hashes", () => {
  const run = runFixture({ state: "running" });
  run.snapshot.runtime = observedRuntime; run.snapshot.harbor_version = "Harbor development build actual-run"; run.snapshot.uv_version = "0.12.0";
  const html = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
  expect(html).toContain("草稿预检工具（仅提示）"); expect(html).toContain("0.40.0"); expect(html).toContain("0.9.0");
  expect(html).toContain("开始后的实际执行观测（非批准配置）"); expect(html).toContain("Harbor development build actual-run"); expect(html).toContain("uv 0.12.0 actual-run"); expect(html).toContain("未提取数值版本"); expect(html).toContain("探测成功");
  expect(html).toContain("/test/actual-work"); expect(html).toContain("/test/actual-task"); expect(html).toContain("actual-gateway");
  expect(run.snapshot.config_hash).toBe("offline-test-hash"); expect(run.snapshot.resolution_hash).toBe("offline-source-hash");
});

test("independent container observation remains visible after later verification failure or cancellation", () => {
  for (const state of ["failed", "cancelled"]) {
    const run = runFixture({ state }); run.snapshot.runtime = observedRuntime;
    const html = renderToStaticMarkup(<><RunOverview run={run} /><RunDiagnostics run={run} /><RunSnapshotDetails run={run} /></>);
    expect(html).toContain("container-cli-development"); expect(html).toContain("2026-01-02T00:01:00Z"); expect(html).toContain("已保存独立观测");
    expect(html).toContain("<dt>实际容器 Agent CLI 版本</dt><dd>container-cli-development</dd>"); expect(html).not.toContain("<dt>实际容器 Agent CLI 版本</dt><dd>host-cli-dev</dd>");
  }
});

test("raw container output without an acknowledged version is not presented as a completed observation", () => {
  const run = runFixture({ state: "running" });
  run.snapshot.runtime = { tools: [], observed_at: "2026-01-02T00:00:00Z", agent_version_output: "agent output only" };
  const html = renderToStaticMarkup(<><RunDiagnostics run={run} /><RunSnapshotDetails run={run} /></>);
  expect(html).toContain("未完成独立版本观测确认"); expect(html).toContain("<dt>实际容器 Agent CLI 版本</dt><dd>尚未观测</dd>"); expect(html).not.toContain("已保存独立观测");
});

test("Git task details use the saved repository, exact SHA and path", () => {
  const run = runFixture(); run.snapshot.registry_kind = "harbor";
  run.snapshot.resolved_tasks = [{ name: "swe-bench/offline-task", kind: "git", path: "tasks/exact-task", git_url: "https://git.invalid/actual-tasks.git", git_commit: "a".repeat(40) }];
  delete run.snapshot.dataset_digest;
  const html = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
  expect(html).toContain("a".repeat(40)); expect(html).toContain("https://git.invalid/actual-tasks.git"); expect(html).toContain("tasks/exact-task"); expect(html).not.toContain("未提供；使用 Package Digest");
});

test("old reports preserve historical versions but do not invent actual runtime or source metadata", () => {
  const run = runFixture({ state: "completed" });
  delete run.snapshot.format_version; delete run.snapshot.resolved_tasks; delete run.snapshot.resolution_hash; delete run.snapshot.resolved_at; delete run.snapshot.preflight_tools; delete run.snapshot.uv_version;
  run.snapshot.harbor_version = "0.25.0"; run.snapshot.spec.agent_version = "1.2.3";
  run.snapshot.suite.task_commit = "b".repeat(40); run.snapshot.suite.task_repository = "https://git.invalid/history.git";
  const html = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
  expect(html).toContain("历史运行快照格式"); expect(html).toContain("历史请求的 Agent 版本（非实际观测）"); expect(html).toContain("1.2.3"); expect(html).toContain("0.25.0"); expect(html).toContain("b".repeat(40));
  expect(html).toContain("<dt>实际 Harbor 版本 / 输出</dt><dd>未记录</dd>"); expect(html).toContain("<dt>实际容器 Agent CLI 版本</dt><dd>未记录</dd>"); expect(html).not.toContain("已保存独立观测");
});

test("diagnostic event text does not expose bearer credentials", () => {
  const html = renderToStaticMarkup(<EventTimeline events={[{ id: 1, run_id: "run_test", timestamp: "2026-01-01T00:00:00Z", kind: "test", message: "Bearer offline-test-only" }]} />);
  expect(html).not.toContain("offline-test-only"); expect(html).toContain("已隐藏");
});
