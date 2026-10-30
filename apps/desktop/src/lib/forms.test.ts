import { describe, expect, test } from "bun:test";
import { APIClient, type Environment, type EnvironmentInput, type NetworkProfile } from "@router-forge/api";
import { testAgent, testEnvironment, testInspection, testModel, testProvider, testResolution, testSuite } from "@router-forge/ui/lib/fixtures.test-support";
import { agentReasons, blankEnvironment, blankModel, blankProvider, blankRun, environmentFields, environmentNetworkErrors, executionNetworkStages, providerFields, validateEnvironment, validateModel, validateProvider, validateRun, type RunChoices } from "./forms";

function networkProfile(id: string, scope: "desktop" | "environment" = "environment", environmentID = testEnvironment.id): NetworkProfile {
  return { id, name: id, scope, ...(scope === "environment" ? { environment_id: environmentID } : {}), proxy_mode: "direct", proxy_auth: "none", tls_mode: "verify", trust_mode: "system", has_credentials: false, tls_risk_accepted: false, revision: "offline-revision", created_at: "2026-01-01T00:00:00Z", mirrors: { apt: { mode: "native" }, pypi: { mode: "native" }, npm: { mode: "native" }, node: { mode: "native" }, uv: { mode: "native" }, uv_python: { mode: "native" } } };
}
const networkProfiles = [...executionNetworkStages.map(stage => networkProfile(`network_${stage}`)), networkProfile("network_model", "desktop")];
const configuredEnvironment: Environment = { ...testEnvironment, networks: { source_profile_id: "network_source", build_profile_id: "network_build", agent_profile_id: "network_agent", verifier_profile_id: "network_verifier" }, docker_gateway_mode: "detect" };
const configuredProvider = { ...testProvider, network_profile_id: "network_model" };
const choices: RunChoices = { environments: [configuredEnvironment], models: [testModel], providers: [configuredProvider], suites: [testSuite], networkProfiles, resolution: testResolution, inspection: testInspection };
const fields = () => ({ ...blankRun(), environmentID: testEnvironment.id, modelID: testModel.id, taskName: "swe-bench/offline-task", agentName: testAgent.name });

