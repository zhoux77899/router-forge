import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { type Run } from "@router-forge/api";
import { countRunningRuns } from "./lib/running-count";
import { isMyGo } from "mygo-runtime";
import { HugeiconsIcon, type IconSvgElement } from "@hugeicons/react";
import { Add01Icon, AnalyticsUpIcon, ArrowReloadHorizontalIcon, ComputerIcon, Moon02Icon, Settings01Icon, SidebarLeft01Icon, Sun03Icon } from "@hugeicons/core-free-icons";
import { ErrorNotice, LoadingState, ResultsWorkspace, errorMessage, resultsClient } from "@router-forge/ui";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@router-forge/ui/components/ui/empty";
import { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarInset, SidebarMenu, SidebarMenuBadge, SidebarMenuButton, SidebarMenuItem, SidebarProvider, SidebarTrigger, useSidebar } from "@router-forge/ui/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@router-forge/ui/components/ui/tooltip";
import { rememberRunInLocation, runIDFromLocation } from "@router-forge/ui/lib/selection";
import { connectDesktop, type DesktopConnection } from "./lib/native-client";
import { RunComposer } from "./components/run-composer";
import { RunControls } from "./components/run-controls";
import { DesktopSettings } from "./components/settings";
import { RetainedView } from "@router-forge/ui/components/retained-view";

type View = "results" | "compose" | "settings";
export type Theme = "light" | "dark";
export const themePreferenceKey = "router-forge-theme";

export function readThemePreference(storage: Pick<Storage, "getItem"> | null, systemDark: boolean): Theme {
  try {
    const saved = storage?.getItem(themePreferenceKey);
    if (saved === "light" || saved === "dark") return saved;
  } catch { /* Storage may be unavailable in the native webview. */ }
  return systemDark ? "dark" : "light";
}

export function applyTheme(theme: Theme, root: { classList: Pick<DOMTokenList, "toggle">; style: Pick<CSSStyleDeclaration, "colorScheme"> }, storage?: Pick<Storage, "setItem"> | null) {
  root.classList.toggle("dark", theme === "dark");
  root.style.colorScheme = theme;
  try { storage?.setItem(themePreferenceKey, theme); } catch { /* The in-memory preference still works. */ }
}

function themeStorage(): Storage | null {
  try { return typeof window === "undefined" ? null : window.localStorage; } catch { return null; }
}

function preferredTheme(): Theme {
  const systemDark = typeof window !== "undefined" && Boolean(window.matchMedia?.("(prefers-color-scheme: dark)").matches);
  return readThemePreference(themeStorage(), systemDark);
}

export function initializeTheme(): Theme {
  const theme = preferredTheme();
  if (typeof document !== "undefined") applyTheme(theme, document.documentElement);
  return theme;
}

export function ThemeToggle({ theme, onToggle }: { theme: Theme; onToggle: () => void }) {
  return <Tooltip><TooltipTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label="深色模式" aria-pressed={theme === "dark"} onClick={onToggle}><HugeiconsIcon icon={theme === "dark" ? Sun03Icon : Moon02Icon} data-icon="inline-start" aria-hidden="true" /></Button></TooltipTrigger><TooltipContent>{theme === "dark" ? "切换到浅色模式" : "切换到深色模式"}</TooltipContent></Tooltip>;
}

function WorkspaceSidebar({ view, onView, runningCount }: { view: View; onView: (view: View) => void; runningCount: number }) {
  const { setOpenMobile, toggleSidebar } = useSidebar();
  function navigate(next: View) { onView(next); setOpenMobile(false); }
  const nav: { id: View; label: string; icon: IconSvgElement }[] = [
    { id: "compose", label: "评测", icon: Add01Icon },
    { id: "results", label: "结果", icon: AnalyticsUpIcon },
  ];
  return <Sidebar collapsible="icon" className="desktop-sidebar">
    <SidebarContent><SidebarGroup><SidebarGroupContent><nav aria-label="桌面控制台导航"><SidebarMenu>{nav.map(item => <SidebarMenuItem key={item.id}>
      <SidebarMenuButton type="button" isActive={view === item.id} aria-label={item.label} aria-current={view === item.id ? "page" : undefined} tooltip={item.label} onClick={() => navigate(item.id)}><HugeiconsIcon icon={item.icon} aria-hidden="true" /><span>{item.label}</span></SidebarMenuButton>
      {item.id === "compose" && runningCount > 0 && <SidebarMenuBadge aria-label={runningCount + " 个任务执行中"}>{runningCount}</SidebarMenuBadge>}
    </SidebarMenuItem>)}</SidebarMenu></nav></SidebarGroupContent></SidebarGroup></SidebarContent>
    <SidebarFooter><SidebarMenu>
      <SidebarMenuItem><SidebarMenuButton type="button" aria-label="设置" isActive={view === "settings"} aria-current={view === "settings" ? "page" : undefined} tooltip="设置" onClick={() => navigate("settings")}><HugeiconsIcon icon={Settings01Icon} aria-hidden="true" /><span>设置</span></SidebarMenuButton></SidebarMenuItem>
      <SidebarMenuItem><SidebarMenuButton type="button" aria-label="切换导航栏" tooltip="切换导航栏" onClick={toggleSidebar}><HugeiconsIcon icon={SidebarLeft01Icon} aria-hidden="true" /><span>切换导航栏</span></SidebarMenuButton></SidebarMenuItem>
    </SidebarMenu></SidebarFooter>
  </Sidebar>;
}

