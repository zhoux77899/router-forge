import { activeRunStates, type APIClient } from "@router-forge/api";

export async function countRunningRuns(client: Pick<APIClient, "runs">): Promise<number> {
  let count = 0;
  for (let offset = 0; ; offset += 100) {
    const page = await client.runs(100, offset);
    count += page.items.filter(run => activeRunStates.has(run.state)).length;
    if (page.items.length === 0 || offset + 100 >= page.total) return count;
  }
}
