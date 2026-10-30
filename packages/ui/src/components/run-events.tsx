import type { JobEvent, Run } from "@router-forge/api";
import { Badge } from "./ui/badge";
import { formatTime, hasAgentObservation, missingObservation, runtimeToolVersion, safeMessage } from "../lib/format";

export function EventTimeline({ events }: { events: JobEvent[] }) {
  if (!events.length) return <p className="rounded-md border border-dashed p-5 text-sm text-muted-foreground">暂无诊断事件。草稿不会自动启动，执行事件产生后将出现在这里。</p>;
  return <ol aria-label="运行事件时间线" className="divide-y">
    {events.map(event => <li key={event.id} className="grid gap-2 py-4 sm:grid-cols-[130px_1fr]">
      <div className="space-y-1"><time className="numeric text-xs text-muted-foreground" dateTime={event.timestamp}>{formatTime(event.timestamp)}</time><Badge variant="outline" className="block w-fit max-w-full break-all">{event.kind}</Badge></div>
      <div className="min-w-0"><p className="whitespace-pre-wrap break-words text-sm">{safeMessage(event.message)}</p>{event.attempt_id && <code className="mt-1 block break-all text-xs text-muted-foreground">Attempt: {event.attempt_id}</code>}</div>
    </li>)}
  </ol>;
}

export function RunDiagnostics({ run }: { run: Run }) {
  const attempts = (run.tasks ?? []).flatMap(task => task.attempts ?? []);
  const runtime = run.snapshot.runtime;
  const missing = missingObservation(run.state);
  return <section className="flex flex-col gap-3"><h3 className="font-semibold">可复核执行记录</h3>
    <dl className="data-grid">
      <div><dt>实际 Harbor / uv 版本</dt><dd>{runtimeToolVersion(runtime, "harbor", missing)} / {runtimeToolVersion(runtime, "uv", missing)}</dd></div>
      <div><dt>工具观测时间</dt><dd>{runtime?.observed_at || missing}</dd></div>
      <div><dt>独立容器 Agent CLI 版本</dt><dd>{safeMessage(runtime?.agent_version || missing)}</dd></div>
      <div><dt>容器 Agent 观测时间 / 确认</dt><dd>{runtime?.agent_observed_at || missing} · {hasAgentObservation(runtime) ? "已保存独立观测" : "未完成独立版本观测确认"}</dd></div>
    </dl>
    {runtime?.agent_version_output && <div><p className="mb-2 text-xs text-muted-foreground">容器 CLI 原始 --version 输出</p><pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{safeMessage(runtime.agent_version_output)}</pre></div>}
    {!attempts.length ? <p className="text-sm text-muted-foreground">尚无 Trial、镜像摘要或落盘结果。</p> : attempts.map(attempt => <dl key={attempt.id} className="data-grid rounded-md border p-4">
      <div><dt>Job / Trial</dt><dd className="text-xs">{attempt.job_name || "未记录"} / {attempt.trial_name || "未记录"}</dd></div>
      <div><dt>Attempt 容器 Agent 版本记录</dt><dd>{safeMessage(attempt.agent_version || missing)}</dd></div>
      <div><dt>结果路径（执行环境内）</dt><dd><code className="text-xs">{safeMessage(attempt.result_path || "尚未落盘")}</code></dd></div>
      <div><dt>实际镜像 Digest</dt><dd>{attempt.image_digests?.length ? attempt.image_digests.map(digest => <code key={digest} className="block text-xs">{digest}</code>) : "尚未解析 / 未记录"}</dd></div>
    </dl>)}
    <p className="text-xs text-muted-foreground">独立版本观测必须在推理前成功并保存；后续评分失败或取消不抹去已记录版本。这里只展示结构化指标与诊断，不读取模型原始提示或输出；镜像 tag 不能替代实际 image digest。</p>
  </section>;
}
