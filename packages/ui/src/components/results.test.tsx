import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RequestTable } from "./request-table";
import { ResultsWorkspace } from "./results-workspace";
import { MeteringSummary, RunDetailHeader, RunOverview } from "./run-summary";
import { EventTimeline, RunDiagnostics } from "./run-events";
import { FrozenNetworkDetails, RunNetworkWarnings, RunSnapshotDetails } from "./frozen-config";
import { EmptyState, ErrorNotice, HarborTLSWarning, LoadingState } from "./feedback";
import type { NetworkMirrors, NetworkProfile, NetworkSnapshot, Page, Run, RuntimeSnapshot } from "@router-forge/api";
import type { ResultsClient } from "../lib/read-client";
import { attemptFixture, requestFixture, runFixture, testSummary } from "../lib/fixtures.test-support";

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
  expect(html).toContain("Harbor 默认容器安装"); expect(html).not.toContain("0.25.0"); expect(html).toContain("历史网络未显式配置");
});

test("TLS risk notice identifies MITM, scoped downloads and non-bypassed network failures", () => {
  const html = renderToStaticMarkup(<HarborTLSWarning />);
  expect(html).toContain('role="alert"'); expect(html).toContain("不验证服务器身份"); expect(html).toContain("MITM"); expect(html).toContain("元数据、下载内容或安装脚本");
  expect(html).toContain("受控 Harbor 进程与运行时任务容器的下载");
  expect(html).toContain("不改变模型服务商 HTTPS、SSH 主机校验、操作系统信任库或 Docker daemon 的镜像拉取");
  expect(html).toContain("不能解决代理 407 认证、Docker IPAM 地址池冲突或原始 Dockerfile 构建中的 TLS 错误");
});

test("legacy TLS readouts retain explicit saved values without inferring defaults or connection results", () => {
  for (const state of ["draft", "completed"]) for (const skip_tls_verification of [undefined, false, true]) {
    const run = runFixture({ state });
    run.snapshot.environment = { ...run.snapshot.environment, skip_tls_verification, ca_cert_path: "/test/approved.pem" };
    if (state === "completed") run.snapshot.runtime = observedRuntime;
    const before = JSON.stringify(run);
    const snapshotHTML = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
    const overviewHTML = renderToStaticMarkup(<RunOverview run={run} />);
    for (const html of [snapshotHTML, overviewHTML]) {
      expect(html).toContain("历史 Harbor TLS 设置"); expect(html).toContain("不是实际 TLS 连接验证结果");
      expect(html).toContain("历史网络未显式配置"); expect(html).toContain("不推定代理、CA、TLS 或镜像策略");
      expect(html).not.toContain("<dt>实际 TLS"); expect(html).not.toContain("严格验证 Harbor TLS（默认）");
      if (skip_tls_verification === true) { expect(html).toContain("跳过 Harbor TLS 验证（不安全）"); expect(html).toContain("MITM"); }
      else if (skip_tls_verification === false) { expect(html).toContain("未跳过 Harbor TLS 验证（历史已保存）"); expect(html).not.toContain("MITM"); }
      else { expect(html).toContain("未记录（不推定默认策略）"); expect(html).not.toContain("MITM"); }
      if (state === "completed") expect(html).toContain("container-cli-development");
    }
    expect(snapshotHTML).toContain("/test/approved.pem"); expect(snapshotHTML).not.toContain("已保留，执行时忽略");
    const actualSection = snapshotHTML.slice(snapshotHTML.indexOf("开始后的实际执行观测（非批准配置）"), snapshotHTML.indexOf("独立容器 Agent 观测（推理前必须完成）"));
    expect(actualSection).not.toContain("TLS"); expect(JSON.stringify(run)).toBe(before);
  }
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
  expect(html).toContain("未显式配置分阶段网络"); expect(html).toContain("未记录（不推定默认策略）"); expect(html).not.toContain("严格验证 Harbor TLS（默认）"); expect(html).not.toContain("MITM");
  expect(run.snapshot.environment.skip_tls_verification).toBeUndefined(); expect(run.snapshot.runtime).toBeUndefined(); expect(run.snapshot.harbor_version).toBe("0.25.0"); expect(run.snapshot.spec.agent_version).toBe("1.2.3");
});

test("diagnostic event text does not expose bearer credentials", () => {
  const html = renderToStaticMarkup(<EventTimeline events={[{ id: 1, run_id: "run_test", timestamp: "2026-01-01T00:00:00Z", kind: "test", message: "Bearer offline-test-only" }]} />);
  expect(html).not.toContain("offline-test-only"); expect(html).toContain("已隐藏");
});

