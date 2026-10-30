import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient } from "@router-forge/api";
import { testEnvironment, testInspection, testModel, testProvider, testResolution, testSuite } from "@router-forge/ui/lib/fixtures.test-support";
import { RunComposer } from "./run-composer";
import { InspectionResults } from "./environments";

test("composer leaves stable metadata unresolved, hides old preview until explicitly requested, and never asks for a version", () => {
  let calls = 0;
  const api = new APIClient("", "", async () => { calls++; return { status: 200, body: "{}" }; });
  const queryClient = new QueryClient();
  queryClient.setQueryData(["environments"], [testEnvironment]);
  queryClient.setQueryData(["models"], [testModel]);
  queryClient.setQueryData(["providers"], [testProvider]);
  queryClient.setQueryData(["benchmarks"], [testSuite]);
  queryClient.setQueryData(["inspection", testEnvironment.id], testInspection);
  queryClient.setQueryData(["benchmark-resolution", "swebench-verified", testEnvironment.id], { ...testResolution, resolution_hash: "cached-preview-only" });
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><RunComposer api={api} environmentID={testEnvironment.id} onEnvironment={() => {}} onDraft={() => {}} /></QueryClientProvider>);
  expect(calls).toBe(0); expect(html).toContain("swe-bench/swe-bench-verified"); expect(html).toContain("解析默认任务来源");
  expect(html).toContain("创建草稿时重新解析实际任务来源"); expect(html).toContain("Harbor 默认容器安装"); expect(html).toContain("未知版本不能替代");
  expect(html).not.toContain("cached-preview-only"); expect(html).not.toContain("run-agent-version"); expect(html).not.toContain("显式容器 Agent 版本"); expect(html).not.toContain("0.25.0"); expect(html).not.toContain("冻结");
  queryClient.clear();
});

test("composer requests full resolution but sends only a new run spec, with no host version autofill or automatic start", () => {
  const source = readFileSync(join(import.meta.dir, "run-composer.tsx"), "utf8");
  expect(source).toContain('api.resolveBenchmark("swebench-verified", environmentID)');
  expect(source).toContain("api.createRun(validated.value)"); expect(source).toContain("后端会重新解析，不采用缓存预览作为批准来源");
  expect(source).not.toMatch(/benchmarkTasks|inspectedVersion|agentVersion|startRun/);
});

test("inspection treats host agent versions as hints and shows successful custom tool version output", () => {
  const inspection = { ...testInspection, tools: [{ name: "harbor", available: true, version: "", version_output: "Harbor custom development build" }] };
  const html = renderToStaticMarkup(<InspectionResults inspection={inspection} />);
  expect(html).toContain("Harbor custom development build"); expect(html).toContain("Harbor 使用默认容器安装"); expect(html).toContain("成功独立观测实际容器 CLI 版本并保存确认后才允许推理");
  expect(html).not.toContain("不能指定固定容器版本"); expect(html).not.toContain("可指定容器版本");
});
