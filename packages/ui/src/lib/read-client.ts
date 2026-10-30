import type { APIClient } from "@router-forge/api";

export type ResultsClient = Pick<APIClient, "status" | "runs" | "run" | "events" | "requests">;

export function resultsClient(api: APIClient): Readonly<ResultsClient> {
  return Object.freeze({
    status: () => api.status(),
    runs: (limit?: number, offset?: number) => api.runs(limit, offset),
    run: (id: string) => api.run(id),
    events: (id: string, after?: number) => api.events(id, after),
    requests: (runID?: string, limit?: number, offset?: number) => api.requests(runID, limit, offset),
  });
}
