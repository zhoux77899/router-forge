import type { APIClient, Run } from "@router-forge/api";
import { agentReasons } from "./forms";

export function legacyDraft(run: Run): boolean {
  return run.state === "draft" && run.snapshot.format_version !== 2;
}

export function boundedDraft(run: Run): boolean {
  const { spec, suite, resolved_tasks, format_version, config_hash, resolution_hash, resolved_at } = run.snapshot;
  return run.state === "draft" && format_version === 2 && spec.suite_id === "swebench-verified"
    && suite.id === spec.suite_id && suite.dataset === "swe-bench/swe-bench-verified"
    && spec.task_names.length === 1 && Boolean(spec.task_names[0])
    && resolved_tasks?.length === 1 && resolved_tasks[0]?.name === spec.task_names[0]
    && Boolean(config_hash && resolution_hash && resolved_at)
    && spec.concurrency === 1 && spec.attempts === 1 && !spec.agent_version && run.snapshot.agent.name === spec.agent
    && !Object.keys(spec.agent_kwargs ?? {}).some(key => /(^|[_-])version([_-]|$)/i.test(key.replace(/([a-z])([A-Z])/g, "$1_$2")))
    && Number.isInteger(spec.max_requests) && spec.max_requests >= 1 && spec.max_requests <= 10000
    && Number.isInteger(spec.max_output_tokens) && spec.max_output_tokens >= 1 && spec.max_output_tokens <= 1000000
    && Number.isInteger(spec.timeout_sec) && spec.timeout_sec >= 1 && spec.timeout_sec <= 86400
    && Number.isInteger(spec.setup_timeout_sec) && spec.setup_timeout_sec >= 1 && spec.setup_timeout_sec <= 86400
    && run.snapshot.provider.has_key && agentReasons(run.snapshot.agent, run.snapshot.model.protocol).length === 0;
}

export async function startConfirmedRun(api: Pick<APIClient, "startRun">, run: Run, paidConfirmed: boolean): Promise<Run> {
  if (legacyDraft(run)) throw new Error("此草稿使用旧运行快照格式，不能启动；请重新创建草稿以解析当前来源。不会自动迁移旧来源。");
  if (!paidConfirmed || !boundedDraft(run)) throw new Error("请明确确认此运行快照的来源与付费执行边界后再启动。每次只启动一个任务。");
  return api.startRun(run.id, true);
}
