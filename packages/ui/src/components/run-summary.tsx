import type { Attempt, RequestSummary, Run } from "@router-forge/api";
import { activeRunStates, protocolLabels } from "@router-forge/api";
import { Badge } from "./ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { formatCost, formatTime, infrastructureLabel, missingObservation, rewardLabel, runtimeToolVersion, safeMessage, stateLabel, summaryToken } from "../lib/format";

export function RunState({ state }: { state: string }) {
  const active = activeRunStates.has(state);
  return <Badge variant={state === "failed" || state === "infra_error" ? "destructive" : "secondary"} className={active ? "gap-1.5 bg-accent text-accent-foreground" : ""}>
    {active && <span className="status-live h-1.5 w-1.5 rounded-full bg-primary" aria-hidden="true" />}
    {stateLabel(state)}
  </Badge>;
}

export function MeteringSummary({ summary }: { summary: RequestSummary }) {
  const fields = [
    ["input_tokens", "输入 Token"], ["output_tokens", "输出 Token"], ["cache_read_tokens", "缓存读取"],
    ["cache_write_tokens", "缓存写入"], ["reasoning_tokens", "推理 Token（输出子项）"],
  ] as const;
  const costs = summary.costs ?? [];
  return <section aria-labelledby="metering-heading" className="space-y-4">
    <div className="panel-heading">
      <div><h3 id="metering-heading" className="font-semibold">请求账本</h3><p className="text-xs text-muted-foreground">网关记录的用量与成本；缺失字段不会补为零。</p></div>
      <span className="text-sm">{summary.request_count} 条请求 · {summary.inference_count} 次推理</span>
    </div>
    <dl className="grid grid-cols-2 gap-x-6 gap-y-4 border-y py-4 md:grid-cols-3">
      {fields.map(([field, label]) => <div key={field}><dt className="text-xs text-muted-foreground">{label}</dt><dd className="numeric mt-1 text-base font-semibold">{summaryToken(summary, field)}</dd></div>)}
    </dl>
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div>
        <p className="text-xs text-muted-foreground">成本 · 按币种独立记录，不自动汇兑</p>
        {costs.length ? <ul className="mt-1 space-y-1">{costs.map(cost => <li key={cost.currency} className="numeric font-semibold">{formatCost(cost.amount, cost.currency)} <span className="font-sans text-xs font-normal text-muted-foreground">{cost.estimated ? "按已配置价格估算" : "记录值"}</span></li>)}</ul> : <p className="mt-1 font-medium">成本未知</p>}
      </div>
      <p className="max-w-[46ch] text-xs text-muted-foreground">{summary.unknown_usage_count} 条请求用量未知或不完整；{summary.unknown_cost_count} 条请求成本未知。缓存及推理是独立分项，不再次叠加进输出或总量。</p>
    </div>
  </section>;
}

function AttemptRows({ attempt }: { attempt: Attempt }) {
  return <TableRow>
    <TableCell className="min-w-44 align-top"><code className="block break-all text-xs">{attempt.id}</code><span className="mt-1 block text-xs text-muted-foreground">Trial: {attempt.trial_name || "尚未生成"}</span></TableCell>
    <TableCell className="min-w-48 align-top"><span className="block text-sm">{infrastructureLabel(attempt)}</span><span className="block text-xs text-muted-foreground">CLI 退出码: {attempt.exit_code == null ? "未知" : attempt.exit_code}</span>{attempt.error && <p className="mt-1 max-w-[55ch] break-words text-xs text-destructive">{safeMessage(attempt.error)}</p>}</TableCell>
    <TableCell className="min-w-40 align-top">{rewardLabel(attempt.reward)}</TableCell>
    <TableCell className="align-top text-xs"><span className="block">{formatTime(attempt.started_at)}</span><span className="text-muted-foreground">至 {formatTime(attempt.finished_at)}</span></TableCell>
  </TableRow>;
}

export function RunOverview({ run }: { run: Run }) {
  const model = run.snapshot.model;
  const tasks = run.tasks ?? [];
  return <div className="space-y-6">
    <dl className="data-grid">
      <div><dt>固定模型 / 原生协议</dt><dd>{model.display_name || model.name} <Badge variant="outline">{protocolLabels[model.protocol]}</Badge></dd></div>
      <div><dt>执行环境</dt><dd>{run.snapshot.environment.name} · {run.snapshot.environment.type.toUpperCase()}</dd></div>
      <div><dt>创建时间</dt><dd>{formatTime(run.created_at)}</dd></div>
      <div><dt>开始 / 结束</dt><dd>{formatTime(run.started_at)} / {formatTime(run.finished_at)}</dd></div>
      <div><dt>实际 Harbor / uv 版本</dt><dd>{runtimeToolVersion(run.snapshot.runtime, "harbor", missingObservation(run.state))} / {runtimeToolVersion(run.snapshot.runtime, "uv", missingObservation(run.state))}</dd></div>
      <div><dt>实际容器 Agent CLI 版本</dt><dd>{safeMessage(run.snapshot.runtime?.agent_version || missingObservation(run.state))}</dd></div>
    </dl>
    {run.error && <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">{safeMessage(run.error)}</div>}
    <MeteringSummary summary={run.summary} />
    <section aria-labelledby="attempt-heading" className="space-y-3">
      <h3 id="attempt-heading" className="font-semibold">任务与执行尝试</h3>
      <p className="text-xs text-muted-foreground">生命周期、基础设施状态与 Harbor 奖励分别记录。奖励 0 或 CLI 退出码 0 都不代表解题成功。</p>
      {tasks.length === 0 ? <p className="rounded-md border border-dashed p-5 text-sm text-muted-foreground">草稿尚未分配执行尝试。</p> : tasks.map(task => <div key={task.id} className="overflow-hidden rounded-md border">
        <div className="flex flex-wrap items-center justify-between gap-2 bg-muted/50 px-4 py-3"><div><h4 className="font-medium">{task.name}</h4><code className="block break-all text-xs text-muted-foreground">Task ID: {task.id}</code></div><RunState state={task.state} /></div>
        {task.attempts?.length ? <Table><TableHeader><TableRow><TableHead>Attempt / Trial</TableHead><TableHead>基础设施 / 执行</TableHead><TableHead>Harbor 奖励</TableHead><TableHead>时间</TableHead></TableRow></TableHeader><TableBody>{task.attempts.map(attempt => <AttemptRows key={attempt.id} attempt={attempt} />)}</TableBody></Table> : <p className="px-4 py-5 text-sm text-muted-foreground">尚未产生尝试。</p>}
      </div>)}
    </section>
  </div>;
}

export function RunDetailHeader({ run }: { run: Run }) {
  return <CardHeader className="border-b px-5 py-5">
    <div className="panel-heading"><div className="min-w-0"><CardTitle className="text-lg">{run.snapshot.spec.task_names.join(", ") || "评测草稿"}</CardTitle><code className="mt-1 block break-all text-xs text-muted-foreground">{run.id}</code></div><RunState state={run.state} /></div>
  </CardHeader>;
}

export function SummaryPanel({ run }: { run: Run }) {
  return <Card><RunDetailHeader run={run} /><CardContent><RunOverview run={run} /></CardContent></Card>;
}
