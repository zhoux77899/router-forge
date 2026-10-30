import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient, type Environment, type NetworkProfile } from "@router-forge/api";
import { testEnvironment, testInspection, testModel, testProvider, testResolution, testSuite } from "@router-forge/ui/lib/fixtures.test-support";
import { RunComposer } from "./run-composer";
import { InspectionResults } from "./environments";
import { blankEnvironment, executionNetworkStages } from "../lib/forms";

const networkProfiles: NetworkProfile[] = [...executionNetworkStages, "model" as const].map(stage => ({ id: `network_${stage}`, name: stage, scope: stage === "model" ? "desktop" : "environment", ...(stage === "model" ? {} : { environment_id: testEnvironment.id }), revision: "offline-revision", created_at: "2026-01-01T00:00:00Z", proxy_mode: "direct", tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false, mirrors: { apt: { mode: "native" }, pypi: { mode: "native" }, npm: { mode: "native" }, node: { mode: "native" }, uv: { mode: "native" }, uv_python: { mode: "native" } } }));
const configuredEnvironment: Environment = { ...testEnvironment, networks: { source_profile_id: "network_source", build_profile_id: "network_build", agent_profile_id: "network_agent", verifier_profile_id: "network_verifier" }, docker_gateway_mode: "detect" };

test("composer leaves stable metadata unresolved, hides old preview until explicitly requested, and never asks for a version", () => {
  let calls = 0;
  const api = new APIClient("", "", async () => { calls++; return { status: 200, body: "{}" }; });
  const queryClient = new QueryClient();
  queryClient.setQueryData(["environments"], [configuredEnvironment]);
  queryClient.setQueryData(["network-profiles"], networkProfiles);
  queryClient.setQueryData(["models"], [testModel]);
  queryClient.setQueryData(["providers"], [{ ...testProvider, network_profile_id: "network_model" }]);
  queryClient.setQueryData(["benchmarks"], [testSuite]);
  queryClient.setQueryData(["inspection", testEnvironment.id], testInspection);
  queryClient.setQueryData(["benchmark-resolution", "swebench-verified", testEnvironment.id], { ...testResolution, resolution_hash: "cached-preview-only" });
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><RunComposer api={api} environmentID={testEnvironment.id} onEnvironment={() => {}} onDraft={() => {}} /></QueryClientProvider>);
  expect(calls).toBe(0); expect(html).toContain("swe-bench/swe-bench-verified"); expect(html).toContain("解析默认任务来源");
  expect(html).toContain('data-slot="field-set"'); expect(html).toContain('data-slot="field-group"'); expect(html).toContain('data-slot="field-legend"');
  expect(html).toContain('form="run-form"'); expect(html).not.toMatch(/<(?:details|summary)\b/);
  const taskTrigger = (html.match(/<button\b[^>]*>/g) ?? []).find(button => button.includes('id="run-task"'));
  expect(taskTrigger).toMatch(/\sdisabled(?:=|[ >])/);
  expect(html).toContain("创建草稿时重新解析实际任务来源"); expect(html).toContain("Harbor 默认容器安装"); expect(html).toContain("未知版本不能替代");
  expect(html).not.toContain("cached-preview-only"); expect(html).not.toContain("run-agent-version"); expect(html).not.toContain("显式容器 Agent 版本"); expect(html).not.toContain("0.25.0"); expect(html).not.toContain("冻结");
  queryClient.clear();
});

test("composer requires explicit profile migration and does not inherit any historical TLS or proxy flags", () => {
  for (const skip_tls_verification of [undefined, false, true]) {
    let calls = 0;
    const api = new APIClient("", "", async () => { calls++; throw new Error("Rendering must not send benchmark requests"); });
    const queryClient = new QueryClient();
    queryClient.setQueryData(["environments"], [{ ...testEnvironment, skip_tls_verification, networks: null, proxy_url: "https://legacy.invalid", docker_host_gateway: "legacy.gateway" }]);
    queryClient.setQueryData(["network-profiles"], networkProfiles);
    queryClient.setQueryData(["inspection", testEnvironment.id], testInspection);
    queryClient.setQueryData(["models"], [testModel]); queryClient.setQueryData(["providers"], [testProvider]); queryClient.setQueryData(["benchmarks"], [testSuite]);
    const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><RunComposer api={api} environmentID={testEnvironment.id} onEnvironment={() => {}} onDraft={() => {}} /></QueryClientProvider>);
    expect(html).toContain("Execution network setup required"); expect(html).toContain("Select Docker gateway detection or a manual host address");
    expect(html).toContain("Source network required"); expect(html).not.toContain("检查通过");
    expect(html).not.toContain("已保存 Harbor TLS 策略"); expect(html).not.toContain("MITM"); expect(html).not.toContain("legacy.gateway"); expect(html).not.toContain("https://legacy.invalid");
    const buttons = html.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? [];
    expect(buttons.find(button => button.includes("检查环境"))).toMatch(/\sdisabled(?:=|[ >])/);
    expect(buttons.find(button => button.includes("解析默认任务来源"))).toMatch(/\sdisabled(?:=|[ >])/);
    expect(calls).toBe(0); queryClient.clear();
  }
});

test("inspection and source resolution require only this host Source profile, while draft creation needs all stages", () => {
  const queryClient = new QueryClient();
  const partial: Environment = { ...testEnvironment, networks: { ...blankEnvironment().networks, source_profile_id: "network_source" } };
  queryClient.setQueryData(["environments"], [partial]); queryClient.setQueryData(["network-profiles"], networkProfiles);
  queryClient.setQueryData(["inspection", partial.id], testInspection); queryClient.setQueryData(["benchmarks"], [testSuite]);
  queryClient.setQueryData(["models"], [testModel]); queryClient.setQueryData(["providers"], [testProvider]);
  let calls = 0; const api = new APIClient("", "", async () => { calls++; throw new Error("No automatic operations"); });
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><RunComposer api={api} environmentID={partial.id} onEnvironment={() => {}} onDraft={() => {}} /></QueryClientProvider>);
  const buttons = html.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? [];
  expect(buttons.find(button => button.includes("检查环境"))).not.toMatch(/\sdisabled(?:=|[ >])/);
  expect(buttons.find(button => button.includes("解析默认任务来源"))).not.toMatch(/\sdisabled(?:=|[ >])/);
  expect(html).toContain("Select a saved build network profile"); expect(html).toContain("Select a saved agent network profile"); expect(html).toContain("Select a saved verifier network profile");
  expect(buttons.find(button => button.includes("创建运行草稿（不启动）"))).toMatch(/\sdisabled(?:=|[ >])/);
  expect(calls).toBe(0); queryClient.clear();
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
