import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import { validateProxyOrigin } from "./proxy";

test("Web entry uses same-origin read-only API, no native bridge or control surface", () => {
  const source = readFileSync(join(import.meta.dir, "App.tsx"), "utf8");
  expect(source).toContain('resultsClient(new APIClient(""))');
  expect(source).not.toMatch(/mygo|native-client|saveProvider|saveEnvironment|saveModel|createRun|startRun|cancelRun|runActions|localStorage|sessionStorage/);
  const html = renderToStaticMarkup(<QueryClientProvider client={new QueryClient()}><App /></QueryClientProvider>);
  expect(html).toContain("只读 Web"); expect(html).not.toContain('type="password"'); expect(html).not.toContain("确认并启动这一个任务");
});

test("Vite dev proxy is loopback-only and never takes control credentials", () => {
  expect(validateProxyOrigin(undefined)).toBeUndefined();
  expect(validateProxyOrigin("http://127.0.0.1:1234")).toBe("http://127.0.0.1:1234");
  for (const url of ["https://remote.test", "http://user:pass@127.0.0.1:1234", "http://127.0.0.1:1234?token=test", "http://127.0.0.1:1234/path"]) expect(() => validateProxyOrigin(url)).toThrow();
  const config = readFileSync(join(import.meta.dir, "..", "vite.config.ts"), "utf8");
  expect(config).toContain('host: "127.0.0.1"'); expect(config).toContain("configurePreviewServer: rejectMissingBackend"); expect(config).not.toMatch(/CONTROL_TOKEN|Authorization|VITE_.*KEY/);
});
