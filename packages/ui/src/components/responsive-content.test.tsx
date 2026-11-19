import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { compile } from "tailwindcss";
import { RequestTable } from "./request-table";
import { EventTimeline, RunDiagnostics } from "./run-events";
import { RunOverview } from "./run-summary";
import { RunSnapshotDetails } from "./frozen-config";
import { Table, TableBody, TableCaption, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "./ui/table";
import { attemptFixture, requestFixture, runFixture } from "../lib/fixtures.test-support";

const responsiveComponents = ["ui\\table.tsx", "request-table.tsx", "results-workspace.tsx", "run-events.tsx", "run-summary.tsx", "frozen-config.tsx"];
const componentSource = (name: string) => readFileSync(join(import.meta.dir, name), "utf8");

test("responsive tables keep semantic headers, labelled cells, spanning details and footers", () => {
  for (const responsive of ["stacked", "stacked-wide"] as const) {
    const html = renderToStaticMarkup(<Table responsive={responsive} aria-label="Responsive records">
      <TableCaption>All records</TableCaption>
      <TableHeader><TableRow><TableHead>Identity</TableHead><TableHead>Value</TableHead></TableRow></TableHeader>
      <TableBody>
        <TableRow><TableCell label="Identity"><code>{"record-".repeat(40)}</code></TableCell><TableCell label="Value">USD 0.00000000000000000001</TableCell></TableRow>
        <TableRow><TableCell colSpan={2}>Expanded association remains full width</TableCell></TableRow>
      </TableBody>
      <TableFooter><TableRow><TableCell colSpan={2}>All fields retained</TableCell></TableRow></TableFooter>
    </Table>);
    expect(html).toContain(`data-responsive="${responsive}"`);
    expect(html).toContain("@container/table");
    expect(html).toContain('role="table"');
    expect(html).toContain('role="rowgroup"');
    expect(html).toContain('role="row"');
    expect(html.match(/role="columnheader"/g)).toHaveLength(2);
    expect(html.match(/scope="col"/g)).toHaveLength(2);
    expect(html.match(/data-slot="table-cell-label"/g)).toHaveLength(2);
    for (const label of ["Identity", "Value"]) expect(html).toContain(`data-label="${label}"`);
    for (const value of ["record-".repeat(40), "USD 0.00000000000000000001", "Expanded association remains full width", "All fields retained", "All records"]) expect(html).toContain(value);
    expect(html.match(/colSpan="2"/g)).toHaveLength(2);
    expect(html).not.toContain("overflow-x");
  }
  const wrapped = renderToStaticMarkup(<Table><TableBody><TableRow><TableCell>Always wrapped</TableCell></TableRow></TableBody></Table>);
  expect(wrapped).toContain("table-fixed");
  expect(wrapped).toContain("whitespace-normal");
  expect(wrapped).toContain("[overflow-wrap:anywhere]");
  expect(wrapped).not.toContain("data-responsive=");
  expect(wrapped).not.toContain("table-cell-label");
});

test("all nine request fields and full association identifiers survive the stacked presentation", () => {
  const request = requestFixture({
    id: `request-${"a".repeat(160)}`, task_id: `task-${"b".repeat(160)}`, attempt_id: `attempt-${"c".repeat(160)}`,
    requested_model: "requested-full-model", actual_model: "observed-full-model",
    upstream_request_id: `upstream-request-${"d".repeat(160)}`, upstream_response_id: `upstream-response-${"e".repeat(160)}`,
    http_status: 0, duration_ms: 0, cost_amount: "0.00000000000000000001", currency: "USD", cost_source: "provider-recorded",
    usage: { input_tokens: 0, output_tokens: 4, cache_read_tokens: null, cache_write_tokens: 0, reasoning_tokens: 2, total_tokens: 4, completeness: "partial", source: "observed-upstream-usage" },
  });
  const html = renderToStaticMarkup(<RequestTable requests={[request, requestFixture({ id: "second-request" })]} />);
  expect(html).toContain('data-responsive="stacked-wide"');
  expect(html.match(/data-slot="table-cell-label"/g)).toHaveLength(18);
  for (const label of ["请求 / 时间", "协议 / 类型", "状态 / HTTP", "耗时 / 首字节", "输入 / 输出", "缓存读 / 写", "推理 / 总量", "成本 / 来源", "请求关联"]) {
    expect(html.split(`data-label="${label}"`)).toHaveLength(3);
  }
  for (const value of [request.id, request.task_id!, request.attempt_id!, request.upstream_request_id!, request.upstream_response_id!, "requested-full-model / observed-full-model", "observed-upstream-usage", "provider-recorded", "second-request", "HTTP 0", "0 ms", "未知 / 0", "2 / 4", "USD 0.00000000000000000001"]) expect(html).toContain(value);
  expect(html).toContain('aria-expanded="false"');
  expect(html).toContain('data-slot="collapsible-content"');
  expect(html).not.toMatch(/truncate|overflow-x|min-w-\[(?:\d+)/);
});

test("snapshot, events and attempts retain long evidence and independent labelled outcomes", () => {
  const longPath = `D:\\records\\${"long-evidence-".repeat(30)}\\agent.exe`;
  const run = runFixture({ state: "completed", tasks: [{ id: "task_test", run_id: "run_test", name: "offline-task", state: "completed", attempts: [attemptFixture({ id: "full-attempt-".repeat(30), reward: 0, exit_code: 0, exception_type: "IndependentInfraEvidence", result_path: longPath })] }] });
  const runtime = { observed_at: "2026-01-01T00:00:00Z", tools: [{ name: "harbor", available: true, path: longPath, version_output: `actual-build-${"f".repeat(180)}` }], agent_version_output: `full-cli-output-${"g".repeat(180)}` };
  run.snapshot.runtime = runtime;
  const snapshot = renderToStaticMarkup(<RunSnapshotDetails run={run} />);
  for (const label of ["预检工具", "实际工具", "探测 / 版本", "原始 --version 输出"]) expect(snapshot).toContain(`data-label="${label}"`);
  for (const value of [longPath, runtime.tools[0]!.version_output, runtime.agent_version_output]) expect(snapshot).toContain(value);
  const diagnostics = renderToStaticMarkup(<RunDiagnostics run={run} />);
  expect(diagnostics).toContain(longPath);
  expect(diagnostics).toContain(runtime.agent_version_output);
  const overview = renderToStaticMarkup(<RunOverview run={run} />);
  for (const label of ["尝试", "执行状态", "基础设施", "CLI 退出码", "Harbor 奖励", "开始 / 结束"]) expect(overview).toContain(`data-label="${label}"`);
  expect(overview).toContain("full-attempt-".repeat(30));
  expect(overview).toContain("IndependentInfraEvidence");
  expect(overview).toContain("奖励 0 · 未解题");
  const events = renderToStaticMarkup(<EventTimeline events={[{ id: 1, run_id: run.id, attempt_id: "full-attempt-".repeat(30), timestamp: "2026-01-01T00:00:00Z", kind: "event-".repeat(40), message: longPath }]} />);
  for (const label of ["时间", "事件", "诊断"]) expect(events).toContain(`data-label="${label}"`);
  expect(events).toContain(longPath);
  expect(events).toContain("event-".repeat(40));
  for (const html of [snapshot, diagnostics, overview, events]) expect(html).not.toMatch(/overflow-x|truncate|min-w-\[(?:\d+)/);
});

test("responsive seams have no fixed minimum widths, clipping, sticky subheaders or horizontal scrollers", () => {
  for (const name of responsiveComponents) {
    const source = componentSource(name);
    expect(source).not.toMatch(/overflow-x-(?:auto|scroll|hidden)|\btruncate\b/);
    expect(source).not.toMatch(/\bmin-w-(?!0\b)(?:\[[^\]]+\]|\d+)/);
  }
  const workspace = componentSource("results-workspace.tsx");
  expect(workspace).toContain("@6xl/workspace:grid-cols-[minmax(0,18rem)_minmax(0,1fr)]");
  expect(workspace).toContain("w-full flex-wrap justify-start");
  expect(workspace).not.toMatch(/\bsticky\b|(?:^|\s)xl:grid-cols/);
});

test("installed Tailwind compiles the actual table and workspace container-width variants", async () => {
  const candidates = responsiveComponents.flatMap(name => Array.from(componentSource(name).matchAll(/"([^"\n]+)"/g)).flatMap(match => match[1]!.split(/\s+/)));
  const theme = readFileSync(Bun.resolveSync("tailwindcss/theme.css", import.meta.dir), "utf8");
  const compiler = await compile(`${theme}\n@tailwind utilities;`);
  const css = compiler.build(candidates);
  for (const query of ["@container table (width >= 48rem)", "@container table (width >= 64rem)", "@container workspace (width >= 72rem)"]) expect(css).toContain(query);
  for (const rule of ["container-name: table", "container-type: inline-size", "display: table-header-group", "display: table-row-group", "display: table-row", "display: table-cell", "table-layout: fixed", "white-space: pre-wrap", "overflow-wrap: anywhere"]) expect(css).toContain(rule);
  expect(css).not.toContain("overflow-x: auto");
});
