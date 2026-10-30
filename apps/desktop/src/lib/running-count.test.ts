import { expect, test } from "bun:test";
import { countRunningRuns } from "./running-count";
import { runFixture } from "@router-forge/ui/lib/fixtures.test-support";

test("evaluation badge counts active runs beyond the first results page", async () => {
  const offsets: number[] = [];
  const client = { runs: async (_limit = 100, offset = 0) => {
    offsets.push(offset);
    return { total: 101, items: offset === 0 ? Array.from({ length: 100 }, (_, index) => runFixture({ state: index === 0 ? "running" : "completed" })) : [runFixture({ state: "running" })] };
  } };
  expect(await countRunningRuns(client)).toBe(2);
  expect(offsets).toEqual([0, 100]);
});