test("feedback uses accessible shadcn empty, loading and error states without mandatory descriptions", () => {
  const empty = renderToStaticMarkup(<EmptyState title="没有数据" />);
  expect(empty).toContain('data-slot="empty"'); expect(empty).toContain('data-slot="empty-title"');
  expect(empty).not.toContain('data-slot="empty-description"');
  const loading = renderToStaticMarkup(<LoadingState label="正在读取选中运行" />);
  expect(loading).toContain('role="status"'); expect(loading).toContain('aria-label="正在读取选中运行"'); expect(loading).toContain('aria-busy="true"');
  expect(loading.match(/data-slot="skeleton"/g)).toHaveLength(3);
  const error = renderToStaticMarkup(<ErrorNotice error={new Error("password=NEVER_PASSWORD")} title="读取失败" retry={() => {}} />);
  expect(error).toContain('role="alert"'); expect(error).toContain("读取失败"); expect(error).toContain("重新读取");
  expect(error).toContain("已隐藏"); expect(error).not.toContain("NEVER_PASSWORD");
});

test("request associations expand accessibly while null, observed zero and independent total tokens stay intact", () => {
  const request = requestFixture({ duration_ms: 0, first_byte_ms: null, http_status: 0, requested_model: "requested-only", actual_model: "actual-only", cost_amount: "0.00000000000000000001", currency: "USD", cost_source: "provider", usage: { input_tokens: 0, output_tokens: 4, cache_read_tokens: null, cache_write_tokens: 0, reasoning_tokens: 2, total_tokens: 4, source: "raw-upstream-usage", completeness: "partial" } });
  Object.assign(request, { raw_usage: { prompt: "NEVER_RAW_USAGE_PROMPT", output: "NEVER_RAW_USAGE_OUTPUT" } });
  const html = renderToStaticMarkup(<RequestTable requests={[request]} />);
  expect(html).toContain('aria-label="请求账本"'); expect(html).toContain('aria-label="查看请求 req_test 的关联"');
  expect(html).toContain('aria-expanded="false"'); expect(html).toContain('data-slot="collapsible-content"'); expect(html).not.toContain("<details");
  expect(html).toContain("requested-only / actual-only"); expect(html).toContain("raw-upstream-usage"); expect(html).toContain("部分");
  expect(html).toContain("HTTP 0"); expect(html).toContain("0 ms"); expect(html).toContain("未知 / 0"); expect(html).toContain("2 / 4");
  expect(html).toContain("USD 0.00000000000000000001");
  expect(html).not.toContain("NEVER_RAW_USAGE_PROMPT"); expect(html).not.toContain("NEVER_RAW_USAGE_OUTPUT");
});

test("execution lifecycle, infrastructure exceptions, CLI exit code and Harbor reward have separate columns", () => {
  const run = runFixture({ state: "failed", tasks: [{ id: "task_test", run_id: "run_test", name: "offline-task", state: "completed", attempts: [attemptFixture({ reward: 0, exception_type: "VerifierSetupError", exit_code: 0 }), attemptFixture({ id: "attempt_unknown", state: "cancelled", reward: null, exit_code: null }), attemptFixture({ id: "attempt_scored", state: "failed", reward: 1, exit_code: 1 })] }] });
  const before = JSON.stringify(run);
  const html = renderToStaticMarkup(<><RunDetailHeader run={run} /><RunOverview run={run} /></>);
  for (const column of ["执行状态", "基础设施", "CLI 退出码", "Harbor 奖励"]) expect(html).toContain(column);
  expect(html).toContain("执行失败"); expect(html).toContain("执行完成"); expect(html).toContain("已取消");
  expect(html).toContain("基础设施 / 执行异常 · VerifierSetupError"); expect(html).toContain("奖励 0 · 未解题"); expect(html).toContain("评分未知"); expect(html).toContain("奖励 1 · 通过评分");
  expect(JSON.stringify(run)).toBe(before);
});

test("summary discloses incomplete accounting instead of converting unknown optional totals into observed zero", () => {
  const html = renderToStaticMarkup(<MeteringSummary summary={{ ...testSummary, unknown_usage_count: 1, input_tokens: 0, output_tokens: 0, costs: [] }} />);
  expect(html).toContain("1 条用量未知或不完整"); expect(html).toContain("1 条成本未知"); expect(html).toContain("成本未知");
  expect(html).toContain("NULL 表示未知"); expect(html).toContain("不再次叠加进输出或总量"); expect(html).not.toContain("已知 0");
  expect(html).toContain('aria-expanded="false"');
});

