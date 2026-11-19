import { expect, test } from "bun:test";
import { applyFontPreferences, applyTheme, availableFontPreference, codeFontPreferenceKey, fontFamiliesForPreference, interfaceFontPreferenceKey, isFontFamily, loadFontPreferences, quoteFontFamily, readFontPreferences, readThemePreference, resolveTheme, subscribeSystemTheme, themePreferenceKey, writeFontPreference, writeThemePreference } from "./appearance";

function themeRoot() {
  const classes = new Set(["existing-class"]);
  return { classes, classList: { toggle(token: string, force?: boolean) { const enabled = force ?? !classes.has(token); if (enabled) classes.add(token); else classes.delete(token); return enabled; } }, style: { colorScheme: "" } };
}

test("appearance keeps light/dark legacy preferences and defaults to system without reading sensitive keys", () => {
  const keys: string[] = [];
  expect(readThemePreference({ getItem: key => { keys.push(key); return "dark"; } })).toBe("dark");
  expect(keys).toEqual([themePreferenceKey]);
  for (const preference of ["light", "dark", "system"] as const) expect(readThemePreference({ getItem: () => preference })).toBe(preference);
  for (const preference of [null, "unrecognized-value"]) expect(readThemePreference({ getItem: () => preference })).toBe("system");
  expect(readThemePreference(null)).toBe("system");
  expect(readThemePreference({ getItem: () => { throw new Error("Storage blocked"); } })).toBe("system");
});

test("system resolution changes with the OS while explicit modes ignore OS changes", () => {
  expect(resolveTheme("system", true)).toBe("dark"); expect(resolveTheme("system", false)).toBe("light");
  for (const systemDark of [false, true]) { expect(resolveTheme("light", systemDark)).toBe("light"); expect(resolveTheme("dark", systemDark)).toBe("dark"); }
});

test("applying a resolved mode updates color-scheme without persisting system resolution", () => {
  const root = themeRoot(); const writes: [string, string][] = [];
  const storage = { setItem: (key: string, value: string) => { writes.push([key, value]); } };
  writeThemePreference(storage, "system");
  applyTheme(resolveTheme("system", true), root); expect(root.classes.has("dark")).toBe(true); expect(root.style.colorScheme).toBe("dark");
  applyTheme(resolveTheme("system", false), root); expect(root.classes.has("dark")).toBe(false); expect(root.style.colorScheme).toBe("light");
  expect(root.classes.has("existing-class")).toBe(true); expect(writes).toEqual([[themePreferenceKey, "system"]]);
  for (const preference of ["light", "dark"] as const) writeThemePreference(storage, preference);
  expect(writes).toEqual([[themePreferenceKey, "system"], [themePreferenceKey, "light"], [themePreferenceKey, "dark"]]);
  expect(() => writeThemePreference({ setItem: () => { throw new Error("Storage blocked"); } }, "light")).not.toThrow();
});

test("system appearance subscribes once and cleans up its media-query listener", () => {
  const listeners = new Set<(event: MediaQueryListEvent) => void>(); const seen: boolean[] = [];
  const media = { addEventListener: (event: string, listener: (event: MediaQueryListEvent) => void) => { expect(event).toBe("change"); listeners.add(listener); }, removeEventListener: (event: string, listener: (event: MediaQueryListEvent) => void) => { expect(event).toBe("change"); listeners.delete(listener); } };
  const unsubscribe = subscribeSystemTheme(media as MediaQueryList, dark => seen.push(dark));
  expect(listeners.size).toBe(1);
  for (const matches of [true, false]) for (const listener of listeners) listener({ matches } as MediaQueryListEvent);
  unsubscribe(); expect(listeners.size).toBe(0); expect(seen).toEqual([true, false]);
});

