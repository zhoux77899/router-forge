import type { RequestRecord } from "@router-forge/api";
import { protocolLabels } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { ArrowDown01Icon } from "@hugeicons/core-free-icons";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { formatCost, formatLatency, formatTime, formatTokens, safeMessage, stateLabel, usageLabel } from "../lib/format";

export function RequestTable({ requests }: { requests: RequestRecord[] }) {
  return <Table aria-label="请求账本" className="min-w-[1060px]">
    <TableHeader><TableRow>
      <TableHead>请求 / 时间</TableHead><TableHead>协议 / 类型</TableHead><TableHead>状态 / HTTP</TableHead><TableHead>耗时 / 首字节</TableHead>
      <TableHead>输入 / 输出</TableHead><TableHead>缓存读 / 写</TableHead><TableHead>推理 / 总量</TableHead><TableHead>成本 / 来源</TableHead><TableHead><span className="sr-only">请求关联</span></TableHead>
    </TableRow></TableHeader>
    <TableBody>{requests.map(request => <TableRow key={request.id}>
      <TableCell className="max-w-44 align-top"><code className="block truncate" title={safeMessage(request.id)}>{safeMessage(request.id)}</code><time className="block text-xs text-muted-foreground" dateTime={request.started_at}>{formatTime(request.started_at)}</time></TableCell>
      <TableCell className="align-top"><span className="block">{protocolLabels[request.protocol]}</span><span className="text-xs text-muted-foreground">{safeMessage(request.kind || "未知")}</span></TableCell>
      <TableCell className="align-top"><span className="block">{stateLabel(request.state)}</span><span className="numeric text-xs text-muted-foreground">HTTP {request.http_status ?? "未知"}</span>{request.error_class && <span className="block text-xs text-destructive">{safeMessage(request.error_class)}</span>}</TableCell>
      <TableCell className="numeric align-top"><span className="block">{formatLatency(request.duration_ms)}</span><span className="text-muted-foreground">{formatLatency(request.first_byte_ms)}</span></TableCell>
      <TableCell className="align-top"><span className="numeric block">{formatTokens(request.usage.input_tokens)} / {formatTokens(request.usage.output_tokens)}</span><Badge variant="outline" className="mt-1">{usageLabel(request.usage)}</Badge></TableCell>
      <TableCell className="numeric align-top">{formatTokens(request.usage.cache_read_tokens)} / {formatTokens(request.usage.cache_write_tokens)}</TableCell>
      <TableCell className="numeric align-top">{formatTokens(request.usage.reasoning_tokens)} / {formatTokens(request.usage.total_tokens)}</TableCell>
      <TableCell className="align-top"><span className="numeric block">{formatCost(request.cost_amount, request.currency)}</span><span className="block text-xs text-muted-foreground">{safeMessage(request.cost_source || "来源未知")}</span></TableCell>
      <TableCell className="min-w-28 align-top">
        <Collapsible>
          <CollapsibleTrigger asChild>
            <Button size="sm" variant="ghost" className="group" aria-label={`查看请求 ${safeMessage(request.id)} 的关联`}>
              关联
              <HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent forceMount className="data-[state=closed]:hidden">
            <dl className="mt-3 flex max-w-72 flex-col gap-3 whitespace-normal break-all [&_dt]:text-xs [&_dt]:text-muted-foreground">
              <div><dt>请求 ID</dt><dd><code>{safeMessage(request.id)}</code></dd></div>
              <div><dt>Task / Attempt</dt><dd><code>{safeMessage(request.task_id || "未知")}<br />{safeMessage(request.attempt_id || "未知")}</code></dd></div>
              <div><dt>请求模型 / 实际模型</dt><dd>{safeMessage(request.requested_model || "未知")} / {safeMessage(request.actual_model || "未知")}</dd></div>
              <div><dt>上游 Request / Response ID</dt><dd>{safeMessage(request.upstream_request_id || "未知")} / {safeMessage(request.upstream_response_id || "未知")}</dd></div>
              <div><dt>用量来源</dt><dd>{safeMessage(request.usage.source || "未知")}</dd></div>
              <div><dt>结束时间</dt><dd>{formatTime(request.finished_at)}</dd></div>
            </dl>
          </CollapsibleContent>
        </Collapsible>
      </TableCell>
    </TableRow>)}</TableBody>
  </Table>;
}
