import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient, type NetworkProfile, type NetworkSnapshot } from "@router-forge/api";
import { resultsClient } from "@router-forge/ui";
import { TooltipProvider } from "@router-forge/ui/components/ui/tooltip";
import { runFixture } from "@router-forge/ui/lib/fixtures.test-support";
import { App, ThemeToggle } from "./App";
import { validateProxyOrigin } from "./proxy";

test("Web entry uses same-origin read-only API, no native bridge or control surface", () => {
  const source = readFileSync(join(import.meta.dir, "App.tsx"), "utf8");
  expect(source).toContain('resultsClient(new APIClient(""))');
  expect(source).not.toMatch(/mygo|native-client|networkProfiles|saveNetworkProfile|checkNetworkProfile|\/api\/network-profiles|saveProvider|saveEnvironment|saveModel|createRun|startRun|cancelRun|runActions|localStorage|sessionStorage/);
  const html = renderToStaticMarkup(<QueryClientProvider client={new QueryClient()}><App /></QueryClientProvider>);
  expect(html).toContain("只读 Web"); expect(html).not.toContain('type="password"'); expect(html).not.toContain("确认并启动这一个任务");
});

test("Web offers Results-only navigation and footer settings without a top area", () => {
  const cache = new QueryClient();
  const html = renderToStaticMarkup(<QueryClientProvider client={cache}><App /></QueryClientProvider>);
  expect(html).toContain('aria-label="Web 导航"');
  expect(html).toContain('aria-label="设置"');
  expect(html).toContain('aria-label="切换导航栏"');
  expect(html).toContain('data-slot="sidebar-footer"');
  expect(html).not.toContain("<header");
  expect(html).not.toContain("新建评测");
  cache.clear();
});

test("Vite dev proxy is loopback-only and never takes control credentials", () => {
  expect(validateProxyOrigin(undefined)).toBeUndefined();
  expect(validateProxyOrigin("http://127.0.0.1:1234")).toBe("http://127.0.0.1:1234");
  for (const url of ["https://remote.test", "http://user:pass@127.0.0.1:1234", "http://127.0.0.1:1234?token=test", "http://127.0.0.1:1234/path"]) expect(() => validateProxyOrigin(url)).toThrow();
  const config = readFileSync(join(import.meta.dir, "..", "vite.config.ts"), "utf8");
  expect(config).toContain('host: "127.0.0.1"'); expect(config).toContain("configurePreviewServer: rejectMissingBackend"); expect(config).not.toMatch(/CONTROL_TOKEN|Authorization|VITE_.*KEY/);
});

test("theme actions have accessible toggle state in both Neutral modes", () => {
  for (const dark of [false, true]) {
    const html = renderToStaticMarkup(<TooltipProvider><ThemeToggle dark={dark} onToggle={() => {}} /></TooltipProvider>);
    expect(html).toContain('aria-label="深色主题"'); expect(html).toContain(`aria-pressed="${dark}"`); expect(html).toContain('<button'); expect(html).toContain('aria-hidden="true"');
  }
});

test("Web uses system light or dark without storing credentials or adding configuration controls", () => {
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  try {
    for (const dark of [false, true]) {
      const mediaQueries: string[] = [];
      Object.defineProperty(globalThis, "window", { configurable: true, value: { location: { search: "" }, matchMedia: (query: string) => { mediaQueries.push(query); return { matches: dark }; } } });
      const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
      queryClient.setQueryData(["status"], { running: true, version: "offline-test", pid: 1 });
      queryClient.setQueryData(["runs", 20, 0], { items: [], total: 0 });
      queryClient.setQueryData(["attach-run"], null);
      const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><App /></QueryClientProvider>);
      expect(html).toContain(`data-theme="${dark ? "dark" : "light"}"`);
      expect(mediaQueries).toEqual(["(prefers-color-scheme: dark)"]);
      expect(html).toContain("只读 Web"); expect(html).not.toContain('type="password"'); expect(html).not.toContain("新建评测"); expect(html).not.toContain("网络配置");
      queryClient.clear();
    }
  } finally {
    if (originalWindow) Object.defineProperty(globalThis, "window", originalWindow);
    else Reflect.deleteProperty(globalThis, "window");
  }
  const source = readFileSync(join(import.meta.dir, "App.tsx"), "utf8");
  expect(source).not.toMatch(/networkProfiles|saveNetwork|inspectEnvironment|resolveBenchmark|\.request\(|localStorage|sessionStorage/);
});

test("the Web results transport only makes same-origin unauthenticated GET reads with retained pagination", async () => {
  const originalFetch = globalThis.fetch;
  const calls: { url: string; method: string; authorization: boolean; credentials: RequestCredentials | undefined; hasBody: boolean }[] = [];
  globalThis.fetch = Object.assign(async (url: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    calls.push({ url: String(url), method: init?.method || "", authorization: new Headers(init?.headers).has("Authorization"), credentials: init?.credentials, hasBody: Boolean(init?.body) });
    return new Response(JSON.stringify({ items: [], total: 0 }), { status: 200 });
  }, originalFetch);
  try {
    const client = resultsClient(new APIClient(""));
    await client.status(); await client.runs(20, 20); await client.run("run_selected"); await client.events("run_selected", 19); await client.requests("run_selected", 50, 50);
    expect(calls.map(call => call.url)).toEqual(["/api/status", "/api/runs?limit=20&offset=20", "/api/runs/run_selected", "/api/runs/run_selected/events?after=19", "/api/requests?limit=50&offset=50&run_id=run_selected"]);
    for (const call of calls) { expect(call.method).toBe("GET"); expect(call.authorization).toBe(false); expect(call.credentials).toBe("omit"); expect(call.hasBody).toBe(false); }
    expect(Object.keys(client).sort()).toEqual(["events", "requests", "run", "runs", "status"]);
  } finally { globalThis.fetch = originalFetch; }
});