test("snapshot provenance and runtime evidence are retained in correctly hidden accessible accordion sections", () => {
  const run = runFixture({ state: "completed" }); run.snapshot.runtime = observedRuntime;
  const before = JSON.stringify(run);
  const html = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
  expect(html.match(/data-slot="accordion-content"/g)).toHaveLength(8);
  expect(html.match(/<div(?=[^>]*data-slot="accordion-content")(?=[^>]*data-state="closed")(?=[^>]*hidden="")[^>]*>/g)).toHaveLength(7);
  expect(html).toContain('aria-expanded="true"'); expect(html).toContain('aria-expanded="false"'); expect(html).toContain("aria-controls=");
  expect(html).toContain("offline-source-hash"); expect(html).toContain("offline-test-hash"); expect(html).toContain("container-cli-development"); expect(html).toContain("2026-01-02T00:01:00Z");
  expect(html).not.toContain("运行快照保存草稿创建时的配置与实际解析来源"); expect(html).not.toContain("<details");
  expect(JSON.stringify(run)).toBe(before);
});

test("expanded snapshot and request metadata redact credential-bearing URLs and diagnostic outputs", () => {
  const run = runFixture({ state: "completed" });
  run.snapshot.provider = { ...run.snapshot.provider, base_url: "https://user:NEVER_PROVIDER_PASSWORD@provider.invalid/v1?api_key=NEVER_PROVIDER_KEY" };
  run.snapshot.agent = { ...run.snapshot.agent, version: "password=NEVER_HOST_PASSWORD" };
  run.snapshot.runtime = { ...observedRuntime, tools: [{ name: "harbor", available: true, version: "password=NEVER_TOOL_PASSWORD", version_output: "authorization=NEVER_TOOL_AUTH" }] };
  const request = requestFixture({ upstream_request_id: "authorization=NEVER_REQUEST_AUTH", cost_source: "password=NEVER_COST_PASSWORD" });
  const html = renderToStaticMarkup(<><RunSnapshotDetails run={run} /><RunDiagnostics run={run} /><RequestTable requests={[request]} /></>);
  for (const secret of ["NEVER_PROVIDER_PASSWORD", "NEVER_PROVIDER_KEY", "NEVER_HOST_PASSWORD", "NEVER_TOOL_PASSWORD", "NEVER_TOOL_AUTH", "NEVER_REQUEST_AUTH", "NEVER_COST_PASSWORD"]) expect(html).not.toContain(secret);
  expect(html).toContain("已隐藏");
});

test("diagnostics preserve saved attempt identity and image digest without exposing raw Harbor payloads", () => {
  const digest = `sha256:${"f".repeat(64)}`;
  const run = runFixture({ state: "completed", tasks: [{ id: "task_test", run_id: "run_test", name: "offline-task", state: "completed", attempts: [attemptFixture({ image_digests: [digest], result_path: "/offline/result.json", harbor_metrics: { raw_prompt: "NEVER_HARBOR_PROMPT", raw_output: "NEVER_HARBOR_OUTPUT" } })] }] });
  run.snapshot.runtime = observedRuntime;
  const html = renderToStaticMarkup(<RunDiagnostics run={run} />);
  expect(html).toContain('aria-label="执行记录"'); expect(html).toContain('data-slot="accordion"'); expect(html).toContain('hidden=""');
  expect(html).toContain(digest); expect(html).toContain("/offline/result.json"); expect(html).toContain("task_test / attempt_test"); expect(html).toContain("已保存独立观测");
  expect(html).not.toContain("NEVER_HARBOR_PROMPT"); expect(html).not.toContain("NEVER_HARBOR_OUTPUT");
});

function renderWorkspace(page: Page<Run>, selected: Run, attach = false) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(["runs", 20, 0], page);
  queryClient.setQueryData(["run", selected.id], selected);
  if (attach) queryClient.setQueryData(["attach-run"], selected.id);
  let reads = 0;
  const unexpectedRead = async () => { reads++; throw new Error("Offline cached rendering must not fetch"); };
  const client: ResultsClient = { status: unexpectedRead, runs: unexpectedRead, run: unexpectedRead, requests: unexpectedRead, events: unexpectedRead };
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><ResultsWorkspace client={client} selectedRunID={attach ? undefined : selected.id} runActions={run => <span data-selected-action={run.id} />} /></QueryClientProvider>);
  queryClient.clear();
  return { html, reads };
}

