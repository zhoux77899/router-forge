import { expect, test } from "bun:test";
import { readSidebarPreference, writeSidebarPreference, sidebarPreferenceKey } from "./sidebar-preference";

test("sidebar preference restores only a non-sensitive boolean and tolerates unavailable storage", () => {
  const keys: string[] = [];
  expect(readSidebarPreference({ getItem: key => { keys.push(key); return "false"; } })).toBe(false);
  expect(keys).toEqual([sidebarPreferenceKey]);
  expect(readSidebarPreference({ getItem: () => "true" }, false)).toBe(true);
  for (const saved of [null, "garbage"]) expect(readSidebarPreference({ getItem: () => saved }, false)).toBe(false);
  expect(readSidebarPreference(null)).toBe(true);
  expect(readSidebarPreference({ getItem: () => { throw Error("blocked"); } })).toBe(true);
});

test("sidebar preference writes only expanded state, not mobile overlay or application form data", () => {
  const writes: [string, string][] = [];
  writeSidebarPreference({ setItem: (key, value) => { writes.push([key, value]); } }, false);
  expect(writes).toEqual([[sidebarPreferenceKey, "false"]]);
  expect(() => writeSidebarPreference({ setItem: () => { throw Error("blocked"); } }, true)).not.toThrow();
});
