import { useEffect, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { activeRunStates, type JobEvent, type Run } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { ArrowLeft01Icon, ArrowRight01Icon, RefreshIcon } from "@hugeicons/core-free-icons";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardAction, CardContent, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@router-forge/ui/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@router-forge/ui/components/ui/tooltip";
import { ErrorNotice, EmptyState, LoadingState } from "./feedback";
import { RunDetailHeader, RunOverview, RunState } from "./run-summary";
import { RunSnapshotDetails } from "./frozen-config";
import { EventTimeline, RunDiagnostics } from "./run-events";
import { RequestTable } from "./request-table";
import { formatTime, safeMessage } from "../lib/format";
import { findAttachRun, rememberRunInLocation, runIDFromLocation } from "../lib/selection";
import type { ResultsClient } from "../lib/read-client";
import { cn } from "../lib/utils";

const RUN_PAGE_SIZE = 20;
const REQUEST_PAGE_SIZE = 50;

function RefreshButton({ label, onClick, fetching }: { label: string; onClick: () => void; fetching: boolean }) {
  return <Tooltip>
    <TooltipTrigger asChild><Button size="icon" variant="ghost" aria-label={label} onClick={onClick} disabled={fetching}>
      <HugeiconsIcon icon={RefreshIcon} data-icon="inline-start" className={cn(fetching && "animate-spin motion-reduce:animate-none")} aria-hidden="true" />
    </Button></TooltipTrigger>
    <TooltipContent>{label}</TooltipContent>
  </Tooltip>;
}

function PageControls({ kind, offset, limit, total, fetching, onOffset }: { kind: "运行" | "请求"; offset: number; limit: number; total: number; fetching: boolean; onOffset: (offset: number) => void }) {
  return <nav aria-label={`${kind}分页`} className="flex w-full items-center justify-between gap-3">
    <span className="numeric text-xs text-muted-foreground">{offset + 1}–{Math.min(offset + limit, total)} / {total}</span>
    <div className="flex gap-1">
      <Button size="icon" variant="ghost" aria-label={`上一页${kind}`} disabled={offset === 0 || fetching} onClick={() => onOffset(Math.max(0, offset - limit))}><HugeiconsIcon icon={ArrowLeft01Icon} data-icon="inline-start" aria-hidden="true" /></Button>
      <Button size="icon" variant="ghost" aria-label={`下一页${kind}`} disabled={offset + limit >= total || fetching} onClick={() => onOffset(offset + limit)}><HugeiconsIcon icon={ArrowRight01Icon} data-icon="inline-start" aria-hidden="true" /></Button>
    </div>
  </nav>;
}

function RequestsPanel({ client, run }: { client: ResultsClient; run: Run }) {
  const [offset, setOffset] = useState(0);
  const requests = useQuery({
    queryKey: ["requests", run.id, REQUEST_PAGE_SIZE, offset],
    queryFn: () => client.requests(run.id, REQUEST_PAGE_SIZE, offset),
    refetchInterval: activeRunStates.has(run.state) ? 2000 : false,
  });
  return <section aria-label="逐请求指标" className="flex flex-col gap-4">
    <div className="flex justify-end"><RefreshButton label="刷新请求账本" onClick={() => void requests.refetch()} fetching={requests.isFetching} /></div>
    {requests.isError && <ErrorNotice error={requests.error} retry={() => void requests.refetch()} />}
    {requests.isPending ? <LoadingState /> : requests.data?.items?.length ? <RequestTable requests={requests.data.items} /> : !requests.isError && <EmptyState title="尚无网关请求" />}
    {requests.data && requests.data.total > 0 && <PageControls kind="请求" offset={offset} limit={REQUEST_PAGE_SIZE} total={requests.data.total} fetching={requests.isFetching} onOffset={setOffset} />}
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
  return <div className="flex flex-col gap-6"><RunDiagnostics run={run} /><section aria-label="诊断事件" className="flex flex-col gap-4">
    <div className="flex items-center justify-end gap-3">
      {events.data?.length === 500 && <Badge variant="outline">最近 500 条</Badge>}
      <RefreshButton label="刷新诊断事件" onClick={() => void events.refetch()} fetching={events.isFetching} />
    </div>
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
  return <TooltipProvider><div className="grid items-start gap-6 xl:grid-cols-[minmax(240px,300px)_minmax(0,1fr)]">
    <Card className="min-w-0 xl:sticky xl:top-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">运行 {runs.data && <Badge variant="secondary">{runs.data.total}</Badge>}</CardTitle>
        <CardAction><RefreshButton label="刷新运行列表" onClick={refresh} fetching={runs.isFetching} /></CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-3">
        {runs.isError && <ErrorNotice error={runs.error} retry={() => void runs.refetch()} />}
        {runs.isPending ? <LoadingState /> : (runs.data?.items ?? []).length === 0 ? !runs.isError && <EmptyState title="还没有评测运行" /> : <Table aria-label="持久化评测运行">
          <TableHeader className="sr-only"><TableRow><TableHead>运行</TableHead></TableRow></TableHeader>
          <TableBody>{runs.data?.items.map(item => <TableRow key={item.id} data-state={selectedID === item.id ? "selected" : undefined}>
            <TableCell>
              <Button type="button" variant="ghost" onClick={() => select(item.id)} aria-current={selectedID === item.id ? "true" : undefined} aria-label={`查看运行 ${safeMessage(item.snapshot.spec.task_names.join(", ") || item.id)} · ${safeMessage(item.id)}`} title={safeMessage(item.id)} className="h-auto w-full flex-col items-start gap-2 whitespace-normal text-left">
                <span className="max-w-full truncate">{safeMessage(item.snapshot.spec.task_names.join(", ") || item.id)}</span>
                <span className="flex w-full flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground"><span>{safeMessage(item.snapshot.model.display_name || item.snapshot.model.name)}</span><time dateTime={item.created_at}>{formatTime(item.created_at)}</time></span>
                <RunState state={item.state} />
              </Button>
            </TableCell>
          </TableRow>)}</TableBody>
        </Table>}
      </CardContent>
      {runs.data && runs.data.total > RUN_PAGE_SIZE && <CardFooter><PageControls kind="运行" offset={offset} limit={RUN_PAGE_SIZE} total={runs.data.total} fetching={runs.isFetching} onOffset={setOffset} /></CardFooter>}
    </Card>
    <div className="flex min-w-0 flex-col gap-4">
      {!selectedID ? <Card>
        <CardHeader className="sr-only"><CardTitle>运行结果</CardTitle></CardHeader>
        <CardContent>{attachment.isPending && !runs.isError ? <LoadingState label="正在查找可附着的后台运行…" /> : attachment.isError ? <ErrorNotice error={attachment.error} retry={() => void attachment.refetch()} /> : <EmptyState title="没有可展示的运行" />}</CardContent>
      </Card> : <>
        {run.isError && <ErrorNotice error={run.error} retry={() => void run.refetch()} title="无法读取此运行" />}
        {run.isPending ? <Card><CardHeader className="sr-only"><CardTitle>运行结果</CardTitle></CardHeader><CardContent><LoadingState label="正在接入运行记录…" /></CardContent></Card> : run.data && <>
          {runActions?.(run.data)}
          <Card className="min-w-0"><RunDetailHeader run={run.data} /><CardContent><Tabs key={run.data.id} defaultValue="overview">
            <div className="overflow-x-auto"><TabsList variant="line" aria-label="运行结果"><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="configuration">快照</TabsTrigger><TabsTrigger value="requests">请求</TabsTrigger><TabsTrigger value="events">诊断</TabsTrigger></TabsList></div>
            <TabsContent value="overview" className="pt-4"><RunOverview run={run.data} /></TabsContent>
            <TabsContent value="configuration" className="pt-4"><RunSnapshotDetails run={run.data} /></TabsContent>
            <TabsContent value="requests" className="pt-4"><RequestsPanel key={run.data.id} client={client} run={run.data} /></TabsContent>
            <TabsContent value="events" className="pt-4"><EventsPanel key={run.data.id} client={client} run={run.data} /></TabsContent>
          </Tabs></CardContent></Card>
        </>}
      </>}
    </div>
  </div></TooltipProvider>;
}
