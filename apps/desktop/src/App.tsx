import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { activeRunStates, type Run } from "@router-forge/api";
import { isMyGo } from "mygo-runtime";
import { HugeiconsIcon, type IconSvgElement } from "@hugeicons/react";
import { Add01Icon, AnalyticsUpIcon, ArrowDown01Icon, ArrowReloadHorizontalIcon, ComputerIcon, DashboardSquare01Icon, Globe02Icon, Key01Icon, Moon02Icon, ServerStack01Icon, Settings01Icon, Sun03Icon } from "@hugeicons/core-free-icons";
import { ErrorNotice, LoadingState, ResultsWorkspace, errorMessage, resultsClient } from "@router-forge/ui";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@router-forge/ui/components/ui/empty";
import { Separator } from "@router-forge/ui/components/ui/separator";
import { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarHeader, SidebarInset, SidebarMenu, SidebarMenuButton, SidebarMenuItem, SidebarProvider, SidebarTrigger, useSidebar } from "@router-forge/ui/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@router-forge/ui/components/ui/tooltip";
import { rememberRunInLocation, runIDFromLocation } from "@router-forge/ui/lib/selection";
import { connectDesktop, type DesktopConnection } from "./lib/native-client";
import { EnvironmentsPanel } from "./components/environments";
import { ServicesPanel } from "./components/services";
import { RunComposer } from "./components/run-composer";
import { RunControls } from "./components/run-controls";
import { NetworkManager } from "./components/networks";

type View = "results" | "compose" | "environments" | "services" | "network";
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

function WorkspaceSidebar({ view, onView, nav, desktop, reconnect, statusError, backendRunning, version, pid }: { view: View; onView: (view: View) => void; nav: readonly { id: View; label: string; icon: IconSvgElement }[]; desktop: DesktopConnection; reconnect: () => void; statusError: boolean; backendRunning?: boolean; version?: string; pid?: number }) {
  const { setOpenMobile } = useSidebar();
  const { connection } = desktop;
  function navigate(next: View) { onView(next); setOpenMobile(false); }
  return <Sidebar collapsible="icon">
    <SidebarHeader><SidebarMenu><SidebarMenuItem><SidebarMenuButton size="lg" onClick={() => navigate("results")} tooltip="Router Forge"><HugeiconsIcon icon={DashboardSquare01Icon} aria-hidden="true" /><span className="truncate">Router Forge</span></SidebarMenuButton></SidebarMenuItem></SidebarMenu></SidebarHeader>
    <SidebarContent><SidebarGroup><SidebarGroupContent><nav aria-label="桌面控制台导航"><SidebarMenu>{nav.map(item => <SidebarMenuItem key={item.id}><SidebarMenuButton type="button" isActive={view === item.id} aria-current={view === item.id ? "page" : undefined} tooltip={item.label} onClick={() => navigate(item.id)}><HugeiconsIcon icon={item.icon} aria-hidden="true" /><span>{item.label}</span></SidebarMenuButton></SidebarMenuItem>)}</SidebarMenu></nav></SidebarGroupContent></SidebarGroup></SidebarContent>
    <SidebarFooter><Collapsible className="group/diagnostics"><SidebarMenu><SidebarMenuItem><CollapsibleTrigger asChild><SidebarMenuButton type="button" tooltip="后端诊断"><HugeiconsIcon icon={Settings01Icon} aria-hidden="true" /><span>后端诊断</span><HugeiconsIcon icon={ArrowDown01Icon} aria-hidden="true" className="ml-auto transition-transform group-data-[state=open]/diagnostics:rotate-180" /></SidebarMenuButton></CollapsibleTrigger></SidebarMenuItem></SidebarMenu>
      <CollapsibleContent forceMount className="flex flex-col gap-4 px-2 pb-2 data-[state=closed]:hidden group-data-[collapsible=icon]:hidden">
        <Badge variant={statusError || backendRunning === false ? "destructive" : "outline"}>{statusError ? "后端不可达" : backendRunning == null ? "读取状态…" : backendRunning ? "MyGo 已附着" : "后端未运行"}</Badge>
        <dl className="flex flex-col gap-3 text-xs"><div><dt className="text-muted-foreground">版本 / PID</dt><dd>{version || connection.version} / {pid || connection.pid}</dd></div><div><dt className="text-muted-foreground">后端 URL</dt><dd className="break-all">{connection.url}</dd></div><div><dt className="text-muted-foreground">数据目录</dt><dd className="break-all">{connection.data_dir || "由本机后端管理"}</dd></div></dl>
        <Button type="button" size="sm" variant="outline" onClick={reconnect}><HugeiconsIcon icon={ArrowReloadHorizontalIcon} data-icon="inline-start" aria-hidden="true" />重新附着</Button>
        <Button size="sm" variant="outline" asChild><a href={connection.url} target="_blank" rel="noopener noreferrer">打开只读结果 Web</a></Button>
      </CollapsibleContent>
    </Collapsible></SidebarFooter>
  </Sidebar>;
}

