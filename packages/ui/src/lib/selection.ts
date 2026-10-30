import type { Run } from "@router-forge/api";
import { activeRunStates } from "@router-forge/api";
import type { ResultsClient } from "./read-client";

export async function findAttachRun(client: Pick<ResultsClient, "runs">): Promise<string | null> {
  let first: string | null = null;
  for (let offset = 0; ; offset += 100) {
    const page = await client.runs(100, offset);
    const items = page.items ?? [];
    first ??= items[0]?.id ?? null;
    const active = items.find(run => activeRunStates.has(run.state));
    if (active) return active.id;
    if (items.length === 0 || offset + 100 >= page.total) return first;
  }
}

export function attachRunID(runs: readonly Run[], selected: string | null): string | null {
  if (selected) return selected;
  return runs.find(run => activeRunStates.has(run.state))?.id ?? runs[0]?.id ?? null;
}

export function runIDFromLocation(): string | null {
  if (typeof window === "undefined") return null;
  const value = new URLSearchParams(window.location.search).get("run");
  return value && /^[A-Za-z0-9_-]{1,128}$/.test(value) ? value : null;
}

export function rememberRunInLocation(id: string): void {
  try {
    const url = new URL(window.location.href);
    url.searchParams.set("run", id);
    window.history.replaceState(null, "", url);
  } catch { /* MyGo custom origins may not support history replacement. */ }
}
