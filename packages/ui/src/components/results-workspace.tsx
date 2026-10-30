import { useEffect, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { activeRunStates, type JobEvent, type Run } from "@router-forge/api";
import { ChevronLeft, ChevronRight, RefreshCw } from "lucide-react";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./ui/tabs";
import { ErrorNotice, EmptyState, LoadingState } from "./feedback";
import { RunDetailHeader, RunOverview, RunState } from "./run-summary";
import { RunSnapshotDetails } from "./frozen-config";
import { EventTimeline, RunDiagnostics } from "./run-events";
import { RequestTable } from "./request-table";
import { formatTime } from "../lib/format";
import { findAttachRun, rememberRunInLocation, runIDFromLocation } from "../lib/selection";
import type { ResultsClient } from "../lib/read-client";
import { cn } from "../lib/utils";

const RUN_PAGE_SIZE = 20;
const REQUEST_PAGE_SIZE = 50;

function RequestsPanel({ client, run }: { client: ResultsClient; run: Run }) {
  const [offset, setOffset] = useState(0);
  const requests = useQuery({
    queryKey: ["requests", run.id, REQUEST_PAGE_SIZE, offset],
    queryFn: () => client.requests(run.id, REQUEST_PAGE_SIZE, offset),
    refetchInterval: activeRunStates.has(run.state) ? 2000 : false,
  });
  return <section className="space-y-4">
    <div className="panel-heading"><div><h3 className="font-semibold">逐请求指标</h3><p className="text-xs text-muted-foreground">NULL 显示为未知；缓存、推理不会补算为零。耗时与首字节独立记录。</p></div><Button size="sm" variant="outline" onClick={() => void requests.refetch()} disabled={requests.isFetching}><RefreshCw className="size-3.5" aria-hidden="true" />刷新账本</Button></div>
    {requests.isError && <ErrorNotice error={requests.error} retry={() => void requests.refetch()} />}
    {requests.isPending ? <LoadingState /> : requests.data?.items?.length ? <div className="overflow-hidden rounded-md border"><RequestTable requests={requests.data.items} /></div> : !requests.isError && <EmptyState title="尚无网关请求">创建草稿或检查环境不会调用模型。只有明确启动后的真实请求会进入账本。</EmptyState>}
    {requests.data && requests.data.total > 0 && <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground"><span>第 {offset + 1}–{Math.min(offset + REQUEST_PAGE_SIZE, requests.data.total)} 条 / 共 {requests.data.total} 条</span><div className="flex gap-2"><Button size="sm" variant="outline" disabled={offset === 0 || requests.isFetching} onClick={() => setOffset(Math.max(0, offset - REQUEST_PAGE_SIZE))}>上一页</Button><Button size="sm" variant="outline" disabled={offset + REQUEST_PAGE_SIZE >= requests.data.total || requests.isFetching} onClick={() => setOffset(offset + REQUEST_PAGE_SIZE)}>下一页</Button></div></div>}
  </section>;
}

function EventsPanel({ client, run }: { client: ResultsClient; run: Run }) {
  const queryClient = useQueryClient();
  const events = useQuery({
    queryKey: ["events", run.id],
    queryFn: async () => {
      const cached = queryClient.getQueryData<JobEvent[]>(["events", run.id]) ?? [];
      const after = cached.at(-1)?.id ?? 0;
      const incoming = await client.events(run.id, after);
      return [...new Map([...cached, ...(incoming ?? [])].map(event => [event.id, event])).values()].sort((a, b) => a.id - b.id).slice(-500);
    },
    refetchInterval: activeRunStates.has(run.state) ? 1500 : false,
  });
  return <div className="space-y-6"><RunDiagnostics run={run} /><section>
    <div className="panel-heading mb-2"><div><h3 className="font-semibold">诊断时间线</h3><p className="text-xs text-muted-foreground">按事件 ID 增量读取，显示最近 500 条。</p></div><Button size="sm" variant="outline" onClick={() => void events.refetch()} disabled={events.isFetching}>刷新事件</Button></div>
    {events.isError && <ErrorNotice error={events.error} retry={() => void events.refetch()} />}
    {events.isPending ? <LoadingState /> : <EventTimeline events={events.data ?? []} />}
  </section></div>;
}

export type ResultsWorkspaceProps = {
  client: ResultsClient;
  selectedRunID?: string | null;
  onSelectRun?: (id: string) => void;
  runActions?: (run: Run) => ReactNode;
};

export function ResultsWorkspace({ client, selectedRunID, onSelectRun, runActions }: ResultsWorkspaceProps) {
  const [offset, setOffset] = useState(0);
  const [localSelection, setLocalSelection] = useState<string | null>(runIDFromLocation);
  const queryClient = useQueryClient();
  const runs = useQuery({
    queryKey: ["runs", RUN_PAGE_SIZE, offset],
    queryFn: () => client.runs(RUN_PAGE_SIZE, offset),
    refetchInterval: query => query.state.data?.items?.some(run => activeRunStates.has(run.state)) ? 2500 : 10000,
  });
  const attachment = useQuery({
    queryKey: ["attach-run"], queryFn: () => findAttachRun(client),
    enabled: !selectedRunID && !localSelection, staleTime: Infinity,
  });
  const selectedID = selectedRunID ?? localSelection ?? attachment.data ?? null;
  useEffect(() => {
    if (!selectedID && attachment.data === null && (runs.data?.total ?? 0) > 0) void attachment.refetch();
  }, [selectedID, attachment.data, attachment.refetch, runs.data?.total]);
  useEffect(() => {
    if (selectedID && !selectedRunID && !localSelection) { setLocalSelection(selectedID); onSelectRun?.(selectedID); }
  }, [selectedID, selectedRunID, localSelection, onSelectRun]);
  const run = useQuery({
    queryKey: ["run", selectedID],
    queryFn: () => client.run(selectedID ?? ""),
    enabled: Boolean(selectedID),
    refetchInterval: query => query.state.data && activeRunStates.has(query.state.data.state) ? 1500 : false,
  });
  function select(id: string) {
    setLocalSelection(id);
    onSelectRun?.(id);
    rememberRunInLocation(id);
  }
  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ["runs"] });
    if (!selectedID) void attachment.refetch();
    if (selectedID) {
      void queryClient.invalidateQueries({ queryKey: ["run", selectedID] });
      void queryClient.invalidateQueries({ queryKey: ["events", selectedID] });
      void queryClient.invalidateQueries({ queryKey: ["requests", selectedID] });
    }
  }
  return <div className="results-layout">
    <Card className="result-list gap-0 py-0">
      <CardHeader className="border-b px-4 py-4"><div className="flex items-center justify-between gap-3"><CardTitle className="text-base">评测运行</CardTitle><Button size="icon" variant="ghost" aria-label="刷新运行列表" onClick={refresh} disabled={runs.isFetching}><RefreshCw className={cn("size-4", runs.isFetching && "animate-spin motion-reduce:animate-none")} aria-hidden="true" /></Button></div><p className="text-xs text-muted-foreground">{runs.data ? `已持久化 ${runs.data.total} 个运行` : "读取本机持久化记录"}</p></CardHeader>
      <CardContent className="space-y-3 p-3">
        {runs.isError && <ErrorNotice error={runs.error} retry={() => void runs.refetch()} />}
        {runs.isPending ? <LoadingState /> : (runs.data?.items ?? []).length === 0 ? !runs.isError && <EmptyState title="还没有评测运行">请在桌面控制台配置环境与模型，然后创建单任务草稿。</EmptyState> : <div className="run-options" role="list" aria-label="持久化评测运行">
          {runs.data?.items.map(item => <div role="listitem" key={item.id}><button type="button" onClick={() => select(item.id)} aria-current={selectedID === item.id ? "true" : undefined} className={cn("w-full rounded-md border border-transparent px-3 py-3 text-left transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring", selectedID === item.id && "border-primary/30 bg-accent/60")}>
            <span className="block truncate font-medium" title={item.snapshot.spec.task_names.join(", ")}>{item.snapshot.spec.task_names.join(", ") || item.id}</span>
            <span className="mt-1 block truncate text-xs text-muted-foreground">{item.snapshot.model.display_name || item.snapshot.model.name} · {formatTime(item.created_at)}</span>
            <span className="mt-2 block"><RunState state={item.state} /></span>
          </button></div>)}
        </div>}
        {runs.data && runs.data.total > RUN_PAGE_SIZE && <div className="flex items-center justify-between border-t pt-3 text-xs text-muted-foreground"><span>{Math.floor(offset / RUN_PAGE_SIZE) + 1} / {Math.ceil(runs.data.total / RUN_PAGE_SIZE)}</span><div className="flex gap-1"><Button size="icon" variant="ghost" aria-label="上一页运行" disabled={offset === 0 || runs.isFetching} onClick={() => setOffset(Math.max(0, offset - RUN_PAGE_SIZE))}><ChevronLeft className="size-4" aria-hidden="true" /></Button><Button size="icon" variant="ghost" aria-label="下一页运行" disabled={offset + RUN_PAGE_SIZE >= runs.data.total || runs.isFetching} onClick={() => setOffset(offset + RUN_PAGE_SIZE)}><ChevronRight className="size-4" aria-hidden="true" /></Button></div></div>}
      </CardContent>
    </Card>
    <div className="result-detail space-y-4">
      {!selectedID ? <Card>{attachment.isPending && !runs.isError ? <LoadingState label="正在查找可附着的后台运行…" /> : attachment.isError ? <div className="p-5"><ErrorNotice error={attachment.error} retry={() => void attachment.refetch()} /></div> : <EmptyState title="结果会保留在本机">尚无可展示记录。网页只读，配置、启动与取消均由桌面控制台完成。</EmptyState>}</Card> : <>
        {run.isError && <ErrorNotice error={run.error} retry={() => void run.refetch()} title="无法读取此运行" />}
        {run.isPending ? <Card><LoadingState label="正在接入运行记录…" /></Card> : run.data && <>
          {runActions?.(run.data)}
          <Card className="gap-0 py-0"><RunDetailHeader run={run.data} /><CardContent className="p-0"><Tabs key={run.data.id} defaultValue="overview" className="gap-0">
            <div className="overflow-x-auto border-b px-5 py-3"><TabsList><TabsTrigger value="overview">结果概览</TabsTrigger><TabsTrigger value="configuration">运行快照</TabsTrigger><TabsTrigger value="requests">请求与成本</TabsTrigger><TabsTrigger value="events">诊断事件</TabsTrigger></TabsList></div>
            <TabsContent value="overview" className="p-5"><RunOverview run={run.data} /></TabsContent>
            <TabsContent value="configuration" className="p-5"><RunSnapshotDetails run={run.data} /></TabsContent>
            <TabsContent value="requests" className="p-5"><RequestsPanel key={run.data.id} client={client} run={run.data} /></TabsContent>
            <TabsContent value="events" className="p-5"><EventsPanel key={run.data.id} client={client} run={run.data} /></TabsContent>
          </Tabs></CardContent></Card>
          <p role="status" className="px-1 text-xs text-muted-foreground">{activeRunStates.has(run.data.state) ? "此运行由独立后端继续执行，当前按运行 ID 自动轮询；关闭窗口不会终止任务。" : "已附着到持久化结果。刷新或重新打开不会启动评测。"}</p>
        </>}
      </>}
    </div>
  </div>;
}
