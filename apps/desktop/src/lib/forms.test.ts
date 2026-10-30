import { describe, expect, test } from "bun:test";
import { testAgent, testEnvironment, testInspection, testModel, testProvider, testResolution, testSuite } from "@router-forge/ui/lib/fixtures.test-support";
import { agentReasons, blankEnvironment, blankModel, blankRun, validateEnvironment, validateModel, validateProvider, validateRun, type RunChoices } from "./forms";

const choices: RunChoices = { environments: [testEnvironment], models: [testModel], providers: [testProvider], suites: [testSuite], resolution: testResolution, inspection: testInspection };
const fields = () => ({ ...blankRun(), environmentID: testEnvironment.id, modelID: testModel.id, taskName: "swe-bench/offline-task", agentName: testAgent.name });

describe("environment configuration", () => {
  test("requires real SSH identity fields and bounded port", () => {
    expect(validateEnvironment({ ...blankEnvironment(), name: "SSH", type: "ssh", host: "", user: "", port: "0" }).ok).toBe(false);
    const result = validateEnvironment({ ...blankEnvironment(), name: "SSH", type: "ssh", host: "server.test", user: "test", port: "22", harborPath: "/test/harbor", dockerPath: "/test/docker" });
    expect(result.ok).toBe(true); if (result.ok) { expect(result.value.type).toBe("ssh"); expect(result.value.tools).toEqual({ harbor: "/test/harbor", docker: "/test/docker" }); }
  });
  test("WSL profiles do not carry hidden SSH settings and never disable TLS", () => {
    const result = validateEnvironment({ ...blankEnvironment(), name: "WSL", host: "unused.test", user: "unused", caCertPath: "/test/trusted.pem" });
    expect(result.ok).toBe(true); if (result.ok) { expect(result.value.host).toBeUndefined(); expect(result.value.ca_cert_path).toBe("/test/trusted.pem"); }
    expect(validateEnvironment({ ...blankEnvironment(), name: "WSL", proxyURL: "https://name:password@proxy.test" }).ok).toBe(false);
  });
});

describe("write-only provider and exact prices", () => {
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
