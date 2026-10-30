import type { RequestRecord } from "@router-forge/api";
import { protocolLabels } from "@router-forge/api";
import { Badge } from "./ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { formatCost, formatLatency, formatTime, formatTokens, safeMessage, stateLabel, usageLabel } from "../lib/format";

export function RequestTable({ requests }: { requests: RequestRecord[] }) {
  return <Table className="min-w-[1120px] text-xs">
    <TableHeader><TableRow>
      <TableHead>请求 / 时间</TableHead><TableHead>协议 / 类型</TableHead><TableHead>状态 / HTTP</TableHead><TableHead>耗时 / 首字节</TableHead>
      <TableHead>输入 / 输出</TableHead><TableHead>缓存读 / 写</TableHead><TableHead>推理 / 总量</TableHead><TableHead>成本 / 来源</TableHead><TableHead>关联信息</TableHead>
    </TableRow></TableHeader>
    <TableBody>{requests.map(request => <TableRow key={request.id}>
      <TableCell className="max-w-44 align-top"><code className="block truncate" title={request.id}>{request.id}</code><span className="block text-muted-foreground">{formatTime(request.started_at)}</span></TableCell>
      <TableCell className="align-top"><span className="block">{protocolLabels[request.protocol]}</span><span className="text-muted-foreground">{request.kind || "未知"}</span></TableCell>
      <TableCell className="align-top"><span className="block">{stateLabel(request.state)}</span><span className="numeric text-muted-foreground">HTTP {request.http_status ?? "未知"}</span>{request.error_class && <span className="block text-destructive">{safeMessage(request.error_class)}</span>}</TableCell>
      <TableCell className="numeric whitespace-nowrap align-top"><span className="block">{formatLatency(request.duration_ms)}</span><span className="text-muted-foreground">{formatLatency(request.first_byte_ms)}</span></TableCell>
      <TableCell className="numeric align-top"><span className="block">{formatTokens(request.usage.input_tokens)} / {formatTokens(request.usage.output_tokens)}</span><Badge variant="outline" className="mt-1 font-sans">{usageLabel(request.usage)}</Badge></TableCell>
      <TableCell className="numeric whitespace-nowrap align-top">{formatTokens(request.usage.cache_read_tokens)} / {formatTokens(request.usage.cache_write_tokens)}</TableCell>
      <TableCell className="numeric whitespace-nowrap align-top">{formatTokens(request.usage.reasoning_tokens)} / {formatTokens(request.usage.total_tokens)}</TableCell>
      <TableCell className="align-top"><span className="numeric block whitespace-nowrap">{formatCost(request.cost_amount, request.currency)}</span><span className="block text-muted-foreground">{request.cost_source || "来源未知"}</span></TableCell>
      <TableCell className="min-w-36 align-top"><details><summary className="cursor-pointer text-primary">查看请求关联</summary><dl className="mt-2 max-w-72 space-y-2 break-all">
        <div><dt className="text-muted-foreground">请求 ID</dt><dd><code>{request.id}</code></dd></div>
        <div><dt className="text-muted-foreground">Task / Attempt</dt><dd><code>{request.task_id || "未知"}<br />{request.attempt_id || "未知"}</code></dd></div>
        <div><dt className="text-muted-foreground">请求模型 / 实际模型</dt><dd>{request.requested_model || "未知"} / {request.actual_model || "未知"}</dd></div>
        <div><dt className="text-muted-foreground">上游 Request / Response ID</dt><dd>{request.upstream_request_id || "未知"} / {request.upstream_response_id || "未知"}</dd></div>
        <div><dt className="text-muted-foreground">用量来源</dt><dd>{request.usage.source || "未知"}</dd></div>
        <div><dt className="text-muted-foreground">结束时间</dt><dd>{formatTime(request.finished_at)}</dd></div>
      </dl></details></TableCell>
    </TableRow>)}</TableBody>
  </Table>;
}
