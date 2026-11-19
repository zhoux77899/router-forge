export type ThemePreference = "light" | "dark" | "system";
export type Theme = Exclude<ThemePreference, "system">;
export type FontPreferences = { interface: string; code: string };
export type FontFamily = { name: string; monospaced: boolean };

export const themePreferenceKey = "router-forge-theme";
export const interfaceFontPreferenceKey = "router-forge-interface-font";
export const codeFontPreferenceKey = "router-forge-code-font";
export const systemFontPreference = "system";
export const systemSansFontStack = '"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei", system-ui, sans-serif';
export const systemMonoFontStack = '"Cascadia Code", Consolas, ui-monospace, monospace';

export function appearanceStorage(): Storage | null {
  try { return typeof window === "undefined" ? null : window.localStorage ?? null; }
  catch { return null; }
}

export function isThemePreference(value: unknown): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}

export function readThemePreference(storage: Pick<Storage, "getItem"> | null): ThemePreference {
  try {
    const saved = storage?.getItem(themePreferenceKey);
    if (isThemePreference(saved)) return saved;
  } catch { /* Custom webview origins may not provide storage. */ }
  return "system";
}

export function writeThemePreference(storage: Pick<Storage, "setItem"> | null, preference: ThemePreference): void {
  if (!isThemePreference(preference)) return;
  try { storage?.setItem(themePreferenceKey, preference); }
  catch { /* The current window still keeps its in-memory preference. */ }
}

export function resolveTheme(preference: ThemePreference, systemDark: boolean): Theme {
  return preference === "system" ? systemDark ? "dark" : "light" : preference;
}

export function systemThemeDark(): boolean {
  return typeof window !== "undefined" && Boolean(window.matchMedia?.("(prefers-color-scheme: dark)").matches);
}

export function applyTheme(theme: Theme, root: {
  classList: Pick<DOMTokenList, "toggle">;
  style: Pick<CSSStyleDeclaration, "colorScheme">;
}): void {
  root.classList.toggle("dark", theme === "dark");
  root.style.colorScheme = theme;
}

export function initializeAppearance(): ThemePreference {
  const preference = readThemePreference(appearanceStorage());
  if (typeof document !== "undefined") applyTheme(resolveTheme(preference, systemThemeDark()), document.documentElement);
  return preference;
}

export function subscribeSystemTheme(media: MediaQueryList, onChange: (dark: boolean) => void): () => void {
  const listener = (event: MediaQueryListEvent) => onChange(event.matches);
  if (typeof media.addEventListener === "function") {
    media.addEventListener("change", listener);
    return () => media.removeEventListener("change", listener);
  }
  if (typeof media.addListener === "function") {
    media.addListener(listener);
    return () => media.removeListener(listener);
  }
  return () => {};
}

export function isFontFamily(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 256 && value === value.trim() && !/[\u0000-\u001f\u007f]/u.test(value);
}

export function loadFontPreferences(storage: Pick<Storage, "getItem"> | null): {
  preferences: FontPreferences; invalid: Partial<Record<keyof FontPreferences, boolean>>;
} {
  const invalid: Partial<Record<keyof FontPreferences, boolean>> = {};
  function read(kind: keyof FontPreferences, key: string): string {
    try {
      const value = storage?.getItem(key);
      if (isFontFamily(value)) return value;
      if (value != null) invalid[kind] = true;
    } catch { /* Font preferences are optional when storage is unavailable. */ }
    return systemFontPreference;
  }
  return { preferences: { interface: read("interface", interfaceFontPreferenceKey), code: read("code", codeFontPreferenceKey) }, invalid };
}

export function readFontPreferences(storage: Pick<Storage, "getItem"> | null): FontPreferences {
  return loadFontPreferences(storage).preferences;
}

export function writeFontPreference(storage: Pick<Storage, "setItem"> | null, kind: keyof FontPreferences, family: string): void {
  if (!isFontFamily(family)) return;
  const key = kind === "interface" ? interfaceFontPreferenceKey : codeFontPreferenceKey;
  try { storage?.setItem(key, family); }
  catch { /* Selecting a font still works until the window is closed. */ }
}

export function fontFamiliesForPreference(kind: keyof FontPreferences, families: readonly FontFamily[]): readonly FontFamily[] {
  return kind === "code" ? families.filter(family => family.monospaced) : families;
}

export function availableFontPreference(preference: string, families: readonly FontFamily[], kind: keyof FontPreferences = "interface"): string {
  if (!isFontFamily(preference) || preference === systemFontPreference) return systemFontPreference;
  return fontFamiliesForPreference(kind, families).find(family => family.name.toLowerCase() === preference.toLowerCase())?.name ?? systemFontPreference;
}

export function quoteFontFamily(family: string): string {
  return `"${family.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

export function applyFontPreferences(preferences: FontPreferences, families: readonly FontFamily[], root: {
  style: Pick<CSSStyleDeclaration, "setProperty" | "removeProperty">;
}): void {
  for (const kind of ["interface", "code"] as const) {
    const family = availableFontPreference(preferences[kind], families, kind);
    const property = kind === "interface" ? "--app-font-sans" : "--app-font-mono";
    if (family === systemFontPreference) root.style.removeProperty(property);
    else root.style.setProperty(property, `${quoteFontFamily(family)}, ${kind === "interface" ? systemSansFontStack : systemMonoFontStack}`);
  }
}
