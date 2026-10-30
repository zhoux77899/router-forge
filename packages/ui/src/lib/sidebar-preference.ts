export const sidebarPreferenceKey = "router-forge-sidebar-open";

export function readSidebarPreference(storage: Pick<Storage, "getItem"> | null, fallback = true): boolean {
  try {
    const saved = storage?.getItem(sidebarPreferenceKey);
    if (saved === "true" || saved === "false") return saved === "true";
  } catch { /* Layout remains usable when storage is blocked. */ }
  return fallback;
}

export function writeSidebarPreference(storage: Pick<Storage, "setItem"> | null, open: boolean): void {
  try { storage?.setItem(sidebarPreferenceKey, String(open)); } catch { /* Keep the in-memory layout preference. */ }
}

export function sidebarStorage(): Storage | null {
  try { return typeof window === "undefined" ? null : window.localStorage; } catch { return null; }
}
