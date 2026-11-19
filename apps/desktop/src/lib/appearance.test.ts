import { expect, test } from "bun:test";
import { createThemeSynchronizer, migrateCodeFontPreference } from "./appearance";
import { codeFontPreferenceKey, interfaceFontPreferenceKey, loadFontPreferences, type ThemePreference } from "@router-forge/ui/lib/appearance";

test("native appearance calls remain in preference order during rapid changes", async () => {
  const calls: ThemePreference[] = [];
  let release!: () => void;
  const first = new Promise<void>(resolve => { release = resolve; });
  const synchronize = createThemeSynchronizer(async preference => { calls.push(preference); if (calls.length === 1) await first; });
  const light = synchronize("light"); const dark = synchronize("dark"); const system = synchronize("system");
  await Promise.resolve(); await Promise.resolve(); expect(calls).toEqual(["light"]);
  release(); await Promise.all([light, dark, system]); expect(calls).toEqual(["light", "dark", "system"]);
});

test("a rejected native update does not block a later preference or retry", async () => {
  const calls: ThemePreference[] = [];
  const synchronize = createThemeSynchronizer(async preference => { calls.push(preference); if (calls.length === 1) throw new Error("Native update failed"); });
  await expect(synchronize("light")).rejects.toThrow("Native update failed");
  await synchronize("dark"); await synchronize("dark"); expect(calls).toEqual(["light", "dark", "dark"]);
});

test("old proportional code choices migrate persistently without erasing the interface choice", () => {
  const values = new Map([[interfaceFontPreferenceKey, "Arial"], [codeFontPreferenceKey, "Arial"]]);
  const writes: [string, string][] = [];
  const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); writes.push([key, value]); } };
  const families = [{ name: "Arial", monospaced: false }, { name: "Consolas", monospaced: true }];
  const migrated = migrateCodeFontPreference(loadFontPreferences(storage), families, storage);
  expect(migrated.preferences).toEqual({ interface: "Arial", code: "system" });
  expect(writes).toEqual([[codeFontPreferenceKey, "system"]]);
  expect(loadFontPreferences(storage).preferences).toEqual({ interface: "Arial", code: "system" });
  expect(migrateCodeFontPreference(migrated, families, storage)).toBe(migrated); expect(writes).toHaveLength(1);
});

test("unknown font catalogs preserve saved mono choices and successful catalogs retain valid choices", () => {
  const saved = { preferences: { interface: "Arial", code: "Consolas" }, invalid: {} };
  const writes: [string, string][] = []; const storage = { setItem: (key: string, value: string) => { writes.push([key, value]); } };
  expect(migrateCodeFontPreference(saved, undefined, storage)).toBe(saved); expect(writes).toEqual([]);
  expect(migrateCodeFontPreference(saved, [{ name: "Consolas", monospaced: true }], storage)).toBe(saved); expect(writes).toEqual([]);
  expect(migrateCodeFontPreference(saved, [], storage).preferences.code).toBe("system"); expect(writes).toEqual([[codeFontPreferenceKey, "system"]]);
});

test("malformed code preferences migrate silently and blocked storage still permits the default", () => {
  const saved = loadFontPreferences({ getItem: key => key === codeFontPreferenceKey ? "A\u0000B" : "Arial" });
  const writes: [string, string][] = [];
  const migrated = migrateCodeFontPreference(saved, [], { setItem: (key, value) => { writes.push([key, value]); } });
  expect(migrated.preferences).toEqual({ interface: "Arial", code: "system" }); expect(migrated.invalid.code).toBe(false);
  expect(writes).toEqual([[codeFontPreferenceKey, "system"]]);
  const proportional = { preferences: { interface: "Arial", code: "Arial" }, invalid: {} };
  expect(migrateCodeFontPreference(proportional, [{ name: "Arial", monospaced: false }], { setItem: () => { throw new Error("Storage blocked"); } }).preferences.code).toBe("system");
});
