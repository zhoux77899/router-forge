import { activeRunStates, type Attempt, type RequestRecord, type RequestSummary, type RuntimeSnapshot } from "@router-forge/api";

export function missingObservation(state: string): string {
  return state === "draft" || activeRunStates.has(state) ? "尚未观测" : "未记录";
}

export function harborTLSPolicyLabel(skipTLSVerification: boolean | undefined): string {
  return skipTLSVerification === true ? "跳过 Harbor TLS 验证（不安全）" : "严格验证 Harbor TLS（默认）";
}

export function runtimeToolVersion(runtime: RuntimeSnapshot | null | undefined, name: string, missing: string): string {
  const tool = runtime?.tools?.find(tool => tool.name === name);
  return safeMessage(tool?.version?.trim() || tool?.version_output?.trim() || missing);
}

export function hasAgentObservation(runtime: RuntimeSnapshot | null | undefined): boolean {
  const version = runtime?.agent_version?.trim();
  return Boolean(version && !/^(?:unknown|unavailable|not available|not installed|n\/a|未知)$/i.test(version) && runtime?.agent_observed_at);
}

export const UNKNOWN = "未知";
const integerFormat = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 });

export function formatTokens(value: number | null | undefined): string {
  return value == null || !Number.isSafeInteger(value) || value < 0 ? UNKNOWN : integerFormat.format(value);
}

export function formatDecimal(value: string | null | undefined): string {
  if (value == null || !/^-?\d+(?:\.\d+)?$/.test(value)) return UNKNOWN;
  const [whole = "", fraction] = value.split(".");
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return fraction === undefined ? grouped : `${grouped}.${fraction}`;
}

export function formatCost(amount: string | null | undefined, currency: string | null | undefined): string {
  if (!currency?.trim() || formatDecimal(amount) === UNKNOWN) return UNKNOWN;
  return `${currency} ${formatDecimal(amount)}`;
}

export function formatLatency(milliseconds: number | null | undefined): string {
  if (milliseconds == null || !Number.isFinite(milliseconds) || milliseconds < 0) return UNKNOWN;
  return milliseconds < 1000 ? `${milliseconds} ms` : `${(milliseconds / 1000).toFixed(2)} s`;
}

export function formatTime(value: string | null | undefined): string {
  if (!value) return UNKNOWN;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? UNKNOWN : new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
  }).format(date);
}

export type SummaryToken = "input_tokens" | "output_tokens" | "cache_read_tokens" | "cache_write_tokens" | "reasoning_tokens";

export function summaryToken(summary: RequestSummary, field: SummaryToken): string {
  if (summary.inference_count === 0) return UNKNOWN;
  const known = formatTokens(summary[field]);
  const optional = field !== "input_tokens" && field !== "output_tokens";
  // Aggregated optional counters do not include a reported-counter count.
  if ((optional || summary.unknown_usage_count >= summary.inference_count) && summary[field] === 0) return UNKNOWN;
  return (optional || summary.unknown_usage_count > 0) && known !== UNKNOWN ? `已知 ${known}` : known;
}

export function usageLabel(usage: RequestRecord["usage"]): string {
  switch (usage.completeness) {
    case "complete": return "完整";
    case "partial": return "部分";
    case "estimated": return "估算";
    default: return UNKNOWN;
  }
}

export function rewardLabel(reward: Attempt["reward"]): string {
  if (reward == null || !Number.isFinite(reward)) return "评分未知";
  if (reward === 0) return "奖励 0 · 未解题";
  if (reward === 1) return "奖励 1 · 通过评分";
  return `奖励 ${reward} · 部分评分`;
}

export function infrastructureLabel(attempt: Attempt): string {
  if (attempt.exception_type || attempt.error) return `基础设施 / 执行异常${attempt.exception_type ? ` · ${safeMessage(attempt.exception_type)}` : ""}`;
  if (attempt.state === "cancelled" || attempt.state === "canceled") return "执行已取消";
  if (attempt.state === "completed" || attempt.state === "succeeded") return "执行结束 · 解题结论见奖励";
  return stateLabel(attempt.state);
}

export function stateLabel(state: string): string {
  const labels: Record<string, string> = {
    draft: "草稿 · 尚未启动", queued: "等待执行", preparing: "准备环境", running: "执行中", cancelling: "取消中",
    completed: "执行完成", succeeded: "执行完成", failed: "执行失败", infra_error: "基础设施异常", cancelled: "已取消", canceled: "已取消",
    pending: "等待", started: "已开始", streaming: "响应中", interrupted: "流中断", rejected: "已拒绝", unknown: UNKNOWN,
  };
  return labels[state] ?? safeMessage(state || UNKNOWN);
}

export function safeMessage(value: string, secrets: readonly string[] = []): string {
  let text = value;
  for (const secret of secrets) if (secret) text = text.split(secret).join("[已隐藏]");
  return text
    .replace(/\\(["'])/g, "$1")
    .replace(/\bBearer\s+[A-Za-z0-9._~+/=\-]+/gi, "Bearer [已隐藏]")
    .replace(/(["']?(?:api[_-]?key|control[_-]?token|gateway[_-]?token|authorization|password|access[_-]?token|refresh[_-]?token)["']?\s*[=:]\s*)(?:"(?:\\.|[^"\\])*"|'[^']*'|[^\s,;}\]]+)/gi, "$1[已隐藏]")
    .replace(/(https?:\/\/)[^/@\s]+:[^/@\s]+@/gi, "$1[已隐藏]@");
}

export function safeURL(value: string): string {
  try {
    const url = new URL(value);
    if (url.username || url.password) { url.username = "[hidden]"; url.password = ""; }
    for (const key of [...url.searchParams.keys()]) if (/key|token|password|auth/i.test(key)) url.searchParams.set(key, "[hidden]");
    return url.toString();
  } catch { return safeMessage(value); }
}

export function errorMessage(error: unknown, secrets: readonly string[] = []): string {
  return safeMessage(error instanceof Error ? error.message : "操作未完成，请重试或检查本机后端。", secrets);
}
