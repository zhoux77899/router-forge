import type { components } from "./schema.generated";

export type Protocol = components["schemas"]["Protocol"];
export type Environment = components["schemas"]["Environment"];
export type EnvironmentInput = components["schemas"]["EnvironmentInput"];
export type Inspection = components["schemas"]["Inspection"];
export type Agent = components["schemas"]["Agent"];
export type Provider = components["schemas"]["Provider"];
export type ProviderInput = components["schemas"]["ProviderInput"];
export type Model = components["schemas"]["Model"];
export type ModelInput = components["schemas"]["ModelInput"];
export type Pricing = components["schemas"]["Pricing"];
export type Suite = components["schemas"]["Suite"];
export type BenchmarkTask = components["schemas"]["BenchmarkTask"];
export type ResolvedBenchmark = components["schemas"]["ResolvedBenchmark"];
export type RunSnapshot = components["schemas"]["RunSnapshot"];
export type RuntimeSnapshot = components["schemas"]["RuntimeSnapshot"];
export type RunSpec = components["schemas"]["RunSpec"];
export type Run = components["schemas"]["Run"];
export type JobEvent = components["schemas"]["JobEvent"];
export type Attempt = components["schemas"]["Attempt"];
export type RequestRecord = components["schemas"]["RequestRecord"];
export type RequestSummary = components["schemas"]["RequestSummary"];
export type NetworkProfile = components["schemas"]["NetworkProfile"];
export type NetworkProfileInput = components["schemas"]["NetworkProfileInput"];
export type NetworkSource = components["schemas"]["NetworkSource"];
export type NetworkMirrors = components["schemas"]["NetworkMirrors"];
export type EnvironmentNetworks = components["schemas"]["EnvironmentNetworks"];
export type NetworkSnapshot = components["schemas"]["NetworkSnapshot"];
export type NetworkCheck = components["schemas"]["NetworkCheck"];
export type NetworkCheckInput = components["schemas"]["NetworkCheckInput"];
export type StartInput = components["schemas"]["StartInput"];
export type Page<T> = { items: T[]; total: number };

export class APIError extends Error {
  constructor(public readonly status: number, message: string) { super(message); this.name = "APIError"; }
}

export type NativeRequest = (method: string, path: string, body: string) => Promise<{ status: number; body: string }>;

export class APIClient {
  constructor(public readonly baseURL = "", private readonly controlToken = "", private readonly nativeRequest?: NativeRequest) {}

  async request<T>(method: string, path: string, data?: unknown): Promise<T> {
    if (!path.startsWith("/api/") || path.includes("..") || path.includes("\\")) throw new Error("Invalid API path");
    const body = data === undefined ? "" : JSON.stringify(data);
    let status: number;
    let text: string;
    if (this.nativeRequest) {
      const result = await this.nativeRequest(method, path, body);
      status = result.status;
      text = result.body;
    } else {
      const result = await fetch(`${this.baseURL.replace(/\/$/, "")}${path}`, {
        method,
        headers: { ...(body ? { "Content-Type": "application/json" } : {}), ...(this.controlToken ? { Authorization: `Bearer ${this.controlToken}` } : {}) },
        body: body || undefined,
        credentials: "omit",
      });
      status = result.status;
      text = await result.text();
    }
    let parsed: unknown;
    try { parsed = text ? JSON.parse(text) : null; }
    catch { throw new APIError(status, "The backend returned an invalid response"); }
    if (status < 200 || status >= 300) {
      const error = parsed && typeof parsed === "object" && "error" in parsed ? String(parsed.error) : `Request failed (${status})`;
      throw new APIError(status, error);
    }
    return parsed as T;
  }

  status() { return this.request<components["schemas"]["Status"]>("GET", "/api/status"); }
  environments() { return this.request<Environment[]>("GET", "/api/environments"); }
  saveEnvironment(input: EnvironmentInput) { return this.request<Environment>("POST", "/api/environments", input); }
  inspectEnvironment(id: string) { return this.request<Inspection>("POST", `/api/environments/${encodeURIComponent(id)}/inspect`, {}); }
  networkProfiles() { return this.request<NetworkProfile[]>("GET", "/api/network-profiles"); }
  saveNetworkProfile(input: NetworkProfileInput) { return this.request<NetworkProfile>("POST", "/api/network-profiles", input); }
  checkNetworkProfile(id: string, input: NetworkCheckInput) { return this.request<NetworkCheck>("POST", `/api/network-profiles/${encodeURIComponent(id)}/check`, input); }
  providers() { return this.request<Provider[]>("GET", "/api/providers"); }
  saveProvider(input: ProviderInput) { return this.request<Provider>("POST", "/api/providers", input); }
  models() { return this.request<Model[]>("GET", "/api/models"); }
  saveModel(input: ModelInput) { return this.request<Model>("POST", "/api/models", input); }
  benchmarks() { return this.request<Suite[]>("GET", "/api/benchmarks"); }
  benchmarkTasks(suiteID: string, environmentID: string) { return this.request<BenchmarkTask[]>("GET", `/api/benchmarks/${encodeURIComponent(suiteID)}/tasks?environment_id=${encodeURIComponent(environmentID)}`); }
  resolveBenchmark(suiteID: string, environmentID: string) { return this.request<ResolvedBenchmark>("GET", `/api/benchmarks/${encodeURIComponent(suiteID)}/resolve?environment_id=${encodeURIComponent(environmentID)}`); }
  runs(limit = 50, offset = 0) { return this.request<Page<Run>>("GET", `/api/runs?limit=${limit}&offset=${offset}`); }
  run(id: string) { return this.request<Run>("GET", `/api/runs/${encodeURIComponent(id)}`); }
  createRun(input: RunSpec) { return this.request<Run>("POST", "/api/runs", input); }
  startRun(id: string, input: StartInput) { return this.request<Run>("POST", `/api/runs/${encodeURIComponent(id)}/start`, input); }
  cancelRun(id: string) { return this.request<Run>("POST", `/api/runs/${encodeURIComponent(id)}/cancel`, {}); }
  events(id: string, after = 0) { return this.request<JobEvent[]>("GET", `/api/runs/${encodeURIComponent(id)}/events?after=${after}`); }
  requests(runID?: string, limit = 100, offset = 0) {
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    if (runID) params.set("run_id", runID);
    return this.request<Page<RequestRecord>>("GET", `/api/requests?${params}`);
  }
}

export const protocolLabels: Record<Protocol, string> = {
  chat_completions: "Chat Completions",
  responses: "Responses",
  anthropic_messages: "Anthropic Messages",
};
export const activeRunStates = new Set(["preparing", "running", "cancelling"]);