test("a selected run outside the current list page stays attached and receives its own action slot", () => {
  const selected = runFixture({ id: "run_older", state: "completed" }); selected.snapshot.spec.task_names = ["older-selected-task"];
  const newest = runFixture({ id: "run_newest", state: "completed" }); newest.snapshot.spec.task_names = ["newest-list-task"];
  const { html, reads } = renderWorkspace({ items: [newest], total: 45 }, selected);
  expect(html).toMatch(/data-slot="card-title"[^>]*>older-selected-task</); expect(html).toContain('data-selected-action="run_older"');
  expect(html).not.toContain('aria-current="true"'); expect(html).toContain('aria-label="查看运行 newest-list-task · run_newest"');
  expect(html).toContain('aria-label="运行分页"'); expect(html).toContain('aria-label="上一页运行"'); expect(html).toContain('aria-label="下一页运行"'); expect(html).toContain("1–20 / 45");
  expect(html).toContain('role="tablist"'); expect(html).toContain('data-variant="line"'); expect(html).toContain("快照"); expect(reads).toBe(0);
});

test("automatic attachment retains a cached active run rather than substituting the newest page item", () => {
  const active = runFixture({ id: "run_active_older", state: "running" }); active.snapshot.spec.task_names = ["active-attachment-task"];
  const newest = runFixture({ id: "run_newest", state: "completed" }); newest.snapshot.spec.task_names = ["newest-list-task"];
  const { html, reads } = renderWorkspace({ items: [newest], total: 1 }, active, true);
  expect(html).toMatch(/data-slot="card-title"[^>]*>active-attachment-task</); expect(html).toContain('data-selected-action="run_active_older"');
  expect(html).toContain("执行中"); expect(html).not.toContain('aria-current="true"'); expect(reads).toBe(0);
});

test("selected list rows use table selection state and a named shadcn button", () => {
  const selected = runFixture();
  const { html } = renderWorkspace({ items: [selected], total: 1 }, selected);
  expect(html).toContain('aria-label="持久化评测运行"'); expect(html).toContain('data-state="selected"'); expect(html).toContain('aria-current="true"');
  expect(html).toContain('data-slot="button"'); expect(html).toContain('aria-label="刷新运行列表"');
});

function networkProfileFixture(id: string, overrides: Partial<NetworkProfile> = {}): NetworkProfile {
  return {
    id: `frozen-${id}`, name: `保存的 ${id} 配置`, revision: `saved-${id}-revision-7`, scope: "environment", environment_id: "env_test",
    proxy_mode: "direct", proxy_targets: [], no_proxy: "", proxy_auth: "none", relay_mode: "loopback",
    tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false, created_at: "2026-01-01T00:00:00Z",
    mirrors: { apt: { mode: "native" }, pypi: { mode: "native" }, npm: { mode: "native" }, node: { mode: "native" }, uv: { mode: "native" }, uv_python: { mode: "native" } },
    ...overrides,
  };
}

function frozenNetworkFixture(): NetworkSnapshot {
  return {
    source: networkProfileFixture("source"), build: networkProfileFixture("build"), agent: networkProfileFixture("agent"), verifier: networkProfileFixture("verifier"),
    model: networkProfileFixture("model", { scope: "desktop", environment_id: undefined }),
  };
}

function profiledRunFixture(overrides: Partial<Run> = {}): Run {
  const run = runFixture(overrides);
  run.snapshot.format_version = 3; run.snapshot.network = frozenNetworkFixture();
  return run;
}

function networkStageMarkup(html: string, stage: keyof NetworkSnapshot) {
  const marker = `data-network-stage="${stage}"`;
  const start = html.indexOf(marker);
  expect(start).toBeGreaterThanOrEqual(0);
  const next = html.indexOf("data-network-stage=", start + marker.length);
  return html.slice(start, next < 0 ? undefined : next);
}

const customMirrors: NetworkMirrors = {
  apt: { mode: "custom", sources: "deb https://apt.saved.invalid/stable stable main\ndeb https://apt.saved.invalid/security stable-security main" },
  pypi: { mode: "custom", url: "https://pypi.saved.invalid/simple" }, npm: { mode: "custom", url: "https://npm.saved.invalid/registry" },
  node: { mode: "custom", url: "https://node.saved.invalid/dist" }, uv: { mode: "custom", url: "https://uv.saved.invalid/simple" },
  uv_python: { mode: "custom", url: "https://python.saved.invalid/downloads" },
};

