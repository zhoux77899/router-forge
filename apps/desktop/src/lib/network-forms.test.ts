import { describe, expect, test } from "bun:test";
import { blankNetwork, changeNetwork, mirrorTools, validateNetwork, type NetworkForm } from "./network-forms";

function explicitNetwork(scope: "desktop" | "environment" = "desktop"): NetworkForm {
  const form = blankNetwork();
  form.name = "Test network";
  form.scope = scope;
  form.environmentID = scope === "environment" ? "env_test" : "";
  form.proxyMode = "direct";
  form.tlsMode = "verify";
  form.trustMode = "system";
  for (const tool of mirrorTools) form.mirrors[tool].mode = "native";
  return form;
}

describe("explicit network forms", () => {
  test("starts without a network, CA, TLS, authentication or mirror selection", () => {
    const form = blankNetwork();
    expect(form.scope).toBe("");
    expect(form.proxyMode).toBe("");
    expect(form.proxyAuth).toBe("");
    expect(form.proxyURL).toBe("");
    expect(form.proxyTargets).toEqual([]);
    expect(form.tlsMode).toBe("");
    expect(form.trustMode).toBe("");
    expect(form.caCertPath).toBe("");
    expect(form.confirmTLSRisk).toBe(false);
    for (const tool of mirrorTools) expect(form.mirrors[tool]).toEqual({ mode: "", url: "", sources: "" });
  });

  test("accepts an explicitly selected direct system-trust network", () => {
    expect(validateNetwork(explicitNetwork())).toBeUndefined();
  });

  test("requires every applicable download source, not a guessed default", () => {
    const form = explicitNetwork("environment");
    form.mirrors.uv.mode = "";
    expect(validateNetwork(form)).toContain("uv");
  });

  test("never validates an embedded proxy credential", () => {
    const form = explicitNetwork();
    form.proxyMode = "proxy";
    form.proxyURL = "https://private-user:private-password@proxy.example:443";
    form.proxyTargets = ["https"];
    form.proxyAuth = "none";
    const error = validateNetwork(form);
    expect(error).toContain("不含凭据");
    expect(error).not.toContain("private-user");
    expect(error).not.toContain("private-password");
  });

  test("requires explicit protocol and authentication choices for a proxy", () => {
    const form = explicitNetwork();
    form.proxyMode = "proxy";
    form.proxyURL = "https://proxy.example:443";
    expect(validateNetwork(form)).toContain("适用协议");
    form.proxyTargets = ["https"];
    expect(validateNetwork(form)).toContain("认证方式");
  });

  test("permits retaining a write-only credential but requires replacements after clearing", () => {
    const form = explicitNetwork();
    form.proxyMode = "proxy";
    form.proxyURL = "https://proxy.example:443";
    form.proxyTargets = ["https"];
    form.proxyAuth = "basic";
    form.relayMode = "loopback";
    expect(validateNetwork(form)).toContain("用户名和密码");
    form.hasCredentials = true;
    expect(validateNetwork(form)).toBeUndefined();
    form.clearCredentials = true;
    expect(validateNetwork(form)).toContain("用户名和密码");
  });

  test("requires a separate TLS risk confirmation", () => {
    const form = explicitNetwork();
    form.tlsMode = "skip";
    expect(validateNetwork(form)).toContain("风险");
    form.confirmTLSRisk = true;
    expect(validateNetwork(form)).toBeUndefined();
  });

  test("preserves an ignored CA path while skipping verification", () => {
    const form = explicitNetwork();
    form.tlsMode = "skip";
    form.trustMode = "custom";
    form.caCertPath = "/missing-but-retained/ca.pem";
    form.confirmTLSRisk = true;
    expect(validateNetwork(form)).toBeUndefined();
    expect(form.caCertPath).toBe("/missing-but-retained/ca.pem");
  });

  test("configuration, credential and destination edits invalidate TLS confirmation", () => {
    const form = explicitNetwork();
    form.tlsMode = "skip";
    form.confirmTLSRisk = true;
    expect(changeNetwork(form, "proxyURL", "https://proxy.example:443").confirmTLSRisk).toBe(false);
    expect(changeNetwork(form, "password", "replacement").confirmTLSRisk).toBe(false);
    const changed = changeNetwork(form, "scope", "environment");
    expect(changed.confirmTLSRisk).toBe(false);
    expect(changed.environmentID).toBe("");
    expect(changed.relayMode).toBe("");
  });

  test("does not leave a proxy or secret active when switching to direct", () => {
    const form = explicitNetwork();
    form.proxyMode = "proxy";
    form.proxyURL = "https://proxy.example:443";
    form.proxyTargets = ["all"];
    form.proxyAuth = "basic";
    form.username = "private-user";
    form.password = "private-password";
    const direct = changeNetwork(form, "proxyMode", "direct");
    expect(direct.proxyURL).toBe("");
    expect(direct.proxyTargets).toEqual([]);
    expect(direct.username).toBe("");
    expect(direct.password).toBe("");
  });

  test("does not accept credentials hidden inside a package mirror URL", () => {
    const form = explicitNetwork("environment");
    form.mirrors.pypi = { mode: "custom", url: "https://name:password@mirror.example/simple", sources: "" };
    expect(validateNetwork(form)).toContain("不含凭据");
  });
});
