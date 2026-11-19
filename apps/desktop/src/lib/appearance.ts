import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { appearanceStorage, applyFontPreferences, availableFontPreference, loadFontPreferences, systemFontPreference, writeFontPreference, type FontFamily, type FontPreferences, type ThemePreference } from "@router-forge/ui/lib/appearance";
import { Appearance } from "../mygo.generated";

export type DesktopFontSettings = {
  families: FontFamily[]; preferences: FontPreferences;
  invalid: Partial<Record<keyof FontPreferences, boolean>>;
  loading: boolean; error: unknown; reload: () => void;
  onSelect: (kind: keyof FontPreferences, family: string) => void;
};

export function createThemeSynchronizer(setTheme: (preference: ThemePreference) => Promise<void>) {
  let pending = Promise.resolve();
  return (preference: ThemePreference) => {
    const next = pending.catch(() => {}).then(() => setTheme(preference));
    pending = next;
    return next;
  };
}

export function useNativeTheme(native: boolean, preference: ThemePreference) {
  const [synchronize] = useState(() => createThemeSynchronizer(Appearance.setTheme));
  const [error, setError] = useState<unknown>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!native) return;
    let alive = true;
    void synchronize(preference).then(() => { if (alive) setError(null); }).catch(reason => { if (alive) setError(reason); });
    return () => { alive = false; };
  }, [native, preference, retry, synchronize]);
  return { error, retry: () => setRetry(value => value + 1) };
}

export function migrateCodeFontPreference(saved: ReturnType<typeof loadFontPreferences>, families: readonly FontFamily[] | undefined, storage: Pick<Storage, "setItem"> | null) {
  if (families === undefined) return saved;
  const code = availableFontPreference(saved.preferences.code, families, "code");
  if (code === saved.preferences.code && !saved.invalid.code) return saved;
  writeFontPreference(storage, "code", code);
  return { preferences: { ...saved.preferences, code }, invalid: { ...saved.invalid, code: false } };
}

export function useDesktopFonts(native: boolean): DesktopFontSettings {
  const [saved, setSaved] = useState(() => loadFontPreferences(appearanceStorage()));
  const fonts = useQuery({ queryKey: ["native-font-families"], queryFn: () => Appearance.fontFamilies(), enabled: native, staleTime: Infinity, retry: 1 });
  useEffect(() => {
    if (native) applyFontPreferences(saved.preferences, fonts.data ?? [], document.documentElement);
  }, [native, saved.preferences, fonts.data]);
  useEffect(() => {
    if (!native || !fonts.isSuccess) return;
    const migrated = migrateCodeFontPreference(saved, fonts.data, appearanceStorage());
    if (migrated !== saved) setSaved(migrated);
  }, [native, fonts.isSuccess, fonts.data, saved]);

  function onSelect(kind: keyof FontPreferences, family: string) {
    const available = availableFontPreference(family, fonts.data ?? [], kind);
    if (family !== systemFontPreference && available === systemFontPreference) return;
    writeFontPreference(appearanceStorage(), kind, available);
    setSaved(previous => ({ preferences: { ...previous.preferences, [kind]: available }, invalid: { ...previous.invalid, [kind]: false } }));
  }

  return { families: fonts.data ?? [], preferences: saved.preferences, invalid: saved.invalid, loading: fonts.isPending, error: fonts.error, reload: () => { void fonts.refetch(); }, onSelect };
}