test("format-3 networking retains all frozen stages, exact public fields and six source policies", () => {
  const run = profiledRunFixture({ state: "completed" });
  const network = frozenNetworkFixture();
  network.source = networkProfileFixture("source", {
    proxy_mode: "proxy", proxy_url: "https://proxy.saved.invalid:3128", proxy_targets: ["https", "http", "all"], no_proxy: "localhost,.saved.invalid",
    proxy_auth: "basic", relay_mode: "docker", trust_mode: "custom", ca_cert_path: "/saved/source-only-ca.pem", has_credentials: true,
    credential_revision: "credential-source-revision-9", mirrors: customMirrors,
  });
  network.model = networkProfileFixture("model", { scope: "desktop", environment_id: undefined, trust_mode: "custom", ca_cert_path: "/saved/model-only-ca.pem", has_credentials: true, credential_revision: "credential-model-revision-11" });
  run.snapshot.network = network;
  const before = JSON.stringify(run);
  const html = renderToStaticMarkup(<FrozenNetworkDetails run={run} />);
  expect(html).toContain('aria-label="分阶段冻结网络配置"'); expect(html.match(/data-network-stage=/g)).toHaveLength(5);
  for (const stage of ["source", "build", "agent", "verifier", "model"] as const) {
    const section = networkStageMarkup(html, stage);
    expect(section).toContain(network[stage].id); expect(section).toContain(network[stage].revision); expect(section).toContain(network[stage].name); expect(section).toContain(network[stage].created_at);
    expect(section).toContain("验证 TLS"); expect(section).toContain("保存时未确认");
  }
  const source = networkStageMarkup(html, "source");
  for (const value of ["env_test", "https://proxy.saved.invalid:3128/", "https, http, all", "localhost,.saved.invalid", "basic", "docker", "/saved/source-only-ca.pem", "credential-source-revision-9", "已配置（不回显）", "自定义 CA", "APT", "PyPI", "npm", "Node 下载", "uv 索引", "uv Python 下载"]) expect(source).toContain(value);
  for (const mirror of Object.values(customMirrors)) {
    if (mirror.url) expect(source).toContain(mirror.url);
    if (mirror.sources) for (const line of mirror.sources.split("\n")) expect(source).toContain(line);
  }
  expect(source.match(/>自定义</g)).toHaveLength(6);
  const model = networkStageMarkup(html, "model"); expect(model).toContain("桌面宿主"); expect(model).toContain("credential-model-revision-11"); expect(model).toContain("/saved/model-only-ca.pem"); expect(model).not.toContain("/saved/source-only-ca.pem");
  const build = networkStageMarkup(html, "build"); expect(build).toContain("直连"); expect(build).toContain("系统信任库"); expect(build).toContain("原生源（显式选择）"); expect(build).toContain("未配置");
  expect(html).toContain("配置与凭据版本纳入批准 Hash"); expect(html).toContain("不是实际 TLS 连接验证结果"); expect(html).toContain("不代表每个 Trial 通过"); expect(JSON.stringify(run)).toBe(before);
});

test("format-3 networking integrates into existing expandable snapshot and diagnostics without replacing runtime evidence", () => {
  const run = profiledRunFixture({ state: "completed" }); run.snapshot.runtime = observedRuntime;
  const before = JSON.stringify(run);
  for (const html of [renderToStaticMarkup(<RunSnapshotDetails run={run} />), renderToStaticMarkup(<RunOverview run={run} />), renderToStaticMarkup(<RunDiagnostics run={run} />)]) {
    expect(html).toContain("frozen-source"); expect(html).toContain("frozen-model"); expect(html).toContain("saved-verifier-revision-7"); expect(html).toContain("container-cli-development");
    const trigger = html.match(/<button(?=[^>]*aria-label="查看来源下载的冻结网络配置")[^>]*>/)?.[0];
    expect(trigger).toBeDefined(); expect(trigger).toContain('aria-expanded="false"');
    const controlledID = trigger?.match(/aria-controls="([^"]+)"/)?.[1];
    if (controlledID) expect(html).toContain(`id="${controlledID}"`);
    expect(html).toMatch(/<tr(?=[^>]*data-slot="collapsible-content")(?=[^>]*data-state="closed")(?=[^>]*id="[^"]+")(?=[^>]*data-\[state=closed\]:hidden)[^>]*>/);
    expect(html).not.toContain("历史运行快照格式"); expect(html).not.toContain("历史网络未显式配置"); expect(html).not.toContain("历史 Harbor TLS 设置");
    expect(html).not.toContain("已保存独立网络验证"); expect(html).not.toContain("<dt>实际 TLS");
  }
  const snapshotHTML = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
  expect(snapshotHTML.match(/data-slot="accordion-content"/g)).toHaveLength(8);
  expect(snapshotHTML).toContain("草稿预检工具（仅提示）"); expect(snapshotHTML).toContain("Harbor 默认容器安装"); expect(snapshotHTML).toContain("offline-test-hash"); expect(snapshotHTML).toContain("offline-source-hash");
  expect(JSON.stringify(run)).toBe(before);
});

