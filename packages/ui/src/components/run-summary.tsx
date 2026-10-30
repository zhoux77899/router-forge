import type { Attempt, RequestSummary, Run } from "@router-forge/api";
import { activeRunStates, protocolLabels } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { ArrowDown01Icon } from "@hugeicons/core-free-icons";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Separator } from "@router-forge/ui/components/ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { EmptyState, ErrorNotice } from "./feedback";
import { FrozenNetworkDetails, hasProfiledNetwork, legacyHarborTLSLabel, RunNetworkWarnings } from "./frozen-config";
import { formatCost, formatTime, infrastructureLabel, missingObservation, rewardLabel, runtimeToolVersion, safeMessage, stateLabel, summaryToken } from "../lib/format";

export function RunState({ state }: { state: string }) {
  return <Badge variant={state === "failed" || state === "infra_error" ? "destructive" : activeRunStates.has(state) ? "default" : "secondary"}>
    {stateLabel(state)}
  </Badge>;
}

export function MeteringSummary({ summary }: { summary: RequestSummary }) {
  const fields = [
    ["input_tokens", "输入 Token"], ["output_tokens", "输出 Token"], ["cache_read_tokens", "缓存读取"],
    ["cache_write_tokens", "缓存写入"], ["reasoning_tokens", "推理 Token（输出子项）"],
  ] as const;
  const costs = summary.costs ?? [];
  return <section aria-label="请求账本" className="flex flex-col gap-4">
    <dl className="grid grid-cols-2 gap-5 md:grid-cols-3 [&_dt]:text-xs [&_dt]:text-muted-foreground [&_dd]:mt-1 [&_dd]:text-xs [&_dd]:font-semibold">
      <div><dt>请求 / 推理</dt><dd className="numeric">{summary.request_count} / {summary.inference_count}</dd></div>
      {fields.map(([field, label]) => <div key={field}><dt>{label}</dt><dd className="numeric">{summaryToken(summary, field)}</dd></div>)}
      <div className="col-span-2 md:col-span-3"><dt>成本</dt><dd>
        {costs.length ? <ul className="flex flex-wrap gap-x-6 gap-y-2">{costs.map(cost => <li key={cost.currency} className="flex flex-wrap items-baseline gap-2"><span className="numeric">{formatCost(cost.amount, cost.currency)}</span><span className="text-xs font-normal text-muted-foreground">{cost.estimated ? "按已配置价格估算" : "记录值"}</span></li>)}</ul> : "成本未知"}
      </dd></div>
    </dl>
    {(summary.unknown_usage_count > 0 || summary.unknown_cost_count > 0) && <div className="flex flex-wrap gap-2">
      {summary.unknown_usage_count > 0 && <Badge variant="outline">{summary.unknown_usage_count} 条用量未知或不完整</Badge>}
      {summary.unknown_cost_count > 0 && <Badge variant="outline">{summary.unknown_cost_count} 条成本未知</Badge>}
    </div>}
    <Collapsible className="flex flex-col gap-2">
      <CollapsibleTrigger asChild><Button size="sm" variant="ghost" className="group w-fit">
        指标口径
        <HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
      </Button></CollapsibleTrigger>
      <CollapsibleContent forceMount className="flex flex-col gap-2 text-xs text-muted-foreground data-[state=closed]:hidden">
        <p>NULL 表示未知；可选累计值仅标示已知部分。缓存及推理独立记录，不再次叠加进输出或总量。</p>
        <p>成本按币种独立记录，不自动汇兑；缺失成本不补为零。</p>
        <p>执行状态、基础设施异常与 Harbor 奖励独立记录。CLI 退出码 0 不代表解题成功。</p>
      </CollapsibleContent>
    </Collapsible>
  </section>;
}

function AttemptRows({ attempt }: { attempt: Attempt }) {
  return <TableRow>
    <TableCell className="max-w-44 align-top"><code className="block truncate text-xs" title={safeMessage(attempt.id)}>{safeMessage(attempt.id)}</code></TableCell>
    <TableCell className="align-top"><RunState state={attempt.state} /></TableCell>
    <TableCell className="min-w-40 whitespace-normal align-top"><span>{attempt.exception_type || attempt.error || attempt.state === "infra_error" ? infrastructureLabel(attempt) : "未记录异常"}</span>{attempt.error && <p className="mt-1 max-w-[55ch] break-words text-xs text-destructive">{safeMessage(attempt.error)}</p>}</TableCell>
    <TableCell className="numeric align-top">{attempt.exit_code ?? "未知"}</TableCell>
    <TableCell className="min-w-40 align-top">{rewardLabel(attempt.reward)}</TableCell>
    <TableCell className="align-top text-xs"><span className="block">{formatTime(attempt.started_at)}</span><span className="text-muted-foreground">{formatTime(attempt.finished_at)}</span></TableCell>
  </TableRow>;
}

