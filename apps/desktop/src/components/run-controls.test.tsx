import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient } from "@router-forge/api";
import { runFixture } from "@router-forge/ui/lib/fixtures.test-support";
import { RunControls } from "./run-controls";

test("new draft confirmation is unchecked and start button disabled until explicit consent", () => {
  let calls = 0;
  const client = new APIClient("", "", async () => { calls++; return { status: 200, body: "{}" }; });
  const html = renderToStaticMarkup(<QueryClientProvider client={new QueryClient()}><RunControls api={client} run={runFixture()} /></QueryClientProvider>);
  expect(calls).toBe(0); expect(html).toContain('aria-checked="false"'); expect(html).toMatch(/<button[^>]*disabled[^>]*>[^]*?确认并启动这一个任务/); expect(html).toContain("明文"); expect(html).toContain("不是绝对金额上限"); expect(html).toContain("1 / 1 / 0");
});

test("legacy drafts show recreation warning with disabled start but still offer cancellation", () => {
  let calls = 0;
  const client = new APIClient("", "", async () => { calls++; return { status: 200, body: "{}" }; });
  for (const format of [undefined, 1, 3]) {
    const draft = runFixture(); draft.snapshot.format_version = format; draft.snapshot.spec.agent_version = "1.2.3";
    const html = renderToStaticMarkup(<QueryClientProvider client={new QueryClient()}><RunControls api={client} run={draft} /></QueryClientProvider>);
    expect(html).toContain("旧草稿不能启动"); expect(html).toContain("请重新创建草稿"); expect(html).toContain("不会自动迁移旧来源");
    const buttons = html.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? [];
    expect(buttons.find(button => button.includes("确认并启动这一个任务"))).toMatch(/\sdisabled(?:=|[ >])/);
    expect(buttons.find(button => button.includes("取消此草稿"))).not.toMatch(/\sdisabled(?:=|[ >])/);
    expect(html).toContain('aria-checked="false"');
  }
  expect(calls).toBe(0);
});

test("start confirmation displays the authoritative saved package identity and separate approval hashes", () => {
  const draft = runFixture(); draft.snapshot.resolution_hash = "saved-source-not-preview"; draft.snapshot.config_hash = "approved-config-hash";
  draft.snapshot.resolved_at = "2026-02-03T04:05:06Z";
  draft.snapshot.resolved_tasks = [{ name: "swe-bench/offline-task", kind: "package", package_name: "swe-bench/offline-task", ref: `sha256:${"b".repeat(64)}`, revision: "saved-task-revision" }];
  const html = renderToStaticMarkup(<QueryClientProvider client={new QueryClient()}><RunControls api={new APIClient()} run={draft} /></QueryClientProvider>);
  expect(html).toContain("saved-source-not-preview"); expect(html).toContain("approved-config-hash"); expect(html).toContain("2026-02-03T04:05:06Z"); expect(html).toContain(`sha256:${"b".repeat(64)}`);
  expect(html).toContain("未提供；使用 Package Digest"); expect(html).toContain("Harbor 默认容器安装"); expect(html).toContain("独立观测成功并落盘确认后才允许推理");
  expect(html).not.toContain("显式版本"); expect(html).not.toContain("冻结");
});

test("provider input is cleared before write and never stored in mutation/browser cache", () => {
  const source = readFileSync(join(import.meta.dir, "services.tsx"), "utf8");
  expect(source.indexOf('setNewKey("")', source.indexOf("async function saveProvider"))).toBeLessThan(source.indexOf("await api.saveProvider"));
  expect(source).not.toMatch(/useMutation|localStorage|sessionStorage|console\.(?:log|debug|info)/);
  expect(source).toContain('type="password"'); expect(source).toContain('autoComplete="new-password"');
});