test("execution and model skip risks stay independent and do not become execution or reward outcomes", () => {
  for (const executionSkip of [false, true]) for (const modelSkip of [false, true]) {
    const run = profiledRunFixture({ state: "completed", tasks: [{ id: "task_test", run_id: "run_test", name: "offline-task", state: "completed", attempts: [attemptFixture({ reward: 1, exit_code: 0 })] }] });
    const network = frozenNetworkFixture();
    network.source = { ...network.source, tls_mode: executionSkip ? "skip" : "verify", tls_risk_accepted: executionSkip };
    network.model = { ...network.model, tls_mode: modelSkip ? "skip" : "verify", tls_risk_accepted: modelSkip };
    run.snapshot.network = network;
    const before = JSON.stringify(run);
    const html = renderToStaticMarkup(<RunOverview run={run} />);
    expect(html.includes("执行侧 TLS 跳过配置（不安全）")).toBe(executionSkip); expect(html.includes("模型侧 TLS 跳过配置（不安全）")).toBe(modelSkip);
    if (executionSkip || modelSkip) { expect(html).toContain("不验证服务器身份"); expect(html).toContain("MITM"); }
    if (modelSkip) expect(html).toContain("读取凭据与请求内容或篡改响应");
    expect(networkStageMarkup(html, "build")).toContain("验证 TLS"); expect(networkStageMarkup(html, "verifier")).toContain("验证 TLS");
    expect(html).toContain("执行完成"); expect(html).toContain("奖励 1 · 通过评分"); expect(html).not.toContain("执行失败"); expect(JSON.stringify(run)).toBe(before);
  }
});

test("agent-only skip risk is execution-scoped and saved confirmation is not reconstructed as start approval", () => {
  const run = profiledRunFixture(); const network = frozenNetworkFixture();
  network.agent = { ...network.agent, tls_mode: "skip", tls_risk_accepted: false }; run.snapshot.network = network;
  const html = renderToStaticMarkup(<><RunNetworkWarnings run={run} /><FrozenNetworkDetails run={run} /></>);
  expect(html).toContain("执行侧 TLS 跳过配置（不安全）"); expect(html).not.toContain("模型侧 TLS 跳过配置（不安全）");
  expect(networkStageMarkup(html, "agent")).toContain("保存时未确认"); expect(networkStageMarkup(html, "agent")).toContain("跳过 TLS 验证");
  expect(html).not.toContain("已批准跳过"); expect(html).toContain("原始 Dockerfile 构建与 Verifier 不放宽校验");
});

test("conflicting build or verifier skip metadata remains readable but never appears approved or relaxed", () => {
  for (const stage of ["build", "verifier"] as const) {
    const run = profiledRunFixture({ state: "completed" }); const network = frozenNetworkFixture();
    network[stage] = { ...network[stage], tls_mode: "skip", tls_risk_accepted: true }; run.snapshot.network = network;
    const before = JSON.stringify(run);
    const html = renderToStaticMarkup(<RunOverview run={run} />);
    expect(html).toContain("快照网络边界冲突"); expect(html).toContain("不允许放宽校验"); expect(html).toContain("不据此推定配置可执行");
    expect(networkStageMarkup(html, stage)).toContain("跳过 TLS 验证"); expect(html).not.toContain("执行侧 TLS 跳过配置（不安全）"); expect(html).not.toContain("模型侧 TLS 跳过配置（不安全）");
    expect(run.state).toBe("completed"); expect(JSON.stringify(run)).toBe(before);
  }
});