describe("environment configuration", () => {
  test("SSH port has no implicit default and missing historical ports require an explicit value", () => {
    expect(blankEnvironment().port).toBe("");
    for (const port of [undefined, 0]) {
      const loaded = environmentFields({ ...testEnvironment, type: "ssh", host: "server.test", user: "test", port });
      expect(loaded.port).toBe(port == null ? "" : "0");
      const result = validateEnvironment(loaded);
      expect(result.ok).toBe(false);
      if (!result.ok) expect(result.errors.port).toContain("1–65535");
    }
    expect(environmentFields({ ...testEnvironment, port: 2222 }).port).toBe("2222");
  });
  test("requires real SSH identity fields and bounded port", () => {
    expect(validateEnvironment({ ...blankEnvironment(), name: "SSH", type: "ssh", host: "", user: "", port: "0" }).ok).toBe(false);
    const result = validateEnvironment({ ...blankEnvironment(), name: "SSH", type: "ssh", host: "server.test", user: "test", port: "22", harborPath: "/test/harbor", dockerPath: "/test/docker" });
    expect(result.ok).toBe(true); if (result.ok) { expect(result.value.type).toBe("ssh"); expect(result.value.tools).toEqual({ harbor: "/test/harbor", docker: "/test/docker" }); }
  });
  test("new WSL hosts can be saved incomplete without hidden SSH or network defaults", () => {
    const blank = blankEnvironment();
    expect(blank.networks).toEqual({ source_profile_id: "", build_profile_id: "", agent_profile_id: "", verifier_profile_id: "" });
    expect(blank.dockerGatewayMode).toBe(""); expect(blank.dockerGateway).toBe("");
    const result = validateEnvironment({ ...blank, name: "WSL", host: "unused.test", user: "unused" });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.host).toBeUndefined(); expect(result.value.networks).toEqual(blank.networks);
      expect(result.value.docker_gateway_mode).toBeUndefined(); expect(result.value.docker_host_gateway).toBe("");
      for (const key of ["proxy_url", "no_proxy", "ca_cert_path", "skip_tls_verification"]) expect(result.value).not.toHaveProperty(key);
    }
    expect(validateEnvironment({ ...blankEnvironment(), name: "SSH", type: "ssh", user: "offline", port: "22", host: "https://name:password@proxy.test" }).ok).toBe(false);
  });
  test("historical proxy, CA, TLS and gateway values never become new form or request defaults", () => {
    for (const type of ["local", "wsl", "ssh"] as const) for (const networks of [undefined, null]) {
      const loaded = environmentFields({ ...testEnvironment, type, host: "server.test", user: "test", port: 22, networks, proxy_url: "https://legacy-proxy.invalid:8443", no_proxy: "legacy.invalid", ca_cert_path: "/legacy/ca.pem", skip_tls_verification: true, docker_host_gateway: "legacy.gateway" });
      expect(loaded.networks).toEqual(blankEnvironment().networks); expect(loaded.dockerGatewayMode).toBe(""); expect(loaded.dockerGateway).toBe("");
      for (const key of ["proxyURL", "noProxy", "caCertPath", "skipTLSVerification"]) expect(loaded).not.toHaveProperty(key);
      const result = validateEnvironment(loaded);
      expect(result.ok).toBe(true);
      if (result.ok) for (const key of ["proxy_url", "no_proxy", "ca_cert_path", "skip_tls_verification", "docker_gateway_mode"]) expect(result.value).not.toHaveProperty(key);
    }
  });
  test("host-scoped Source selection requires a saved host and is sufficient for inspection only", () => {
    const partial: Environment = { ...testEnvironment, networks: { ...blankEnvironment().networks, source_profile_id: "network_source" } };
    const loaded = environmentFields(partial);
    expect(validateEnvironment(loaded, networkProfiles).ok).toBe(true);
    expect(environmentNetworkErrors(partial, networkProfiles)).toEqual({});
    expect(Object.keys(environmentNetworkErrors(partial, networkProfiles, true))).toEqual(["build", "agent", "verifier", "dockerGatewayMode"]);
    const unsaved = validateEnvironment({ ...loaded, id: "" }, networkProfiles);
    expect(unsaved.ok).toBe(false); if (!unsaved.ok) expect(unsaved.errors.source).toContain("Save this environment");
    for (const profile of [networkProfile("network_source", "desktop"), networkProfile("network_source", "environment", "other-host")]) {
      const invalid = validateEnvironment(loaded, [profile]);
      expect(invalid.ok).toBe(false); if (!invalid.ok) expect(invalid.errors.source).toContain("for this environment");
      expect(environmentNetworkErrors(partial, [profile]).source).toContain("for this environment");
    }
  });
  test("Docker detection and manual addresses are explicit and hidden addresses are never reused", () => {
    expect(validateEnvironment({ ...blankEnvironment(), name: "Host", dockerGatewayMode: "manual" }).ok).toBe(false);
    for (const address of ["https://gateway.invalid", "user@gateway.invalid", "host with spaces"]) expect(validateEnvironment({ ...blankEnvironment(), name: "Host", dockerGatewayMode: "manual", dockerGateway: address }).ok).toBe(false);
    const manual = validateEnvironment({ ...blankEnvironment(), name: "Host", dockerGatewayMode: "manual", dockerGateway: "  gateway.invalid  " });
    if (!manual.ok) throw new Error("Expected explicit manual gateway");
    expect(manual.value.docker_gateway_mode).toBe("manual"); expect(manual.value.docker_host_gateway).toBe("gateway.invalid");
    const detected = validateEnvironment({ ...blankEnvironment(), name: "Host", dockerGatewayMode: "detect", dockerGateway: "hidden-old-host" });
    if (!detected.ok) throw new Error("Expected explicit detection mode");
    expect(detected.value.docker_gateway_mode).toBe("detect"); expect(detected.value.docker_host_gateway).toBe("");
    expect(environmentFields({ ...testEnvironment, docker_gateway_mode: "detect", docker_host_gateway: "observed-old-host" }).dockerGateway).toBe("");
  });
  test("environment API writes exact selected IDs and explicit mode without copying historical values", async () => {
    let saved: Environment = { ...configuredEnvironment, skip_tls_verification: true, proxy_url: "https://legacy.invalid", ca_cert_path: "/legacy/ca.pem" };
    const calls: { method: string; path: string; input?: EnvironmentInput }[] = [];
    const api = new APIClient("", "", async (method, path, body) => {
      if (method === "GET" && path === "/api/environments") { calls.push({ method, path }); return { status: 200, body: JSON.stringify([saved]) }; }
      if (method === "POST" && path === "/api/environments") {
        const input: EnvironmentInput = JSON.parse(body);
        calls.push({ method, path, input }); saved = { ...saved, ...input };
        return { status: 200, body: JSON.stringify(saved) };
      }
      throw new Error(`Unexpected request: ${method} ${path}`);
    });
    const loaded = environmentFields((await api.environments())[0]!);
    const result = validateEnvironment({ ...loaded, name: "Edited host" }, networkProfiles);
    if (!result.ok) throw new Error("Expected valid profile bindings");
    await api.saveEnvironment(result.value);
    const reloaded = environmentFields((await api.environments())[0]!);
    expect(reloaded).toEqual({ ...loaded, name: "Edited host" });
    const write = calls.find(call => call.method === "POST")?.input;
    expect(write?.networks).toEqual(configuredEnvironment.networks); expect(write?.docker_gateway_mode).toBe("detect");
    for (const key of ["proxy_url", "no_proxy", "ca_cert_path", "skip_tls_verification"]) expect(write).not.toHaveProperty(key);
    expect(saved.ca_cert_path).toBe("/legacy/ca.pem");
    expect(calls.every(call => call.path === "/api/environments")).toBe(true);
  });
});

