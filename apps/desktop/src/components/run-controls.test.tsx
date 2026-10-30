import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient, type NetworkProfile, type Run } from "@router-forge/api";
import { runFixture as historicalFixture, testEnvironment } from "@router-forge/ui/lib/fixtures.test-support";
import { RunControls } from "./run-controls";

function profile(id: string, scope: "desktop" | "environment" = "environment"): NetworkProfile {
  return { id, name: id, scope, ...(scope === "environment" ? { environment_id: "env_test" } : {}), revision: `revision-${id}`, created_at: "2026-01-01T00:00:00Z", proxy_mode: "direct", proxy_auth: "none", tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false, mirrors: { apt: { mode: "native" }, pypi: { mode: "native" }, npm: { mode: "native" }, node: { mode: "native" }, uv: { mode: "native" }, uv_python: { mode: "native" } } };
}
function runFixture(): Run {
  const draft = structuredClone(historicalFixture());
  const network = { source: profile("network_source"), build: profile("network_build"), agent: profile("network_agent"), verifier: profile("network_verifier"), model: profile("network_model", "desktop") };
  draft.snapshot.format_version = 3; draft.snapshot.network = network;
  draft.snapshot.environment.networks = { source_profile_id: network.source.id, build_profile_id: network.build.id, agent_profile_id: network.agent.id, verifier_profile_id: network.verifier.id };
  draft.snapshot.environment.docker_gateway_mode = "detect"; draft.snapshot.provider.network_profile_id = network.model.id;
  return draft;
}
function renderControls(run: Run, cache?: QueryClient): string {
  let calls = 0;
  const client = new APIClient("", "", async () => { calls++; throw new Error("Rendering must not start or cancel a run"); });
  const queryClient = cache ?? new QueryClient();
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><RunControls api={client} run={run} /></QueryClientProvider>);
  expect(calls).toBe(0); queryClient.clear(); return html;
}
function checkboxTag(html: string, id: string): string {
  const tag = (html.match(/<button\b[^>]*>/g) ?? []).find(tag => tag.includes(`id="${id}"`));
  if (!tag) throw new Error(`Missing checkbox: ${id}`);
  return tag;
}

test("new v3 draft payment is unchecked and Start disabled until explicit independent consent", () => {
  const html = renderControls(runFixture());
  expect(checkboxTag(html, "confirm-paid-run_test")).toContain('aria-checked="false"');
  expect(html).toMatch(/<button[^>]*disabled[^>]*>[^]*?确认并启动这一个任务/);
  expect(html).toContain("明文"); expect(html).toContain("不是绝对金额上限"); expect(html).toContain("1 / 1 / 0"); expect(html).toContain("账本成本可能未知");
  expect(html).toContain('data-slot="card"'); expect(html).toContain('data-slot="field"'); expect(html).toContain('data-slot="field-label"');
  expect(html).toContain('for="confirm-paid-run_test"'); expect(html).toContain('aria-describedby="paid-version-run_test paid-cost-run_test"');
  expect(html).toContain('id="paid-version-run_test"'); expect(html).toContain('id="paid-cost-run_test"');
  expect(html).not.toContain('id="confirm-execution-tls-run_test"'); expect(html).not.toContain('id="confirm-model-tls-run_test"');
});

test("historical and networkless drafts show recreation with disabled Start and available cancellation", () => {
  const drafts = [undefined, 0, 1, 2, 4].map(format => { const draft = runFixture(); draft.snapshot.format_version = format; return draft; });
  for (const network of [undefined, null]) { const draft = runFixture(); draft.snapshot.network = network; drafts.push(draft); }
  for (const draft of drafts) {
    const html = renderControls(draft);
    expect(html).toContain("Historical draft cannot start"); expect(html).toContain("Recreate this draft with format-3 network profiles"); expect(html).toContain("not migrated automatically");
    const buttons = html.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? [];
    expect(buttons.find(button => button.includes("确认并启动这一个任务"))).toMatch(/\sdisabled(?:=|[ >])/);
    expect(buttons.find(button => button.includes("取消此草稿"))).not.toMatch(/\sdisabled(?:=|[ >])/);
    expect(checkboxTag(html, "confirm-paid-run_test")).toContain('aria-checked="false"');
  }
});

test("start confirmation displays saved package identity, hashes and all five frozen profile revisions", () => {
  const draft = runFixture(); draft.snapshot.resolution_hash = "saved-source-not-preview"; draft.snapshot.config_hash = "approved-config-hash";
  draft.snapshot.resolved_at = "2026-02-03T04:05:06Z";
  draft.snapshot.resolved_tasks = [{ name: "swe-bench/offline-task", kind: "package", package_name: "swe-bench/offline-task", ref: `sha256:${"b".repeat(64)}`, revision: "saved-task-revision" }];
  const html = renderControls(draft);
  expect(html).toContain("saved-source-not-preview"); expect(html).toContain("approved-config-hash"); expect(html).toContain("2026-02-03T04:05:06Z"); expect(html).toContain(`sha256:${"b".repeat(64)}`);
  expect(html).toContain("未提供；使用 Package Digest"); expect(html).toContain("Harbor 默认容器安装"); expect(html).toContain("独立观测成功并落盘确认后才允许推理");
  expect(html).toContain('aria-label="Frozen network profiles"');
  for (const stage of ["source", "build", "agent", "verifier", "model"]) expect(html).toContain(`revision-network_${stage}`);
  expect(html).not.toContain("显式版本");
});