test("format-3 reports ignore sanitized legacy environment networking and live profile references", () => {
  const run = profiledRunFixture({ state: "completed" });
  run.snapshot.environment = { ...run.snapshot.environment, skip_tls_verification: true, ca_cert_path: "/NEVER_LEGACY_CA.pem", proxy_url: "http://NEVER_LEGACY_PROXY.invalid" };
  Object.assign(run.snapshot.environment, { proxy_username: "NEVER_LEGACY_USERNAME", proxy_password: "NEVER_LEGACY_PASSWORD", networks: { source: "NEVER_LIVE_SOURCE_REFERENCE", build: "NEVER_LIVE_BUILD_REFERENCE", agent: "NEVER_LIVE_AGENT_REFERENCE", verifier: "NEVER_LIVE_VERIFIER_REFERENCE" } });
  run.snapshot.provider = { ...run.snapshot.provider, network_profile_id: "NEVER_LIVE_MODEL_REFERENCE" };
  const before = JSON.stringify(run);
  for (const html of [renderToStaticMarkup(<RunSnapshotDetails run={run} />), renderToStaticMarkup(<RunOverview run={run} />), renderToStaticMarkup(<RunDiagnostics run={run} />)]) {
    for (const marker of ["NEVER_LEGACY_CA", "NEVER_LEGACY_PROXY", "NEVER_LEGACY_USERNAME", "NEVER_LEGACY_PASSWORD", "NEVER_LIVE_SOURCE_REFERENCE", "NEVER_LIVE_BUILD_REFERENCE", "NEVER_LIVE_AGENT_REFERENCE", "NEVER_LIVE_VERIFIER_REFERENCE", "NEVER_LIVE_MODEL_REFERENCE"]) expect(html).not.toContain(marker);
    expect(html).toContain("saved-source-revision-7"); expect(html).toContain("saved-model-revision-7"); expect(html).not.toContain("历史 Harbor TLS 设置"); expect(html).not.toContain("MITM");
  }
  expect(JSON.stringify(run)).toBe(before);
});

test("network metadata never serializes credential values, URL userinfo or mirror authentication", () => {
  const run = profiledRunFixture({ state: "completed" }); const network = frozenNetworkFixture();
  network.source = networkProfileFixture("source", {
    proxy_mode: "proxy", proxy_url: "http://NEVER_URL_USER@proxy.saved.invalid:3128?username=NEVER_QUERY_USER#password=NEVER_FRAGMENT_PASSWORD",
    proxy_auth: "basic", has_credentials: true, credential_revision: "safe-credential-revision",
    mirrors: { ...customMirrors, apt: { mode: "custom", sources: "deb https://NEVER_APT_USER@apt.saved.invalid/stable stable main" }, pypi: { mode: "custom", url: "https://NEVER_MIRROR_USER:NEVER_MIRROR_PASSWORD@pypi.saved.invalid/simple?password=NEVER_QUERY_PASSWORD" } },
  });
  for (const profile of Object.values(network)) Object.assign(profile, { proxy_username: "NEVER_PROFILE_USERNAME", proxy_password: "NEVER_PROFILE_PASSWORD", credentials: { username: "NEVER_NESTED_USERNAME", password: "NEVER_NESTED_PASSWORD" }, raw_payload: "NEVER_RAW_PROFILE" });
  run.snapshot.network = network; const before = JSON.stringify(run);
  for (const html of [renderToStaticMarkup(<RunSnapshotDetails run={run} />), renderToStaticMarkup(<RunOverview run={run} />), renderToStaticMarkup(<RunDiagnostics run={run} />)]) {
    for (const marker of ["NEVER_URL_USER", "NEVER_QUERY_USER", "NEVER_FRAGMENT_PASSWORD", "NEVER_APT_USER", "NEVER_MIRROR_USER", "NEVER_MIRROR_PASSWORD", "NEVER_QUERY_PASSWORD", "NEVER_PROFILE_USERNAME", "NEVER_PROFILE_PASSWORD", "NEVER_NESTED_USERNAME", "NEVER_NESTED_PASSWORD", "NEVER_RAW_PROFILE"]) expect(html).not.toContain(marker);
    expect(html).toContain("proxy.saved.invalid:3128"); expect(html).toContain("apt.saved.invalid/stable"); expect(html).toContain("pypi.saved.invalid/simple"); expect(html).toContain("safe-credential-revision"); expect(html).toContain("已配置（不回显）"); expect(html).not.toContain('type="password"');
  }
  expect(JSON.stringify(run)).toBe(before);
});