export function ConsoleWorkspace({ desktop, reconnect, theme, onTheme }: { desktop: DesktopConnection; reconnect: () => void; theme: Theme; onTheme: () => void }) {
  const { api, connection } = desktop;
  const [view, setView] = useState<View>("results");
  const [environmentID, setEnvironmentID] = useState("");
  const [runID, setRunID] = useState<string | null>(runIDFromLocation);
  const [reader] = useState(() => resultsClient(api));
  const status = useQuery({ queryKey: ["status"], queryFn: () => api.status(), refetchInterval: 10000 });
  const runningRuns = useQuery({ queryKey: ["running-count"], queryFn: () => countRunningRuns(api), refetchInterval: 10000 });
  const runningCount = runningRuns.data ?? 0;
  function attachedDraft(draft: Run) { setRunID(draft.id); rememberRunInLocation(draft.id); setView("results"); }
  return <SidebarProvider className="desktop-workspace">
    <WorkspaceSidebar view={view} onView={setView} runningCount={runningCount} />
    <SidebarInset data-native-backend="true" data-backend-pid={connection.pid} data-backend-url={connection.url} data-api-ready={status.data?.running ? "true" : "false"} className="min-w-0">
      <header data-slot="desktop-titlebar" className="desktop-titlebar"><SidebarTrigger aria-label="切换导航栏" /></header>
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-4 md:p-6">
        <h1 className="font-semibold">{view === "compose" ? "评测" : view === "settings" ? "设置" : "结果"}</h1>
        {status.isError && <ErrorNotice error={status.error} retry={reconnect} title="本机后端暂时不可达" />}
        {view === "results" && <ResultsWorkspace client={reader} selectedRunID={runID} onSelectRun={setRunID} runActions={run => <RunControls key={run.id} api={api} run={run} />} />}
        <RetainedView active={view === "compose"}><RunComposer api={api} environmentID={environmentID} onEnvironment={setEnvironmentID} onDraft={attachedDraft} /></RetainedView>
        <RetainedView active={view === "settings"}><DesktopSettings desktop={desktop} environmentID={environmentID} onEnvironment={setEnvironmentID} theme={theme} onTheme={onTheme} reconnect={reconnect} statusError={status.isError} backendRunning={status.data?.running} version={status.data?.version} pid={status.data?.pid} /></RetainedView>
      </div>
    </SidebarInset>
  </SidebarProvider>;
}

export function App() {
  const queryClient = useQueryClient();
  const [native] = useState(isMyGo);
  const [theme, setTheme] = useState(preferredTheme);
  const [desktop, setDesktop] = useState<DesktopConnection | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [connecting, setConnecting] = useState(native);
  useEffect(() => { applyTheme(theme, document.documentElement); }, [theme]);
  useEffect(() => {
    if (!native) return;
    let alive = true;
    void connectDesktop().then(connection => { if (alive) setDesktop(connection); }).catch(error => { if (alive) setError(new Error(errorMessage(error))); }).finally(() => { if (alive) setConnecting(false); });
    return () => { alive = false; };
  }, [native]);
  async function reconnect() {
    setConnecting(true); setError(null);
    try { const connected = await connectDesktop(); queryClient.clear(); setDesktop(connected); }
    catch (error) { setError(new Error(errorMessage(error))); } finally { setConnecting(false); }
  }
  function toggleTheme() {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    applyTheme(next, document.documentElement, themeStorage());
  }
  if (!native) return <main className="mx-auto max-w-2xl px-6 py-20"><Empty><EmptyHeader><EmptyMedia variant="icon"><HugeiconsIcon icon={ComputerIcon} aria-hidden="true" /></EmptyMedia><EmptyTitle>需要 MyGo 原生桌面桥接</EmptyTitle><EmptyDescription>普通浏览器不能创建、启动或取消评测，也不会回退到带控制凭据的请求。浏览器仅使用本机后端的只读结果页面。</EmptyDescription></EmptyHeader><EmptyContent><code>bun run dev:desktop</code></EmptyContent></Empty></main>;
  if (connecting || !desktop || error) return <><header data-slot="desktop-titlebar" className="desktop-titlebar" /><main className="mx-auto max-w-2xl px-6 py-20"><Card><CardHeader><CardTitle>附着到本机后端</CardTitle><CardDescription>仅连接后台服务，不创建任务或调用模型。</CardDescription></CardHeader><CardContent>{connecting && <LoadingState label="正在附着…" />}{error && <ErrorNotice error={error} title="无法附着后端" />}</CardContent>{!connecting && <CardFooter><Button onClick={() => void reconnect()}><HugeiconsIcon icon={ArrowReloadHorizontalIcon} data-icon="inline-start" aria-hidden="true" />  重新连接</Button></CardFooter>}</Card></main></>;
  return <ConsoleWorkspace key={`${desktop.connection.url}-${desktop.connection.pid}`} desktop={desktop} reconnect={() => void reconnect()} theme={theme} onTheme={toggleTheme} />;
}