test("execution, model and payment checkboxes are distinct, unselected and linked to visible necessary warnings", () => {
  const draft = runFixture(); const network = draft.snapshot.network!;
  network.source = { ...network.source, tls_mode: "skip", tls_risk_accepted: true }; network.model = { ...network.model, tls_mode: "skip", tls_risk_accepted: true };
  draft.snapshot.spec.confirm_paid = true;
  const html = renderControls(draft);
  for (const id of ["confirm-execution-tls-run_test", "confirm-model-tls-run_test", "confirm-paid-run_test"]) {
    expect(checkboxTag(html, id)).toContain('aria-checked="false"'); expect(html).toContain(`for="${id}"`);
  }
  expect(checkboxTag(html, "confirm-execution-tls-run_test")).toContain('aria-describedby="execution-tls-risk-run_test"');
  expect(checkboxTag(html, "confirm-model-tls-run_test")).toContain('aria-describedby="model-tls-risk-run_test"');
  expect(checkboxTag(html, "confirm-paid-run_test")).not.toContain("tls-risk");
  expect(html).toContain("Execution TLS verification is disabled"); expect(html).toContain("Model TLS verification is disabled"); expect(html).toContain("MITM");
  expect(html).toContain("model HTTPS, SSH host checks, Docker daemon pulls, original Dockerfile builds or verifier checks");
  expect(html).toContain("intercept model requests, responses or the API Key"); expect(html).toContain("Execution TLS approval does not authorize this risk");
  expect(html).toContain("I reviewed this immutable snapshot and authorize one paid task within these limits.");
  expect(html).toMatch(/<button[^>]*disabled[^>]*>[^]*?确认并启动这一个任务/);
});

test("frozen execution and model TLS policies ignore edited caches and legacy flags without cross-scope inheritance", () => {
  for (const scope of ["none", "execution", "model"]) {
    const draft = runFixture(); draft.snapshot.environment.skip_tls_verification = true; draft.snapshot.environment.ca_cert_path = "/legacy/ignored.pem";
    if (scope === "execution") draft.snapshot.network!.source = { ...draft.snapshot.network!.source, tls_mode: "skip", trust_mode: "custom", ca_cert_path: "/frozen/source.pem", tls_risk_accepted: true };
    if (scope === "model") draft.snapshot.network!.model = { ...draft.snapshot.network!.model, tls_mode: "skip", trust_mode: "custom", ca_cert_path: "/frozen/model.pem", tls_risk_accepted: true };
    const cache = new QueryClient();
    cache.setQueryData(["environments"], [{ ...testEnvironment, ca_cert_path: "/cache/edited-after-draft.pem" }]);
    cache.setQueryData(["network-profiles"], Object.values(draft.snapshot.network!).map(profile => ({ ...profile, name: "edited-after-draft", revision: "new-cache-revision", tls_mode: "skip" })));
    const before = JSON.stringify(draft); const html = renderControls(draft, cache);
    expect(html).not.toContain("/legacy/ignored.pem"); expect(html).not.toContain("/cache/edited-after-draft.pem"); expect(html).not.toContain("edited-after-draft"); expect(html).not.toContain("new-cache-revision");
    expect(html.includes('id="confirm-execution-tls-run_test"')).toBe(scope === "execution");
    expect(html.includes('id="confirm-model-tls-run_test"')).toBe(scope === "model");
    if (scope !== "none") expect(html).toContain(`/${scope === "execution" ? "frozen/source" : "frozen/model"}.pem`);
    expect(checkboxTag(html, "confirm-paid-run_test")).toContain('aria-checked="false"'); expect(JSON.stringify(draft)).toBe(before);
  }
});

test("historical draft network settings remain readable as migration diagnostics, never editable approvals", () => {
  const draft = historicalFixture(); draft.snapshot.environment = { ...testEnvironment, skip_tls_verification: true, ca_cert_path: "/legacy/retained.pem" };
  const html = renderControls(draft);
  expect(html).toContain("Legacy network migration diagnostic"); expect(html).toContain("/legacy/retained.pem"); expect(html).toContain("Read-only historical values");
  expect(html).not.toContain('id="confirm-execution-tls-run_test"'); expect(html).not.toContain('id="confirm-model-tls-run_test"');
});

test("provider key is cleared before write without cache persistence, and profile bindings are Desktop-only", () => {
  const source = readFileSync(join(import.meta.dir, "services.tsx"), "utf8");
  expect(source.indexOf('setNewKey("")', source.indexOf("async function saveProvider"))).toBeLessThan(source.indexOf("await api.saveProvider"));
  expect(source).not.toMatch(/useMutation|localStorage|sessionStorage|console\.(?:log|debug|info)/);
  expect(source).toContain('type="password"'); expect(source).toContain('autoComplete="new-password"');
  expect(source).toContain('<NetworkProfileSelect api={api} scope="desktop"'); expect(source).toContain('updateProvider("networkProfileID", value)');
});
