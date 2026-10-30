import { describe, expect, test } from "bun:test";
import { attemptFixture, requestFixture, testSummary } from "./fixtures.test-support";
import { formatCost, formatDecimal, formatLatency, formatTime, formatTokens, harborTLSPolicyLabel, hasAgentObservation, infrastructureLabel, missingObservation, rewardLabel, runtimeToolVersion, safeMessage, summaryToken, usageLabel } from "./format";

test("actual tool versions preserve successful custom output without numerical gates or defaults", () => {
  expect(runtimeToolVersion(undefined, "harbor", "尚未观测")).toBe("尚未观测");
  expect(runtimeToolVersion(null, "uv", "未记录")).toBe("未记录");
  const runtime = { tools: [{ name: "harbor", available: true, version: "", version_output: "Harbor custom development build" }, { name: "uv", available: true, version: "0.12.0", version_output: "uv 0.12.0" }], observed_at: "2026-01-01T00:00:00Z" };
  expect(runtimeToolVersion(runtime, "harbor", "未记录")).toBe("Harbor custom development build");
  expect(runtimeToolVersion(runtime, "uv", "未记录")).toBe("0.12.0");
  expect(runtimeToolVersion(runtime, "missing", "未记录")).toBe("未记录");
  for (const state of ["draft", "preparing", "running", "cancelling"]) expect(missingObservation(state)).toBe("尚未观测");
  for (const state of ["completed", "failed", "cancelled"]) expect(missingObservation(state)).toBe("未记录");
});

test("TLS policy labels require explicit opt-in and default missing historical settings to strict configuration", () => {
  expect(harborTLSPolicyLabel(undefined)).toBe("严格验证 Harbor TLS（默认）");
  expect(harborTLSPolicyLabel(false)).toBe("严格验证 Harbor TLS（默认）");
  expect(harborTLSPolicyLabel(true)).toBe("跳过 Harbor TLS 验证（不安全）");
});

test("agent observation requires a real CLI version and persisted acknowledgment, not raw output alone", () => {
  expect(hasAgentObservation(undefined)).toBe(false);
  const runtime = { tools: [], observed_at: "2026-01-01T00:00:00Z", agent_version_output: "agent unknown", agent_observed_at: "2026-01-01T00:01:00Z" };
  expect(hasAgentObservation(runtime)).toBe(false);
  expect(hasAgentObservation({ ...runtime, agent_version: "unknown" })).toBe(false);
  expect(hasAgentObservation({ ...runtime, agent_version: "container-development-build", agent_observed_at: undefined })).toBe(false);
  expect(hasAgentObservation({ ...runtime, agent_version: "container-development-build" })).toBe(true);
});

describe("unknown and partial metering", () => {
  test("null, missing and unsafe counters are unknown, while measured zero is zero", () => {
    expect(formatTokens(null)).toBe("未知"); expect(formatTokens(undefined)).toBe("未知"); expect(formatTokens(Number.NaN)).toBe("未知"); expect(formatTokens(Number.MAX_SAFE_INTEGER + 1)).toBe("未知"); expect(formatTokens(0)).toBe("0"); expect(formatTokens(1200)).toBe("1,200");
  });
  test("zero optional aggregate subtotals do not invent reported cache/reasoning zero", () => {
    expect(summaryToken(testSummary, "input_tokens")).toBe("12"); expect(summaryToken(testSummary, "cache_read_tokens")).toBe("未知"); expect(summaryToken(testSummary, "reasoning_tokens")).toBe("未知");
    expect(summaryToken({ ...testSummary, unknown_usage_count: 1, input_tokens: 12 }, "input_tokens")).toBe("已知 12");
    expect(summaryToken({ ...testSummary, unknown_usage_count: 1, input_tokens: 0 }, "input_tokens")).toBe("未知");
  });
  test("latency and usage completeness are separate from execution success", () => {
    expect(formatLatency(null)).toBe("未知"); expect(formatLatency(0)).toBe("0 ms"); expect(formatLatency(1240)).toBe("1.24 s");
    expect(usageLabel({ ...requestFixture().usage, completeness: "partial" })).toBe("部分"); expect(formatTime("bad-date")).toBe("未知");
  });
});

describe("exact per-currency costs", () => {
  test("does not convert exact decimal prices/amounts to floating point", () => {
    expect(formatDecimal("12345678901234567890.00000980246912858024691276")).toBe("12,345,678,901,234,567,890.00000980246912858024691276");
    expect(formatCost("0.00000980246912858024691276", "USD")).toBe("USD 0.00000980246912858024691276");
    expect(formatCost("1.2300", "CNY")).toBe("CNY 1.2300"); expect(formatCost("0", "USD")).toBe("USD 0");
  });
  test("missing amount or currency stays unknown rather than free", () => {
    expect(formatCost(null, "USD")).toBe("未知"); expect(formatCost("0", null)).toBe("未知"); expect(formatDecimal("1e-9")).toBe("未知");
  });
});

test("Harbor reward=0 and CLI exit=0 do not claim success", () => {
  const attempt = attemptFixture({ reward: 0, exit_code: 0, exception_type: "AgentTimeoutError" });
  expect(rewardLabel(attempt.reward)).toContain("未解题"); expect(infrastructureLabel(attempt)).toContain("异常"); expect(rewardLabel(null)).toBe("评分未知");
  expect(infrastructureLabel(attemptFixture())).toContain("解题结论见奖励");
});

test("diagnostics defensively hide credentials", () => {
  const output = safeMessage("Bearer offline-secret api_key=offline-key https://name:pass@example.invalid", ["explicit-only-test"]);
  expect(output).not.toContain("offline-secret"); expect(output).not.toContain("offline-key"); expect(output).not.toContain("name:pass");
  expect(safeMessage('{"api_key":"quoted secret with spaces"}')).not.toContain("quoted secret");
  expect(safeMessage(String.raw`failed {\"api_key\":\"escaped secret\"}`)).not.toContain("escaped secret");
});