export function ConsoleWorkspace({ desktop, reconnect, theme, onTheme }: { desktop: DesktopConnection; reconnect: () => void; theme: Theme; onTheme: () => void }) {
  const { api, connection } = desktop;
  const [view, setView] = useState<View>("results");
  const [environmentID, setEnvironmentID] = useState("");
  const [runID, setRunID] = useState<string | null>(runIDFromLocation);
  const [reader] = useState(() => resultsClient(api));
  const status = useQuery({ queryKey: ["status"], queryFn: () => api.status(), refetchInterval: 10000 });
  const recentRuns = useQuery({ queryKey: ["runs", 10, 0], queryFn: () => api.runs(10, 0), refetchInterval: 5000 });
  const running = recentRuns.data?.items?.some(run => activeRunStates.has(run.state));
  const nav: { id: View; label: string; icon: IconSvgElement }[] = [
    { id: "results", label: "结果", icon: AnalyticsUpIcon },
    { id: "compose", label: "新建评测", icon: Add01Icon },
    { id: "environments", label: "执行环境", icon: ServerStack01Icon },
    { id: "services", label: "模型", icon: Key01Icon },
    { id: "network", label: "网络", icon: Globe02Icon },
  ];
  function attachedDraft(draft: Run) { setRunID(draft.id); rememberRunInLocation(draft.id); setView("results"); }
  return <SidebarProvider>
    <WorkspaceSidebar view={view} onView={setView} nav={nav} desktop={desktop} reconnect={reconnect} statusError={status.isError} backendRunning={status.data?.running} version={status.data?.version} pid={status.data?.pid} />
    <SidebarInset data-native-backend="true" data-backend-pid={connection.pid} data-backend-url={connection.url} data-api-ready={status.data?.running ? "true" : "false"}>
      <header className="flex h-16 items-center gap-3 border-b px-4 md:px-6"><SidebarTrigger aria-label="切换导航栏" /><Separator orientation="vertical" className="h-4" /><h1 className="font-semibold">{nav.find(item => item.id === view)?.label}</h1><div className="ml-auto flex items-center gap-3">{running && <Badge variant="outline">任务执行中</Badge>}<ThemeToggle theme={theme} onToggle={onTheme} /></div></header>
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-4 md:p-6">
        {status.isError && <ErrorNotice error={status.error} retry={reconnect} title="本机后端暂时不可达" />}
        {view === "results" && <ResultsWorkspace client={reader} selectedRunID={runID} onSelectRun={setRunID} runActions={run => <RunControls key={run.id} api={api} run={run} />} />}
        {view === "compose" && <RunComposer api={api} environmentID={environmentID} onEnvironment={setEnvironmentID} onDraft={attachedDraft} />}
        {view === "environments" && <EnvironmentsPanel api={api} selectedID={environmentID} onSelect={setEnvironmentID} />}
        {view === "services" && <ServicesPanel api={api} />}
        {view === "network" && <NetworkManager api={api} />}
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
  if (connecting || !desktop || error) return <main className="mx-auto max-w-2xl px-6 py-20"><Card><CardHeader><CardTitle>附着到本机后端</CardTitle><CardDescription>仅连接后台服务，不创建任务或调用模型。</CardDescription></CardHeader><CardContent>{connecting && <LoadingState label="正在附着…" />}{error && <ErrorNotice error={error} title="无法附着后端" />}</CardContent>{!connecting && <CardFooter><Button onClick={() => void reconnect()}><HugeiconsIcon icon={ArrowReloadHorizontalIcon} data-icon="inline-start" aria-hidden="true" />重新连接</Button></CardFooter>}</Card></main>;
  return <ConsoleWorkspace key={`${desktop.connection.url}-${desktop.connection.pid}`} desktop={desktop} reconnect={() => void reconnect()} theme={theme} onTheme={toggleTheme} />;
}
