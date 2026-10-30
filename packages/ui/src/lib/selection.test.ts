import { expect, test } from "bun:test";
import { attachRunID, findAttachRun } from "./selection";
import { runFixture } from "./fixtures.test-support";

test("reopened UI attaches to an active persisted run without starting it", () => {
  expect(attachRunID([runFixture({ id: "draft_newest" }), runFixture({ id: "active_old", state: "running" })], null)).toBe("active_old");
  expect(attachRunID([runFixture({ id: "another" })], "selected_previous")).toBe("selected_previous");
  expect(attachRunID([], null)).toBeNull();
});

test("attachment locates an older active run beyond the first results page", async () => {
  const calls: number[] = [];
  const id = await findAttachRun({ runs: async (_limit, offset = 0) => {
    calls.push(offset);
    return { total: 101, items: offset === 0 ? [runFixture({ id: "new_draft" })] : [runFixture({ id: "older_active", state: "running" })] };
  } });
  expect(id).toBe("older_active"); expect(calls).toEqual([0, 100]);
});