test("font preferences store only the two family names and tolerate unavailable storage", () => {
  const keys: string[] = []; const writes: [string, string][] = [];
  const storage = { getItem: (key: string) => { keys.push(key); return key === interfaceFontPreferenceKey ? "Segoe UI" : "Consolas"; }, setItem: (key: string, value: string) => { writes.push([key, value]); } };
  expect(readFontPreferences(storage)).toEqual({ interface: "Segoe UI", code: "Consolas" }); expect(keys).toEqual([interfaceFontPreferenceKey, codeFontPreferenceKey]);
  writeFontPreference(storage, "interface", "Arial"); writeFontPreference(storage, "code", "Segoe UI");
  expect(writes).toEqual([[interfaceFontPreferenceKey, "Arial"], [codeFontPreferenceKey, "Segoe UI"]]);
  expect(readFontPreferences(null)).toEqual({ interface: "system", code: "system" });
  expect(readFontPreferences({ getItem: () => { throw new Error("Storage blocked"); } })).toEqual({ interface: "system", code: "system" });
  expect(() => writeFontPreference({ setItem: () => { throw new Error("Storage blocked"); } }, "code", "Arial")).not.toThrow();
});

test("uninstalled or malformed fonts fall back without erasing a recoverable preference", () => {
  const families = [{ name: "Segoe UI", monospaced: false }, { name: "Consolas", monospaced: true }];
  expect(availableFontPreference("segoe ui", families)).toBe("Segoe UI"); expect(availableFontPreference("Removed Font", families)).toBe("system");
  for (const invalid of ["", "  Arial", "Arial\n", "A\u0000B", "A".repeat(257)]) { expect(isFontFamily(invalid)).toBe(false); expect(availableFontPreference(invalid, families)).toBe("system"); }
  expect(readFontPreferences({ getItem: () => "Removed Font" }).code).toBe("Removed Font");
  expect(loadFontPreferences({ getItem: () => "A\u0000B" })).toEqual({ preferences: { interface: "system", code: "system" }, invalid: { interface: true, code: true } });
});

test("font variables are independent, quoted safely, and restore the platform fallback", () => {
  const properties = new Map<string, string>(); const root = { style: { setProperty: (key: string, value: string) => { properties.set(key, value); }, removeProperty: (key: string) => { const previous = properties.get(key) ?? ""; properties.delete(key); return previous; } } };
  const quoted = 'A"\\B'; const families = [{ name: "Arial", monospaced: false }, { name: quoted, monospaced: true }];
  applyFontPreferences({ interface: "Arial", code: quoted }, families, root);
  expect(properties.get("--app-font-sans")).toStartWith('"Arial", '); expect(properties.get("--app-font-mono")).toStartWith(quoteFontFamily(quoted) + ", ");
  expect(quoteFontFamily(quoted)).toBe('"A\\"\\\\B"');
  applyFontPreferences({ interface: "Removed Font", code: "system" }, families, root); expect(properties.size).toBe(0);
});

test("only code font lists and resolutions restrict installed families to monospaced faces", () => {
  const families = [{ name: "Arial", monospaced: false }, { name: "Consolas", monospaced: true }, { name: "Maple Mono", monospaced: true }];
  expect(fontFamiliesForPreference("interface", families)).toBe(families);
  expect(fontFamiliesForPreference("code", families).map(family => family.name)).toEqual(["Consolas", "Maple Mono"]);
  expect(availableFontPreference("Arial", families, "interface")).toBe("Arial");
  expect(availableFontPreference("Arial", families, "code")).toBe("system");
  expect(availableFontPreference("consolas", families, "code")).toBe("Consolas");
  expect(availableFontPreference("Removed Font", families, "code")).toBe("system");
  expect(fontFamiliesForPreference("code", [])).toEqual([]);
});

test("applying proportional code preferences restores the mono fallback without changing the interface face", () => {
  const properties = new Map<string, string>();
  const root = { style: { setProperty: (key: string, value: string) => { properties.set(key, value); }, removeProperty: (key: string) => { properties.delete(key); return ""; } } };
  const families = [{ name: "Arial", monospaced: false }, { name: "Consolas", monospaced: true }];
  applyFontPreferences({ interface: "Arial", code: "Consolas" }, families, root);
  expect(properties.get("--app-font-mono")).toStartWith('"Consolas", ');
  applyFontPreferences({ interface: "Arial", code: "Arial" }, families, root);
  expect(properties.get("--app-font-sans")).toStartWith('"Arial", '); expect(properties.has("--app-font-mono")).toBe(false);
});
