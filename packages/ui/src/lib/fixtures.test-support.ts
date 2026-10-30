import type { Agent, Attempt, Environment, Inspection, Model, Provider, RequestRecord, RequestSummary, ResolvedBenchmark, Run, Suite } from "@router-forge/api";

// Offline test data only. Application entry points never import this module.
export const testEnvironment: Environment = { id: "env_test", name: "Offline Linux", type: "wsl", distribution: "test-distro", work_dir: "/test", created_at: "2026-01-01T00:00:00Z" };
export const testProvider: Provider = { id: "provider_test", name: "Offline provider", base_url: "https://provider.invalid/v1", has_key: true, key_revision: "test-key-reference", created_at: "2026-01-01T00:00:00Z" };
export const testModel: Model = { id: "model_test", provider_id: "provider_test", name: "offline-model", display_name: "Offline model", protocol: "chat_completions", created_at: "2026-01-01T00:00:00Z" };
export const testAgent: Agent = { name: "test-agent", command: "test-agent", installed: false, harbor_supported: true, supported_protocols: ["chat_completions"], supports_version: false, container_install: "Harbor default container installation", version: "host-cli-dev", schema: { type: "object", properties: { temperature: { type: "number" }, max_iterations: { type: "integer" }, enabled: { type: "boolean" } } } };
export const testInspection: Inspection = { environment_id: "env_test", os: "linux", arch: "amd64", home_dir: "/test", work_dir: "/test", ready: true, tools: [], agents: [testAgent], issues: [], docker_gateway: "test-only" };
export const testSuite: Suite = { id: "swebench-verified", name: "Offline suite", dataset: "swe-bench/swe-bench-verified", version: "", registry_url: "", task_repository: "", task_commit: "", task_count: 0, suggested_task: "" };
export const testResolution: ResolvedBenchmark = {
  suite: { ...testSuite, version: "offline-revision", registry_url: "https://hub.invalid", task_count: 1 },
  registry_kind: "package", registry_metadata_url: "https://hub.invalid/datasets/swe-bench/swe-bench-verified",
  dataset_digest: `sha256:${"d".repeat(64)}`, dataset_revision: "offline-revision", resolved_at: "2026-01-01T00:00:00Z", resolution_hash: "offline-source-hash",
  tasks: [{ name: "swe-bench/offline-task", kind: "package", package_name: "swe-bench/offline-task", ref: `sha256:${"a".repeat(64)}`, revision: "offline-task-revision" }],
};
export const testSummary: RequestSummary = { request_count: 1, inference_count: 1, unknown_usage_count: 0, unknown_cost_count: 1, input_tokens: 12, output_tokens: 4, cache_read_tokens: 0, cache_write_tokens: 0, reasoning_tokens: 0, costs: [] };

export function runFixture(overrides: Partial<Run> = {}): Run {
  return { id: "run_test", state: "draft", created_at: "2026-01-01T00:00:00Z", started_at: null, finished_at: null, tasks: [], summary: { ...testSummary }, snapshot: {
    spec: { environment_id: "env_test", model_id: "model_test", suite_id: "swebench-verified", task_names: ["swe-bench/offline-task"], agent: "test-agent", agent_kwargs: {}, timeout_sec: 600, setup_timeout_sec: 300, max_requests: 5, max_output_tokens: 128, concurrency: 1, attempts: 1, confirm_paid: false },
    environment: testEnvironment, model: testModel, provider: testProvider, suite: { ...testResolution.suite }, harbor_version: "0.40.0", uv_version: "0.9.0", agent: testAgent, config_hash: "offline-test-hash", format_version: 2,
    registry_kind: testResolution.registry_kind, registry_metadata_url: testResolution.registry_metadata_url, dataset_digest: testResolution.dataset_digest, dataset_revision: testResolution.dataset_revision, resolved_at: testResolution.resolved_at, resolution_hash: testResolution.resolution_hash, resolved_tasks: testResolution.tasks.map(task => ({ ...task })),
    preflight_tools: [{ name: "harbor", available: true, version: "0.40.0", version_output: "Harbor 0.40.0" }, { name: "uv", available: true, version: "0.9.0", version_output: "uv 0.9.0" }],
  }, ...overrides };
}

export function attemptFixture(overrides: Partial<Attempt> = {}): Attempt {
  return { id: "attempt_test", task_id: "task_test", run_id: "run_test", state: "completed", job_name: "offline-job", trial_name: "offline-trial", started_at: null, finished_at: null, reward: null, exit_code: 0, image_digests: [], ...overrides };
}

export function requestFixture(overrides: Partial<RequestRecord> = {}): RequestRecord {
  return { id: "req_test", run_id: "run_test", task_id: "task_test", attempt_id: "attempt_test", provider_id: "provider_test", model_id: "model_test", protocol: "chat_completions", kind: "inference", requested_model: "offline-model", actual_model: "offline-model", started_at: "2026-01-01T00:00:00Z", finished_at: null, duration_ms: null, first_byte_ms: null, http_status: null, state: "interrupted", usage: { input_tokens: null, output_tokens: null, cache_read_tokens: null, cache_write_tokens: null, reasoning_tokens: null, total_tokens: null, source: "unknown", completeness: "unknown" }, cost_amount: null, currency: null, cost_source: "unknown", ...overrides };
}
