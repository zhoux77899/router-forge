import { useState } from "react";
import type { JobEvent, Run } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { ArrowDown01Icon } from "@hugeicons/core-free-icons";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@router-forge/ui/components/ui/accordion";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { EmptyState } from "./feedback";
import { FrozenNetworkDetails, RunNetworkWarnings } from "./frozen-config";
import { formatTime, hasAgentObservation, missingObservation, runtimeToolVersion, safeMessage } from "../lib/format";

export function EventTimeline({ events }: { events: JobEvent[] }) {
  if (!events.length) return <EmptyState title="暂无诊断事件" />;
  return <Table aria-label="运行事件时间线">
    <TableHeader><TableRow><TableHead>时间</TableHead><TableHead>事件</TableHead><TableHead>诊断</TableHead></TableRow></TableHeader>
    <TableBody>{events.map(event => <TableRow key={event.id}>
      <TableCell className="align-top"><time className="numeric text-xs text-muted-foreground" dateTime={event.timestamp}>{formatTime(event.timestamp)}</time></TableCell>
      <TableCell className="align-top"><Badge variant="outline">{safeMessage(event.kind)}</Badge></TableCell>
      <TableCell className="min-w-64 whitespace-normal align-top">
        <p className="whitespace-pre-wrap break-words">{safeMessage(event.message)}</p>
        {event.attempt_id && <Collapsible className="mt-2">
          <CollapsibleTrigger asChild><Button size="sm" variant="ghost" className="group" aria-label={`查看事件 ${event.id} 的关联`}>
            关联
            <HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
          </Button></CollapsibleTrigger>
          <CollapsibleContent forceMount className="data-[state=closed]:hidden">
            <dl className="mt-2 flex flex-col gap-2 break-all [&_dt]:text-xs [&_dt]:text-muted-foreground">
              <div><dt>事件 ID</dt><dd>{event.id}</dd></div>
              <div><dt>Run / Attempt</dt><dd><code>{safeMessage(event.run_id)} / {safeMessage(event.attempt_id)}</code></dd></div>
            </dl>
          </CollapsibleContent>
        </Collapsible>}
      </TableCell>
    </TableRow>)}</TableBody>
  </Table>;
}

export function RunDiagnostics({ run }: { run: Run }) {
  const [openSections, setOpenSections] = useState<string[]>([]);
  const attempts = (run.tasks ?? []).flatMap(task => task.attempts ?? []);
  const runtime = run.snapshot.runtime;
  const missing = missingObservation(run.state);
  return <section aria-label="执行记录" className="flex flex-col gap-4">
    <Accordion type="multiple" value={openSections} onValueChange={setOpenSections}>
      <AccordionItem value="network">
        <AccordionTrigger>冻结网络</AccordionTrigger>
        <AccordionContent forceMount hidden={!openSections.includes("network")}>
          <div className="flex flex-col gap-3"><RunNetworkWarnings run={run} /><FrozenNetworkDetails run={run} /></div>
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="tools">
        <AccordionTrigger>工具与容器观测</AccordionTrigger>
        <AccordionContent forceMount hidden={!openSections.includes("tools")}>
          <div className="flex flex-col gap-3">
            <dl className="data-grid">
              <div><dt>实际 Harbor / uv 版本</dt><dd>{runtimeToolVersion(runtime, "harbor", missing)} / {runtimeToolVersion(runtime, "uv", missing)}</dd></div>
              <div><dt>工具观测时间</dt><dd>{runtime?.observed_at || missing}</dd></div>
              <div><dt>独立容器 Agent CLI 版本</dt><dd>{safeMessage(runtime?.agent_version || missing)}</dd></div>
              <div><dt>容器 Agent 观测时间 / 确认</dt><dd>{runtime?.agent_observed_at || missing} · {hasAgentObservation(runtime) ? "已保存独立观测" : "未完成独立版本观测确认"}</dd></div>
            </dl>
            {runtime?.agent_version_output && <div><p className="mb-2 text-xs text-muted-foreground">容器 CLI 原始 --version 输出</p><pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{safeMessage(runtime.agent_version_output)}</pre></div>}
            <p className="text-xs text-muted-foreground">独立版本观测必须在推理前成功并保存；后续失败或取消不抹去观测。镜像 tag 不代表实际 image digest。</p>
          </div>
        </AccordionContent>
      </AccordionItem>
      {attempts.map(attempt => <AccordionItem key={attempt.id} value={attempt.id}>
        <AccordionTrigger>{safeMessage(attempt.trial_name || attempt.id)}</AccordionTrigger>
        <AccordionContent forceMount hidden={!openSections.includes(attempt.id)}>
          <dl className="data-grid">
            <div><dt>Task / Attempt</dt><dd><code className="text-xs">{safeMessage(attempt.task_id)} / {safeMessage(attempt.id)}</code></dd></div>
            <div><dt>Job / Trial</dt><dd className="text-xs">{safeMessage(attempt.job_name || "未记录")} / {safeMessage(attempt.trial_name || "未记录")}</dd></div>
            <div><dt>Attempt 容器 Agent 版本记录</dt><dd>{safeMessage(attempt.agent_version || missing)}</dd></div>
            <div><dt>结果路径（执行环境内）</dt><dd><code className="text-xs">{safeMessage(attempt.result_path || "尚未落盘")}</code></dd></div>
            <div><dt>实际镜像 Digest</dt><dd>{attempt.image_digests?.length ? attempt.image_digests.map(digest => <code key={digest} className="block text-xs">{safeMessage(digest)}</code>) : "尚未解析 / 未记录"}</dd></div>
          </dl>
        </AccordionContent>
      </AccordionItem>)}
    </Accordion>
    {!attempts.length && <EmptyState title="未记录 Trial 或镜像摘要" />}
  </section>;
}
