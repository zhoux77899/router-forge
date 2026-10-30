import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient, type Environment, type NetworkProfile } from "@router-forge/api";
import { testEnvironment } from "@router-forge/ui/lib/fixtures.test-support";
import { blankEnvironment, environmentFields, validateEnvironment } from "../lib/forms";
import { EnvironmentsPanel } from "./environments";
import { TextField } from "./fields";

function sourceProfile(environmentID = testEnvironment.id, scope: "desktop" | "environment" = "environment"): NetworkProfile {
  return { id: "network_source", name: "Selected host Source", scope, ...(scope === "environment" ? { environment_id: environmentID } : {}), revision: "revision-source", created_at: "2026-01-01T00:00:00Z", proxy_mode: "direct", tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false };
}
function renderPanel(profiles: Environment[], selectedID = "", networkProfiles: NetworkProfile[] = []): string {
  let calls = 0;
  const api = new APIClient("", "", async () => { calls++; throw new Error("Rendering must not send environment operations"); });
  const queryClient = new QueryClient();
  queryClient.setQueryData(["environments"], profiles);
  queryClient.setQueryData(["network-profiles"], networkProfiles);
  const html = renderToStaticMarkup(<QueryClientProvider client={queryClient}><EnvironmentsPanel api={api} selectedID={selectedID} onSelect={() => {}} /></QueryClientProvider>);
  expect(calls).toBe(0);
  queryClient.clear();
  return html;
}

function inputTag(html: string, id: string): string {
  const tag = (html.match(/<input\b[^>]*>/g) ?? []).find(tag => tag.includes(`id="${id}"`));
  if (!tag) throw new Error(`Missing input: ${id}`);
  return tag;
}

test("fields use official semantic primitives and link validation feedback without empty help rows", () => {
  const plain = renderToStaticMarkup(<TextField id="plain-field" label="名称" value="" onChange={() => {}} />);
  expect(plain).toContain('data-slot="field"');
  expect(plain).toContain('data-slot="field-label"');
  expect(plain).toContain('for="plain-field"');
  expect(plain).not.toContain("plain-field-help");
  const invalid = renderToStaticMarkup(<TextField id="invalid-field" label="名称" value="" onChange={() => {}} error="请填写名称。" />);
  expect(invalid).toContain('data-invalid="true"');
  expect(invalid).toContain('aria-invalid="true"');
  expect(invalid).toContain('aria-describedby="invalid-field-help"');
  expect(invalid).toContain('id="invalid-field-help"');
  expect(invalid).toContain('data-slot="field-error"');
  expect(invalid).toContain('role="alert"');
  const disabled = renderToStaticMarkup(<TextField id="disabled-field" label="CA" value="retained-path" onChange={() => {}} disabled hint="路径已保留。" />);
  expect(disabled).toContain('data-disabled="true"');
  expect(inputTag(disabled, "disabled-field")).toMatch(/\sdisabled(?:=|[ >])/);
  expect(disabled).toContain('aria-describedby="disabled-field-help"');
  expect(disabled).toContain('data-slot="field-description"');
});

test("SSH ports are displayed exactly as saved and absent ports are never filled with 22", () => {
  for (const port of [undefined, 2222]) {
    const profile = { ...testEnvironment, type: "ssh" as const, host: "offline.invalid", user: "test", port };
    const html = renderPanel([profile], profile.id);
    expect(inputTag(html, "env-port")).toContain(`value="${port ?? ""}"`);
    expect(html).toContain('data-slot="field-set"');
    expect(html).toContain('data-slot="field-legend"');
    expect(html).toContain('data-slot="collapsible-trigger"');
    expect(html).not.toMatch(/<(?:details|summary)\b/);
  }
});

test("new hosts start with disabled host-scoped selectors and no inline legacy settings or gateway defaults", () => {
  const html = renderPanel([]);
  expect(html).toContain("Save this environment, then create its host-scoped profiles in Network.");
  for (const stage of ["source", "build", "agent", "verifier"]) {
    const trigger = (html.match(/<button\b[^>]*>/g) ?? []).find(tag => tag.includes(`id="env-network-${stage}"`));
    expect(trigger).toMatch(/\sdisabled(?:=|[ >])/); expect(html).toContain(`for="env-network-${stage}"`);
  }
  expect(html).toContain('id="env-gateway-mode"'); expect(html).toContain("Unconfigured");
  for (const id of ["env-proxy", "env-no-proxy", "env-ca", "env-skip-tls", "env-gateway"]) expect(html).not.toContain(`id="${id}"`);
  expect(html).not.toContain("Legacy network settings");
});

test("historical proxy, CA, TLS and gateway values are a read-only migration diagnostic", () => {
  const profile: Environment = { ...testEnvironment, skip_tls_verification: true, proxy_url: "https://legacy.invalid:8443", no_proxy: "legacy.internal", ca_cert_path: "/legacy/retained.pem", docker_host_gateway: "legacy.gateway" };
  const before = JSON.stringify(profile); const html = renderPanel([profile], profile.id);
  expect(html).toContain("Legacy network migration diagnostic"); expect(html).toContain("Read-only historical values"); expect(html).toContain("do not use or inherit them");
  expect(html).toContain("/legacy/retained.pem"); expect(html).toContain("legacy.gateway"); expect(html).toContain("Skip (historical)");
  for (const id of ["env-proxy", "env-no-proxy", "env-ca", "env-skip-tls", "env-gateway"]) expect(html).not.toContain(`id="${id}"`);
  const loaded = environmentFields(profile); const input = validateEnvironment(loaded);
  if (!input.ok) throw new Error("Expected an incomplete saved host");
  expect(input.value.networks).toEqual(blankEnvironment().networks); expect(input.value.docker_gateway_mode).toBeUndefined();
  for (const key of ["proxy_url", "no_proxy", "ca_cert_path", "skip_tls_verification"]) expect(input.value).not.toHaveProperty(key);
  expect(JSON.stringify(profile)).toBe(before);
});

test("Source selection is limited to the saved host and permits inspection before the other run stages", () => {
  const environment: Environment = { ...testEnvironment, networks: { ...blankEnvironment().networks, source_profile_id: "network_source" } };
  const html = renderPanel([environment], environment.id, [sourceProfile()]);
  expect(html).toContain("Selected host Source");
  const buttons = html.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? [];
  expect(buttons.find(button => button.includes("检查环境"))).not.toMatch(/\sdisabled(?:=|[ >])/);
  expect(html).toContain("Unconfigured");
  for (const profile of [sourceProfile("other-host"), sourceProfile(environment.id, "desktop")]) {
    const invalid = renderPanel([environment], environment.id, [profile]);
    expect(invalid).toContain("配置失效"); expect(invalid).toContain("for this environment");
    const actions = invalid.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? [];
    expect(actions.find(button => button.includes("检查环境"))).toMatch(/\sdisabled(?:=|[ >])/);
  }
});

test("gateway host input appears only for an explicit manual mode and never contains an inferred address", () => {
  for (const docker_gateway_mode of [undefined, "detect"] as const) {
    const environment = { ...testEnvironment, docker_gateway_mode, docker_host_gateway: "previous.gateway" };
    const html = renderPanel([environment], environment.id);
    expect(html).not.toContain('id="env-gateway"');
  }
  const manual = { ...testEnvironment, docker_gateway_mode: "manual" as const, docker_host_gateway: "explicit.gateway" };
  const html = renderPanel([manual], manual.id);
  expect(inputTag(html, "env-gateway")).toContain('value="explicit.gateway"'); expect(html).toContain("Manual host address");
});