describe("write-only provider and exact prices", () => {
  test("provider profiles start unset and only explicit Desktop selections are written", () => {
    expect(blankProvider().networkProfileID).toBe("");
    expect(providerFields(testProvider).networkProfileID).toBe("");
    expect(providerFields(configuredProvider).networkProfileID).toBe("network_model");
    const provider = { id: testProvider.id, name: testProvider.name, baseURL: testProvider.base_url };
    for (const networkProfileID of [undefined, null, "", "network_model"]) {
      const result = validateProvider({ ...provider, networkProfileID }, "", networkProfiles);
      if (!result.ok) throw new Error("Expected an explicit or retained provider binding");
      expect(result.value.network_profile_id).toBe(networkProfileID);
      expect(Object.hasOwn(result.value, "network_profile_id")).toBe(networkProfileID !== undefined);
      expect(result.value).not.toHaveProperty("api_key");
    }
    const invalid = validateProvider({ ...provider, networkProfileID: "network_source" }, "", networkProfiles);
    expect(invalid.ok).toBe(false); if (!invalid.ok) expect(invalid.errors.networkProfileID).toContain("Desktop");
  });
  test("blank replacement key is omitted to retain saved key", () => {
    const result = validateProvider({ id: "provider_test", name: "Existing", baseURL: "https://provider.invalid/v1" }, "");
    expect(result.ok).toBe(true); if (result.ok) expect(Object.hasOwn(result.value, "api_key")).toBe(false);
  });
  test("new provider key required, URL credentials and whitespace key rejected", () => {
    expect(validateProvider({ id: "", name: "New", baseURL: "https://provider.invalid/v1" }, "").ok).toBe(false);
    expect(validateProvider({ id: "provider_test", name: "New", baseURL: "https://provider.invalid/v1?api_key=offline" }, "").ok).toBe(false);
    expect(validateProvider({ id: "provider_test", name: "New", baseURL: "https://provider.invalid/v1" }, "has spaces").ok).toBe(false);
  });
  test("prices remain exact decimal strings and unspecified cache price stays absent", () => {
    const result = validateModel({ ...blankModel(), providerID: testProvider.id, name: "native-model", displayName: "Native", priceEnabled: true, currency: "CNY", inputPrice: "0.000000000000000001", outputPrice: "1.2300", priceSource: "offline-test" }, [testProvider]);
    expect(result.ok).toBe(true); if (result.ok) { expect(result.value.pricing?.input_per_million).toBe("0.000000000000000001"); expect(result.value.pricing?.cache_read_per_million).toBeUndefined(); }
    expect(validateModel({ ...blankModel(), providerID: testProvider.id, name: "m", displayName: "M", priceEnabled: true, inputPrice: "1e-9", outputPrice: "0", priceSource: "test" }, [testProvider]).ok).toBe(false);
  });
});