test("Web renders hash-bound snapshot networking through GET-only run reads without profile APIs or controls", async () => {
  const run = runFixture({ id: "run_frozen_web", state: "completed" });
  const profile: NetworkProfile = {
    id: "web-frozen-execution-profile", name: "已保存的执行配置", revision: "web-frozen-execution-revision", scope: "environment", environment_id: "env_test",
    proxy_mode: "direct", tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false, created_at: "2026-01-01T00:00:00Z",
  };
  const network: NetworkSnapshot = {
    source: profile, build: { ...profile, id: "web-frozen-build" }, agent: { ...profile, id: "web-frozen-agent" }, verifier: { ...profile, id: "web-frozen-verifier" },
    model: { ...profile, id: "web-frozen-model", name: "已保存的模型配置", revision: "web-frozen-model-revision", scope: "desktop", environment_id: undefined, tls_mode: "skip", tls_risk_accepted: true, has_credentials: true, credential_revision: "web-frozen-credential-revision" },
  };
  Object.assign(network.model, { proxy_username: "NEVER_WEB_PROXY_USERNAME", proxy_password: "NEVER_WEB_PROXY_PASSWORD" });
  run.snapshot.format_version = 3; run.snapshot.network = network;
  run.snapshot.provider = { ...run.snapshot.provider, network_profile_id: "NEVER_WEB_LIVE_PROFILE_REFERENCE" };
  run.snapshot.environment = { ...run.snapshot.environment, skip_tls_verification: true, ca_cert_path: "/NEVER_WEB_LEGACY_CA.pem" };
  const before = JSON.stringify(run);
  const originalFetch = globalThis.fetch;
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const calls: { url: string; method: string; authorization: boolean; credentials: RequestCredentials | undefined; hasBody: boolean }[] = [];
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  globalThis.fetch = Object.assign(async (url: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    calls.push({ url: String(url), method: init?.method || "", authorization: new Headers(init?.headers).has("Authorization"), credentials: init?.credentials, hasBody: Boolean(init?.body) });
    if (String(url) !== "/api/runs/run_frozen_web") throw new Error("Unexpected offline request");
    return new Response(JSON.stringify(run), { status: 200 });
  }, originalFetch);
  try {
    const client = resultsClient(new APIClient(""));
    const received = await client.run(run.id);
    expect(received.snapshot.network).toEqual(network); expect(Object.keys(client).sort()).toEqual(["events", "requests", "run", "runs", "status"]);
    Object.defineProperty(globalThis, "window", { configurable: true, value: { location: { search: "?run=run_frozen_web" }, matchMedia: () => ({ matches: false }) } });
    queryClient.setQueryData(["status"], { running: true, version: "offline-test", pid: 1 });
    queryClient.setQueryData(["runs", 20, 0], { items: [], total: 40 }); queryClient.setQueryData(["attach-run"], run.id); queryClient.setQueryData(["run", run.id], received);
    queryClient.setQueryData(["requests", run.id, 50, 0], { items: [], total: 0 }); queryClient.setQueryData(["events", run.id], []);
    const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><App /></QueryClientProvider>);
    expect(html).toContain("只读 Web"); expect(html).toContain("web-frozen-model-revision"); expect(html).toContain("web-frozen-execution-revision"); expect(html).toContain("web-frozen-credential-revision"); expect(html).toContain("模型侧 TLS 跳过配置（不安全）"); expect(html).not.toContain("执行侧 TLS 跳过配置（不安全）");
    for (const marker of ["NEVER_WEB_PROXY_USERNAME", "NEVER_WEB_PROXY_PASSWORD", "NEVER_WEB_LIVE_PROFILE_REFERENCE", "NEVER_WEB_LEGACY_CA"]) expect(html).not.toContain(marker);
    expect(html).not.toContain('type="password"'); expect(html).not.toContain("保存网络配置"); expect(html).not.toContain("测试网络连接"); expect(html).not.toContain("确认并启动"); expect(html).toContain("不是实际 TLS 连接验证结果");
    expect(calls).toEqual([{ url: "/api/runs/run_frozen_web", method: "GET", authorization: false, credentials: "omit", hasBody: false }]);
    expect(JSON.stringify(run)).toBe(before);
  } finally {
    queryClient.clear(); globalThis.fetch = originalFetch;
    if (originalWindow) Object.defineProperty(globalThis, "window", originalWindow);
    else Reflect.deleteProperty(globalThis, "window");
  }
});

test("backend connection errors remain visible and redacted in the simplified Web shell", () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false, retryOnMount: false, refetchOnMount: false } } });
  const status = queryClient.getQueryCache().build(queryClient, { queryKey: ["status"] });
  status.setState({ status: "error", error: new Error("password=NEVER_BACKEND_PASSWORD"), fetchStatus: "idle" });
  queryClient.setQueryData(["runs", 20, 0], { items: [], total: 0 }); queryClient.setQueryData(["attach-run"], null);
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><App /></QueryClientProvider>);
  expect(html).toContain('role="alert"'); expect(html).toContain("无法连接本机后端"); expect(html).toContain("重新读取"); expect(html).toContain("已隐藏"); expect(html).not.toContain("NEVER_BACKEND_PASSWORD");
  queryClient.clear();
});
