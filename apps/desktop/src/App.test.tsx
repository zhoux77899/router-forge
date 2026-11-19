import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient } from "@router-forge/api";
import { TooltipProvider } from "@router-forge/ui/components/ui/tooltip";
import { App, ConsoleWorkspace } from "./App";
import type { DesktopConnection } from "./lib/native-client";

test("ordinary browsers retain the MyGo-only control boundary and never render a control workspace", () => {
  const queryClient = new QueryClient();
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><TooltipProvider><App /></TooltipProvider></QueryClientProvider>);
  expect(html).toContain("需要 MyGo 原生桌面桥接");
  expect(html).toContain("普通浏览器不能创建、启动或取消评测");
  expect(html).toContain("bun run dev:desktop");
  expect(html).toContain('data-slot="empty"');
  expect(html).not.toContain('data-native-backend="true"');
  expect(html).not.toContain('data-slot="sidebar"');
  expect(html).not.toContain('id="run-form"');
  queryClient.clear();
});

function workspaceMarkup(sidebarOpen = true) {
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  let calls = 0;
  const desktop: DesktopConnection = {
    api: new APIClient("", "", async () => { calls++; throw new Error("Rendering must not control the backend"); }),
    connection: { url: "http://127.0.0.1:42731", pid: 246, version: "offline-native", data_dir: "D:\\RouterForgeTest\\data", worker_dir: "D:\\RouterForgeTest\\worker" },
  };
  const queryClient = new QueryClient();
  queryClient.setQueryData(["status"], { running: true, version: "offline-native", pid: 246 });
  queryClient.setQueryData(["runs", 10, 0], { items: [], total: 0, limit: 10, offset: 0 });
  try {
    Object.defineProperty(globalThis, "window", { configurable: true, value: { location: { search: "" }, localStorage: { getItem: (key: string) => key === "router-forge-sidebar-open" ? String(sidebarOpen) : null } } });
    const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><TooltipProvider><ConsoleWorkspace desktop={desktop} reconnect={() => { calls++; }} theme="light" onTheme={() => {}} fonts={{ families: [], preferences: { interface: "system", code: "system" }, invalid: {}, loading: false, error: null, reload: () => {}, onSelect: () => {} }} /></TooltipProvider></QueryClientProvider>);
    expect(calls).toBe(0);
    return html;
  } finally {
    queryClient.clear();
    if (originalWindow) Object.defineProperty(globalThis, "window", originalWindow);
    else Reflect.deleteProperty(globalThis, "window");
  }
}

test("Desktop keeps only evaluation/results navigation and footer settings below native App chrome", () => {
  const html = workspaceMarkup();
  expect(html).toContain('data-slot="sidebar"');
  expect(html).toContain('aria-label="桌面控制台导航"');
  expect(html.match(/aria-label="切换导航栏"/g)).toHaveLength(1);
  expect(html).toContain('data-slot="page-header"');
  expect(html).toContain('data-slot="scroll-area-viewport"');
  expect(html).toContain('aria-current="page"');
  for (const label of ["结果", "评测", "设置"]) expect(html).toContain(label);
  const navigation = html.match(/<nav\b[^>]*aria-label="桌面控制台导航"[^>]*>([\s\S]*?)<\/nav>/)?.[1];
  expect(navigation).not.toMatch(/执行环境|模型|网络|设置|Router Forge/);
  expect(html).not.toContain("后端诊断");
  expect(html).not.toContain("打开只读结果 Web");
  expect(html).toContain('data-slot="desktop-titlebar"');
  const header = html.match(/<header\b[^>]*>([\s\S]*?)<\/header>/)?.[1];
  expect(header).toBeDefined();
  expect(header).not.toMatch(/<h1|深色模式|任务执行中|127\.0\.0\.1|PID|RouterForgeTest/);
  expect(header).toContain('class="desktop-titlebar-controls"'); expect(header).toContain('aria-label="Router Forge"');
  expect(header!.indexOf('aria-label="Router Forge"')).toBeLessThan(header!.indexOf('data-slot="sidebar-trigger"'));
  expect(header!.replace(/<[^>]+>/g, "")).not.toContain("Router Forge");
});

test("Desktop restores a fully hidden sidebar with no icon rail or titlebar sidebar width", () => {
  for (const open of [false, true]) {
    const html = workspaceMarkup(open);
    const header = html.match(/<header\b[^>]*data-slot="desktop-titlebar"[^>]*>/)?.[0] ?? "";
    expect(header).toContain(`data-sidebar-state="${open ? "expanded" : "collapsed"}"`);
    expect(header).toContain(`--desktop-sidebar-width:${open ? "var(--sidebar-width)" : "0px"}`);
    expect(header).not.toContain("var(--sidebar-width-icon)");
    expect(html).toContain(`data-collapsible="${open ? "" : "offcanvas"}"`);
    expect(html).not.toContain('data-collapsible="icon"');
    expect(html).not.toContain('data-mobile-rail="true"');
    expect(html).toContain("group-data-[collapsible=offcanvas]:w-0");
    expect(html.match(/aria-label="切换导航栏"/g)).toHaveLength(1);
    expect(html).toContain(`data-sidebar-action="${open ? "collapse" : "expand"}"`);
  }
});