describe("fixed-model, single-task draft validation", () => {
  test("requires all four host profiles, the Desktop model profile and explicit Docker mode", () => {
    for (const stage of executionNetworkStages) {
      const incomplete = { ...configuredEnvironment, networks: { ...configuredEnvironment.networks!, [`${stage}_profile_id`]: "" } };
      const result = validateRun(fields(), { ...choices, environments: [incomplete] });
      expect(result.ok).toBe(false); if (!result.ok) expect(result.errors.environmentID).toContain(stage);
    }
    for (const environment of [{ ...configuredEnvironment, networks: null }, { ...configuredEnvironment, docker_gateway_mode: undefined }, { ...configuredEnvironment, docker_gateway_mode: "manual" as const, docker_host_gateway: "" }]) expect(validateRun(fields(), { ...choices, environments: [environment] }).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, providers: [testProvider] }).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, providers: [{ ...configuredProvider, network_profile_id: "network_source" }] }).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, networkProfiles: [] }).ok).toBe(false);
  });
  test("rejects unsupported Build, verifier skip and cross-host stage bindings", () => {
    const build = networkProfiles.find(profile => profile.id === "network_build")!;
    const invalidProfiles: NetworkProfile[] = [{ ...build, tls_mode: "skip" }, { ...build, trust_mode: "custom" }, { ...build, proxy_auth: "basic" }, { ...build, mirrors: { ...build.mirrors!, apt: { mode: "custom", sources: "deb https://apt.invalid stable main" } } }, { ...networkProfiles[3]!, tls_mode: "skip" }, { ...networkProfiles[0]!, environment_id: "foreign-host" }];
    for (const profile of invalidProfiles) expect(validateRun(fields(), { ...choices, networkProfiles: networkProfiles.map(saved => saved.id === profile.id ? profile : saved) }).ok).toBe(false);
  });
  test("saved TLS risk acceptance never becomes payment or start confirmations in a draft request", () => {
    const accepted = networkProfiles.map(profile => profile.id === "network_source" || profile.id === "network_model" ? { ...profile, tls_mode: "skip" as const, tls_risk_accepted: true } : profile);
    const result = validateRun(fields(), { ...choices, networkProfiles: accepted, environments: [{ ...configuredEnvironment, skip_tls_verification: true, proxy_url: "https://legacy.invalid" }] });
    if (!result.ok) throw new Error("Expected explicitly bound saved profiles");
    expect(result.value.confirm_paid).toBe(false);
    for (const key of ["confirm_execution_tls_risk", "confirm_model_tls_risk", "network", "proxy_url", "skip_tls_verification"]) expect(result.value).not.toHaveProperty(key);
  });
  test("accepts inspected compatible agent without host CLI installed and never confirms payment", () => {
    const result = validateRun(fields(), choices);
    expect(result.ok).toBe(true); if (result.ok) { expect(result.value.task_names).toEqual(["swe-bench/offline-task"]); expect(result.value.concurrency).toBe(1); expect(result.value.attempts).toBe(1); expect(result.value.confirm_paid).toBe(false); expect(result.value).not.toHaveProperty("agent_version"); expect(result.value).not.toHaveProperty("resolution_hash"); }
    expect(agentReasons(testAgent, testModel.protocol)).toEqual([]);
  });
  test("rejects stale/failed inspections, unknown task, provider without key and incompatible protocol", () => {
    expect(validateRun(fields(), { ...choices, inspection: { ...testInspection, environment_id: "other" } }).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, inspection: { ...testInspection, ready: false } }).ok).toBe(false);
    expect(validateRun({ ...fields(), taskName: "outside-catalog" }, choices).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, providers: [{ ...testProvider, has_key: false }] }).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, models: [{ ...testModel, protocol: "responses" }] }).ok).toBe(false);
    expect(agentReasons({ ...testAgent, command: "" }, testModel.protocol)).not.toEqual([]);
  });
  test("host CLI versions and historical supports_version never become requested container versions", () => {
    for (const version of [undefined, "", "1.2.3", "1.2.3-beta.1+build.5", "host-development-build"]) {
      const result = validateRun(fields(), { ...choices, inspection: { ...testInspection, agents: [{ ...testAgent, version, supports_version: false }] } });
      expect(result.ok).toBe(true);
      if (result.ok) expect(Object.hasOwn(result.value, "agent_version")).toBe(false);
    }
    expect(Object.hasOwn(blankRun(), "agentVersion")).toBe(false);
    expect(agentReasons({ ...testAgent, supports_version: false }, testModel.protocol)).toEqual([]);
    expect(agentReasons({ ...testAgent, harbor_supported: false }, testModel.protocol)).not.toEqual([]);
  });
  test("resolution must be for the canonical dataset and task names are never matched by basename", () => {
    expect(validateRun(fields(), { ...choices, resolution: undefined }).ok).toBe(false);
    expect(validateRun(fields(), { ...choices, resolution: { ...testResolution, suite: { ...testResolution.suite, dataset: "other/swe-bench-verified" } } }).ok).toBe(false);
    expect(validateRun({ ...fields(), taskName: "offline-task" }, choices).ok).toBe(false);
    expect(validateRun({ ...fields(), taskName: "other/offline-task" }, choices).ok).toBe(false);
    const gitResolution = { ...testResolution, registry_kind: "harbor", tasks: [{ name: "git-task", kind: "git", path: "tasks/git-task", git_url: "https://git.invalid/tasks.git", git_commit: "a".repeat(40) }] };
    expect(validateRun({ ...fields(), taskName: "git-task" }, { ...choices, resolution: gitResolution }).ok).toBe(true);
  });
  test("only actual schema scalar kwargs accepted and dangerous overrides blocked", () => {
    expect(validateRun({ ...fields(), kwargsText: '{"temperature":0.2,"max_iterations":4,"enabled":true}' }, choices).ok).toBe(true);
    for (const kwargsText of ['null', '[]', '{bad}', '{"version":"1.2.3"}', '{"model":"override"}', '{"api_key":"test-only"}', '{"baseURL":"https://elsewhere.test"}', '{"env":{"x":"y"}}', '{"unknown":1}', '{"temperature":[]}', '{"max_iterations":1.2}']) expect(validateRun({ ...fields(), kwargsText }, choices).ok).toBe(false);
    const declaresVersion = { ...choices, inspection: { ...testInspection, agents: [{ ...testAgent, schema: { properties: { version: { type: "string" } } } }] } };
    expect(validateRun({ ...fields(), kwargsText: '{"version":"1.2.3"}' }, declaresVersion).ok).toBe(false);
  });
  test("schema-declared path overrides are still forbidden", () => {
    const withPaths = { ...choices, inspection: { ...testInspection, agents: [{ ...testAgent, schema: { type: "object", properties: { agent_path: { type: "string" }, work_dir: { type: "string" }, cwd: { type: "string" } } } }] } };
    for (const key of ["agent_path", "work_dir", "cwd"]) expect(validateRun({ ...fields(), kwargsText: JSON.stringify({ [key]: "/not-controlled" }) }, withPaths).ok).toBe(false);
  });
  test("bounded integer timeouts and budgets required", () => {
    for (const [field, value] of [["timeout", "0"], ["setupTimeout", "86401"], ["maxRequests", "10001"], ["maxOutputTokens", "1000001"], ["maxRequests", "1.5"]] as const) expect(validateRun({ ...fields(), [field]: value }, choices).ok).toBe(false);
  });
});
