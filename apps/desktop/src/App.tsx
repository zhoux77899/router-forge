import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { activeRunStates, type Run } from "@router-forge/api";
import { isMyGo } from "mygo-runtime";
import { ClipboardList, FlaskConical, KeyRound, Monitor, Plus, RefreshCw, Router, Server, ShieldCheck } from "lucide-react";
import { ErrorNotice, LoadingState, ResultsWorkspace, errorMessage, resultsClient } from "@router-forge/ui";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { rememberRunInLocation, runIDFromLocation } from "@router-forge/ui/lib/selection";
import { connectDesktop, type DesktopConnection } from "./lib/native-client";
import { EnvironmentsPanel } from "./components/environments";
import { ServicesPanel } from "./components/services";
import { RunComposer } from "./components/run-composer";
import { RunControls } from "./components/run-controls";

type View = "results" | "compose" | "environments" | "services";

function ConsoleWorkspace({ desktop, reconnect }: { desktop: DesktopConnection; reconnect: () => void }) {
  const { api, connection } = desktop;
  const [view, setView] = useState<View>("results");
  const [environmentID, setEnvironmentID] = useState("");
  const [runID, setRunID] = useState<string | null>(runIDFromLocation);
  const [reader] = useState(() => resultsClient(api));
  const status = useQuery({ queryKey: ["status"], queryFn: () => api.status(), refetchInterval: 10000 });
  const recentRuns = useQuery({ queryKey: ["runs", 10, 0], queryFn: () => api.runs(10, 0), refetchInterval: 5000 });
  const running = recentRuns.data?.items?.some(run => activeRunStates.has(run.state));
  const nav = [
    ["results", "评测结果", ClipboardList], ["compose", "新建评测", FlaskConical], ["environments", "执行环境", Server], ["services", "模型服务", KeyRound],
  ] as const;
  function attachedDraft(draft: Run) { setRunID(draft.id); rememberRunInLocation(draft.id); setView("results"); }
  return <div className="console-layout">
    <aside className="console-sidebar"><div className="flex items-center gap-3"><div className="rounded-md bg-primary p-2 text-primary-foreground"><Router className="size-5" aria-hidden="true" /></div><div><h1 className="text-lg leading-tight">Router Forge</h1><p className="mt-1 text-xs text-muted-foreground">本机评测控制台</p></div></div>
      <nav aria-label="桌面控制台导航" className="space-y-1">{nav.map(([id, label, Icon]) => <Button key={id} type="button" variant="ghost" onClick={() => setView(id)} aria-current={view === id ? "page" : undefined} className={`w-full justify-start ${view === id ? "bg-card text-primary shadow-xs" : "text-secondary-foreground"}`}><Icon className="size-4" aria-hidden="true" />{label}</Button>)}</nav>
      <div className="sidebar-note mt-auto space-y-4 border-t pt-4"><div className="flex items-center gap-2 text-xs"><span className={`h-1.5 w-1.5 rounded-full ${status.data?.running ? "bg-primary" : "bg-destructive"}`} aria-hidden="true" />{status.isError ? "后端连接异常" : "独立本机后端"}</div><p className="text-xs text-muted-foreground">{running ? "后台有执行中任务；离开页面不终止执行。" : "结果与配置保存在本机；不会自动启动评测。"}</p><div className="flex items-start gap-2 text-xs text-muted-foreground"><ShieldCheck className="mt-0.5 size-4 shrink-0" aria-hidden="true" /><p>固定模型 · 单任务<br />并发 1 · 尝试 1 · 重试 0</p></div></div>
    </aside>
    <main className="console-main" data-native-backend="true" data-backend-pid={connection.pid} data-backend-url={connection.url} data-api-ready={status.data?.running ? "true" : "false"}><div className="console-content space-y-6">
      <header className="panel-heading border-b pb-5"><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><Badge variant="outline" className="gap-1.5 bg-card"><Monitor className="size-3" aria-hidden="true" />MyGo 原生桌面</Badge><span role="status" className="text-xs text-muted-foreground">{status.isError ? "后端不可达，请重新连接" : `Backend ${status.data?.version || connection.version} · PID ${status.data?.pid || connection.pid}`}</span></div><a href={connection.url} target="_blank" rel="noopener noreferrer" className="mt-2 block w-fit break-all text-xs text-primary underline underline-offset-4">{connection.url} · 打开只读结果 Web</a></div><div className="flex shrink-0 gap-2"><Button type="button" size="sm" variant="outline" onClick={reconnect}><RefreshCw className="size-3.5" aria-hidden="true" />重新附着</Button>{view === "results" && <Button type="button" size="sm" onClick={() => setView("compose")}><Plus className="size-4" aria-hidden="true" />创建草稿</Button>}</div></header>
      {status.isError && <ErrorNotice error={status.error} retry={reconnect} title="本机后端暂时不可达" />}
      {view === "results" && <ResultsWorkspace client={reader} selectedRunID={runID} onSelectRun={setRunID} runActions={run => <RunControls key={run.id} api={api} run={run} />} />}
      {view === "compose" && <RunComposer api={api} environmentID={environmentID} onEnvironment={setEnvironmentID} onDraft={attachedDraft} />}
      {view === "environments" && <EnvironmentsPanel api={api} selectedID={environmentID} onSelect={setEnvironmentID} />}
      {view === "services" && <ServicesPanel api={api} />}
      <footer className="border-t pt-4 text-xs text-muted-foreground">数据目录：{connection.data_dir || "由本机后端管理"}<span className="mt-1 block">Key 仅写入、明文 SQLite、权限保护而非加密。关闭桌面不取消任务；取消必须在运行详情中明确执行。</span></footer>
    </div></main>
  </div>;
}

export function App() {
  const queryClient = useQueryClient();
  const [native] = useState(isMyGo);
  const [desktop, setDesktop] = useState<DesktopConnection | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [connecting, setConnecting] = useState(native);
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
  if (!native) return <main className="mx-auto max-w-2xl px-6 py-20"><Card><CardHeader><CardTitle>需要 MyGo 原生桌面桥接</CardTitle></CardHeader><CardContent className="space-y-4"><p>当前页面在普通浏览器中打开，无法连接原生控制服务。这里不会回退到带控制凭据的浏览器请求，也不能创建、启动或取消评测。</p><p className="text-sm text-muted-foreground">请运行仓库的 <code>bun run dev:desktop</code> 或打开已打包的 Router Forge。浏览器仅使用本机后端提供的只读结果页面。</p></CardContent></Card></main>;
  if (connecting || !desktop) return <main className="mx-auto max-w-2xl px-6 py-20"><Card><CardHeader><CardTitle>附着到本机评测后端</CardTitle></CardHeader><CardContent className="space-y-4">{connecting && <LoadingState label="启动或重新附着独立后端…" />}{error && <ErrorNotice error={error} title="无法附着后端" />}{!connecting && <Button onClick={() => void reconnect()}>重新连接</Button>}<p className="text-sm text-muted-foreground">仅连接本机后台服务，不创建评测任务，不调用模型。缺少 helper 二进制时请先运行根 build:backend 与 build:web。</p></CardContent></Card></main>;
  return <ConsoleWorkspace key={`${desktop.connection.url}-${desktop.connection.pid}`} desktop={desktop} reconnect={() => void reconnect()} />;
}
