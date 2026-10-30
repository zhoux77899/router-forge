import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient } from "@router-forge/api";
import { TooltipProvider } from "@router-forge/ui/components/ui/tooltip";
import { App, applyTheme, ConsoleWorkspace, readThemePreference, ThemeToggle, themePreferenceKey } from "./App";
import type { DesktopConnection } from "./lib/native-client";

function themeRoot() {
  const classes = new Set(["existing-class"]);
  return {
    classes,
    classList: {
      toggle(token: string, force?: boolean) {
        const enabled = force ?? !classes.has(token);
        if (enabled) classes.add(token); else classes.delete(token);
        return enabled;
      },
    },
    style: { colorScheme: "" },
  };
}

test("theme preference reads only its non-sensitive key and otherwise follows the system", () => {
  const keys: string[] = [];
  const storage = { getItem: (key: string) => { keys.push(key); return "dark"; } };
  expect(readThemePreference(storage, false)).toBe("dark");
  expect(keys).toEqual([themePreferenceKey]);
  expect(readThemePreference({ getItem: () => "light" }, true)).toBe("light");
  for (const saved of [null, "system", "unrecognized-value"]) {
    expect(readThemePreference({ getItem: () => saved }, true)).toBe("dark");
    expect(readThemePreference({ getItem: () => saved }, false)).toBe("light");
  }
  expect(readThemePreference(null, true)).toBe("dark");
  expect(readThemePreference({ getItem: () => { throw new Error("Storage blocked"); } }, false)).toBe("light");
});

test("light and dark persistence writes only the theme and remains usable without storage", () => {
  const root = themeRoot();
  const writes: [string, string][] = [];
  const storage = { setItem: (key: string, value: string) => { writes.push([key, value]); } };
  applyTheme("dark", root);
  expect(root.classes.has("dark")).toBe(true);
  expect(root.style.colorScheme).toBe("dark");
  expect(writes).toHaveLength(0);
  applyTheme("light", root, storage);
  applyTheme("dark", root, storage);
  expect(writes).toEqual([[themePreferenceKey, "light"], [themePreferenceKey, "dark"]]);
  expect(root.classes.has("existing-class")).toBe(true);
  expect(() => applyTheme("light", root, { setItem: () => { throw new Error("Storage blocked"); } })).not.toThrow();
  expect(root.classes.has("dark")).toBe(false);
  expect(root.style.colorScheme).toBe("light");
});

test("theme toggle has an accessible name and exposes state without triggering a toggle on render", () => {
  let toggles = 0;
  for (const theme of ["light", "dark"] as const) {
    const html = renderToStaticMarkup(<TooltipProvider><ThemeToggle theme={theme} onToggle={() => { toggles++; }} /></TooltipProvider>);
    expect(html).toContain('aria-label="深色模式"');
    expect(html).toContain(`aria-pressed="${theme === "dark"}"`);
    expect(html).toContain('type="button"');
    expect(html).toMatch(/<svg\b[^>]*aria-hidden="true"/);
  }
  expect(toggles).toBe(0);
});

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

function workspaceMarkup() {
  let calls = 0;
  const desktop: DesktopConnection = {
    api: new APIClient("", "", async () => { calls++; throw new Error("Rendering must not control the backend"); }),
    connection: { url: "http://127.0.0.1:42731", pid: 246, version: "offline-native", data_dir: "D:\\RouterForgeTest\\data", worker_dir: "D:\\RouterForgeTest\\worker" },
  };
  const queryClient = new QueryClient();
  queryClient.setQueryData(["status"], { running: true, version: "offline-native", pid: 246 });
  queryClient.setQueryData(["runs", 10, 0], { items: [], total: 0, limit: 10, offset: 0 });
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><TooltipProvider><ConsoleWorkspace desktop={desktop} reconnect={() => { calls++; }} theme="light" onTheme={() => {}} /></TooltipProvider></QueryClientProvider>);
  expect(calls).toBe(0);
  queryClient.clear();
  return html;
}

test("Sidebar navigation marks the current view and keeps diagnostics out of the minimal header", () => {
  const html = workspaceMarkup();
  expect(html).toContain('data-slot="sidebar"');
  expect(html).toContain('data-slot="sidebar-menu"');
  expect(html).toContain('aria-label="桌面控制台导航"');
  expect(html).toContain('aria-label="切换导航栏"');
  expect(html).toContain('aria-current="page"');
  for (const label of ["结果", "新建评测", "执行环境", "模型", "后端诊断"]) expect(html).toContain(label);
  expect(html).toContain('data-slot="collapsible-content"');
  expect(html).toContain('aria-expanded="false"');
  expect(html).toContain("D:\\RouterForgeTest\\data");
  expect(html).toContain("http://127.0.0.1:42731");
  expect(html).toContain("打开只读结果 Web");
  const header = html.match(/<header\b[^>]*>([\s\S]*?)<\/header>/)?.[1];
  expect(header).toBeDefined();
  expect(header).not.toContain("127.0.0.1");
  expect(header).not.toContain("PID");
  expect(header).not.toContain("RouterForgeTest");
});

test("Network navigation is available without issuing network operations while rendering results", () => {
  expect(workspaceMarkup()).toContain("<span>网络</span>");
});