export function RunOverview({ run }: { run: Run }) {
  const tasks = run.tasks ?? [];
  const missing = missingObservation(run.state);
  const profiledNetwork = hasProfiledNetwork(run);
  return <div className="flex flex-col gap-6">
    <RunNetworkWarnings run={run} />
    {run.error && <ErrorNotice error={new Error(run.error)} title="执行错误" />}
    <MeteringSummary summary={run.summary} />
    <Separator />
    <section aria-label="任务与执行尝试" className="flex flex-col gap-4">
      {tasks.length === 0 ? <EmptyState title={run.state === "draft" ? "草稿尚未分配执行尝试" : "未记录执行尝试"} /> : tasks.map(task => <Card key={task.id}>
        <CardHeader>
          <CardTitle>{safeMessage(task.name)}</CardTitle>
          <CardAction><RunState state={task.state} /></CardAction>
        </CardHeader>
        <CardContent>
          {task.attempts?.length ? <Table aria-label={`${safeMessage(task.name)} 的执行尝试`}><TableHeader><TableRow><TableHead>尝试</TableHead><TableHead>执行状态</TableHead><TableHead>基础设施</TableHead><TableHead>CLI 退出码</TableHead><TableHead>Harbor 奖励</TableHead><TableHead>开始 / 结束</TableHead></TableRow></TableHeader><TableBody>{task.attempts.map(attempt => <AttemptRows key={attempt.id} attempt={attempt} />)}</TableBody></Table> : <EmptyState title="尚未产生尝试" />}
        </CardContent>
      </Card>)}
    </section>
    <Collapsible className="flex flex-col gap-3">
      <CollapsibleTrigger asChild><Button size="sm" variant="ghost" className="group w-fit">
        环境与工具
        <HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
      </Button></CollapsibleTrigger>
      <CollapsibleContent forceMount className="flex flex-col gap-3 data-[state=closed]:hidden">
        <dl className="data-grid">
          <div><dt>执行环境</dt><dd>{safeMessage(run.snapshot.environment.name)} · {run.snapshot.environment.type.toUpperCase()}</dd></div>
          <div><dt>创建时间</dt><dd>{formatTime(run.created_at)}</dd></div>
          <div><dt>开始 / 结束</dt><dd>{formatTime(run.started_at)} / {formatTime(run.finished_at)}</dd></div>
          {!profiledNetwork && <div><dt>历史 Harbor TLS 设置</dt><dd>{legacyHarborTLSLabel(run)}</dd></div>}
          <div><dt>实际 Harbor / uv 版本</dt><dd>{runtimeToolVersion(run.snapshot.runtime, "harbor", missing)} / {runtimeToolVersion(run.snapshot.runtime, "uv", missing)}</dd></div>
          <div><dt>实际容器 Agent CLI 版本</dt><dd>{safeMessage(run.snapshot.runtime?.agent_version || missing)}</dd></div>
        </dl>
        {!profiledNetwork && <p className="text-xs text-muted-foreground">历史 TLS 设置不是实际 TLS 连接验证结果；未记录值不推定默认策略。</p>}
        <FrozenNetworkDetails run={run} />
      </CollapsibleContent>
    </Collapsible>
  </div>;
}

export function RunDetailHeader({ run }: { run: Run }) {
  return <CardHeader className="min-w-0">
    <CardTitle className="break-words">{safeMessage(run.snapshot.spec.task_names.join(", ") || "评测草稿")}</CardTitle>
    <CardDescription className="flex flex-wrap items-center gap-2">
      <span>{safeMessage(run.snapshot.model.display_name || run.snapshot.model.name)}</span>
      <Badge variant="outline">{protocolLabels[run.snapshot.model.protocol]}</Badge>
      <span>{safeMessage(run.snapshot.environment.name)}</span>
    </CardDescription>
    <CardAction><RunState state={run.state} /></CardAction>
  </CardHeader>;
}

export function SummaryPanel({ run }: { run: Run }) {
  return <Card><RunDetailHeader run={run} /><CardContent><RunOverview run={run} /></CardContent></Card>;
}
