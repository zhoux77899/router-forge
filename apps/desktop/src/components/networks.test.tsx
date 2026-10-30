import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIClient } from "@router-forge/api";
import { NetworkCheckReadout, NetworkManager, NetworkProfileSelect, type NetworkCheckResult } from "./networks";
import { blankNetwork, mirrorTools, networkFromRecord, networkToRecord, type NetworkForm, type NetworkProfileRecord } from "../lib/network-forms";

function observedCheck(overrides: Partial<NetworkCheckResult> = {}): NetworkCheckResult {
  return { profile_id: "net_test", profile_revision: "netrev_test", target_url: "https://target.invalid", reachable: true, observed_at: "2026-01-01T00:00:00Z", issues: [], ...overrides };
}

describe("network check evidence", () => {
  test("unknown TLS and CA evidence never becomes an inferred success", () => {
    const html = renderToStaticMarkup(<NetworkCheckReadout check={observedCheck({ http_status: 204 })} />);
    expect(html).toContain("204");
    expect(html).not.toContain("已验证");
    expect(html).not.toContain("未验证");
    expect(html).not.toContain("CA SHA-256");
    expect(html).toContain("—");
  });

  test("shows only the actual loaded custom CA fingerprint", () => {
    const fingerprint = "a".repeat(64);
    const html = renderToStaticMarkup(<NetworkCheckReadout check={observedCheck({ http_status: 204, tls_verified: true, ca_sha256: fingerprint })} />);
    expect(html).toContain("已验证");
    expect(html).toContain("CA SHA-256");
    expect(html).toContain(fingerprint);
    expect(html).toContain("break-all");
  });

  test("skipped verification remains distinct from reachability", () => {
    const html = renderToStaticMarkup(<NetworkCheckReadout check={observedCheck({ tls_verified: false, issues: ["loopback fixture warning"] })} />);
    expect(html).toContain("收到网络响应");
    expect(html).toContain("未验证");
    expect(html).not.toContain("CA SHA-256");
    expect(html).toContain("loopback fixture warning");
  });
});

function selectedNetwork(): NetworkForm {
  const form = blankNetwork();
  Object.assign(form, { name: "Explicit test profile", scope: "environment", environmentID: "env_test", proxyMode: "direct", tlsMode: "verify", trustMode: "system" });
  for (const tool of mirrorTools) form.mirrors[tool].mode = "native";
  return form;
}

function publicProfile(): NetworkProfileRecord {
  const input = networkToRecord(selectedNetwork()).profile;
  return { ...input, id: "net_test", revision: "netrev_test", created_at: "2026-01-01T00:00:00Z" };
}

describe("network profile presentation", () => {
  test("renders unset modes with no address, CA, risk opt-in or hidden network request", () => {
    const cache = new QueryClient();
    cache.setQueryData(["network-profiles"], []);
    cache.setQueryData(["environments"], []);
    let requests = 0;
    const api = new APIClient("", "", async () => { requests++; throw new Error("unexpected request"); });
    const html = renderToStaticMarkup(<QueryClientProvider client={cache}><NetworkManager api={api} /></QueryClientProvider>);
    expect(requests).toBe(0);
    expect(html).toContain("尚无网络配置");
    expect(html).toContain("选择");
    expect(html).not.toContain("mirrors.tools.huawei.com");
    expect(html).not.toContain("不再验证服务器证书");
    expect(html).not.toContain('aria-checked="true"');
    const submit = html.match(/<button\b[^>]*type="submit"[^>]*>保存<\/button>/)?.[0];
    expect(submit).toBeDefined();
    expect(submit).toContain("disabled=");
  });

  test("model profile selections cannot borrow an execution-host CA or proxy", () => {
    const cache = new QueryClient();
    cache.setQueryData(["network-profiles"], [publicProfile()]);
    const html = renderToStaticMarkup(<QueryClientProvider client={cache}><NetworkProfileSelect api={new APIClient()} id="model-network" scope="desktop" value="" onValueChange={() => {}} /></QueryClientProvider>);
    expect(html).toContain("未配置");
    expect(html).not.toContain("Explicit test profile");
  });

  test("credential writes remain outside the public profile", () => {
    const form = selectedNetwork();
    form.proxyMode = "proxy";
    form.proxyURL = "https://proxy.example:443";
    form.proxyTargets = ["https"];
    form.proxyAuth = "basic";
    form.relayMode = "docker";
    form.username = "proxy-user-private";
    form.password = "proxy-password-private";
    const input = networkToRecord(form);
    expect(input.proxy_username).toBe(form.username);
    expect(input.proxy_password).toBe(form.password);
    expect(JSON.stringify(input.profile)).not.toContain(form.username);
    expect(JSON.stringify(input.profile)).not.toContain(form.password);
  });

  test("loading a saved profile neither echoes credentials nor renews risk consent", () => {
    const record: NetworkProfileRecord = {
      ...publicProfile(), proxy_mode: "proxy", proxy_url: "https://proxy.example:443",
      proxy_targets: ["https"], proxy_auth: "basic", relay_mode: "docker",
      has_credentials: true, credential_revision: "proxycredential_test",
      tls_mode: "skip", tls_risk_accepted: true,
    };
    const form = networkFromRecord(record);
    expect(form.username).toBe("");
    expect(form.password).toBe("");
    expect(form.confirmTLSRisk).toBe(false);
    expect(form.hasCredentials).toBe(true);
  });

  test("saved profile cache and native source settings are not mutated by a draft", () => {
    const record = publicProfile();
    const original = JSON.stringify(record);
    const form = networkFromRecord(record);
    form.mirrors.npm.mode = "custom";
    form.mirrors.npm.url = "https://mirror.example/npm/";
    form.proxyTargets.push("all");
    expect(JSON.stringify(record)).toBe(original);
  });

  test("choosing native sources omits previously entered custom values", () => {
    const form = selectedNetwork();
    form.mirrors.npm.url = "https://previous.example/npm/";
    form.mirrors.apt.sources = "previous source";
    const profile = networkToRecord(form).profile;
    expect(profile.mirrors?.npm).toEqual({ mode: "native" });
    expect(profile.mirrors?.apt).toEqual({ mode: "native" });
  });
});
