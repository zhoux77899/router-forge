import { expect, test } from "bun:test";
import { APIClient } from "@router-forge/api";
import { resultsClient } from "./read-client";
import { runFixture } from "./fixtures.test-support";

test("public results facade exposes reads only and all wire requests are GET", async () => {
  const calls: { method: string; path: string; body: string }[] = [];
  const api = new APIClient("", "", async (method, path, body) => {
    calls.push({ method, path, body });
    const result = path === "/api/status" ? { running: true, pid: 1, version: "test" } : path.includes("/events") ? [] : path.includes("/requests") || path.startsWith("/api/runs?") ? { items: [], total: 0 } : runFixture();
    return { status: 200, body: JSON.stringify(result) };
  });
  const reader = resultsClient(api);
  expect(Object.keys(reader).sort()).toEqual(["events", "requests", "run", "runs", "status"]);
  expect(Object.isFrozen(reader)).toBe(true);
  await reader.status(); await reader.runs(20, 0); await reader.run("run_test"); await reader.events("run_test", 12); await reader.requests("run_test", 50, 0);
  expect(calls).toHaveLength(5); expect(calls.every(call => call.method === "GET" && call.body === "")).toBe(true);
  expect("startRun" in reader || "saveProvider" in reader || "cancelRun" in reader).toBe(false);
});