test("missing format-3 network or individual stages stays unknown and never falls back to legacy policy", () => {
  for (const network of [undefined, null]) {
    const run = profiledRunFixture({ state: "completed" }); run.snapshot.network = network;
    run.snapshot.environment = { ...run.snapshot.environment, skip_tls_verification: true, ca_cert_path: "/NEVER_FALLBACK_CA.pem" };
    const html = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
    expect(html).toContain("网络快照未记录"); expect(html.match(/未记录（不推定默认策略）/g)).toHaveLength(5);
    expect(html).not.toContain("NEVER_FALLBACK_CA"); expect(html).not.toContain("直连"); expect(html).not.toContain("系统信任库"); expect(html).not.toContain("原生源"); expect(html).not.toContain("MITM");
  }
  const wire = JSON.parse(JSON.stringify(profiledRunFixture({ state: "completed" }))); delete wire.snapshot.network.model;
  const html = renderToStaticMarkup(<RunSnapshotDetails run={wire} />);
  expect(html).toContain("网络快照不完整"); expect(html).toContain("frozen-source"); expect(networkStageMarkup(html, "model")).toContain("未记录（不推定默认策略）"); expect(html).not.toContain("frozen-model");
});

test("optional frozen networking distinguishes explicit empty values from missing mirrors and fields", () => {
  const run = profiledRunFixture(); const network = frozenNetworkFixture();
  network.source = networkProfileFixture("source", { mirrors: null, proxy_targets: undefined, no_proxy: undefined, proxy_auth: undefined, relay_mode: undefined, credential_revision: undefined, ca_cert_path: undefined }); run.snapshot.network = network;
  let html = networkStageMarkup(renderToStaticMarkup(<FrozenNetworkDetails run={run} />), "source");
  expect(html).toContain("<dt>代理目标</dt><dd>未记录</dd>"); expect(html).toContain("<dt>NO_PROXY</dt><dd>未记录</dd>"); expect(html).toContain("<dt>代理凭据版本</dt><dd><code>未记录</code></dd>"); expect(html).not.toContain("原生源"); expect(html).not.toContain("自定义");
  network.source = { ...network.source, mirrors: undefined, proxy_targets: [], no_proxy: "" };
  html = networkStageMarkup(renderToStaticMarkup(<FrozenNetworkDetails run={run} />), "source");
  expect(html).toContain("<dt>代理目标</dt><dd>空列表</dd>"); expect(html).toContain("<dt>NO_PROXY</dt><dd>空</dd>"); expect(html).not.toContain("原生源");
});

test("format-2 and older reports remain unprofiled without invented network defaults or retroactive profiles", () => {
  for (const version of [undefined, 1, 2]) for (const state of ["draft", "completed"]) {
    const run = runFixture({ state }); run.snapshot.format_version = version; run.snapshot.network = frozenNetworkFixture();
    const before = JSON.stringify(run);
    for (const html of [renderToStaticMarkup(<RunSnapshotDetails run={run} />), renderToStaticMarkup(<RunOverview run={run} />), renderToStaticMarkup(<RunDiagnostics run={run} />)]) {
      expect(html).toContain("未显式配置分阶段网络"); expect(html).toContain("不推定代理、CA、TLS 或镜像策略");
      expect(html).not.toContain("frozen-source"); expect(html).not.toContain("frozen-model"); expect(html).not.toContain("原生源"); expect(html).not.toContain("系统信任库"); expect(html).not.toContain("严格验证 Harbor TLS（默认）");
      if (state === "draft") { expect(html).toContain("此草稿不能启动"); expect(html).toContain("请重新创建草稿"); }
      else expect(html).not.toContain("此草稿不能启动");
    }
    expect(JSON.stringify(run)).toBe(before);
  }
});

test("selected or attached runs retain their own frozen networking outside the current page", () => {
  const selected = profiledRunFixture({ id: "run_frozen_older", state: "completed" }); const newest = profiledRunFixture({ id: "run_frozen_newer", state: "completed" });
  const selectedNetwork = frozenNetworkFixture(); selectedNetwork.model = { ...selectedNetwork.model, id: "older-frozen-model-profile", revision: "older-frozen-model-revision", credential_revision: "older-frozen-credential-revision", has_credentials: true }; selected.snapshot.network = selectedNetwork;
  const newestNetwork = frozenNetworkFixture(); newestNetwork.model = { ...newestNetwork.model, id: "NEVER_SUBSTITUTE_NEWER_PROFILE", revision: "NEVER_SUBSTITUTE_NEWER_REVISION" }; newest.snapshot.network = newestNetwork;
  for (const attach of [false, true]) {
    const { html, reads } = renderWorkspace({ items: [newest], total: 40 }, selected, attach);
    expect(html).toContain("older-frozen-model-profile"); expect(html).toContain("older-frozen-model-revision"); expect(html).toContain("older-frozen-credential-revision"); expect(html).not.toContain("NEVER_SUBSTITUTE_NEWER_PROFILE"); expect(html).not.toContain("NEVER_SUBSTITUTE_NEWER_REVISION"); expect(reads).toBe(0);
  }
});
