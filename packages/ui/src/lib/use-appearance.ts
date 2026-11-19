"use client";

import { useEffect, useState } from "react";
import { appearanceStorage, applyTheme, isThemePreference, readThemePreference, resolveTheme, subscribeSystemTheme, systemThemeDark, writeThemePreference, type ThemePreference } from "./appearance";

export function useAppearance() {
  const [preference, setPreference] = useState(() => readThemePreference(appearanceStorage()));
  const [systemDark, setSystemDark] = useState(systemThemeDark);
  const theme = resolveTheme(preference, systemDark);

  useEffect(() => {
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!media) return;
    setSystemDark(media.matches);
    return subscribeSystemTheme(media, setSystemDark);
  }, []);

  useEffect(() => { applyTheme(theme, document.documentElement); }, [theme]);

  function changePreference(next: ThemePreference) {
    if (!isThemePreference(next)) return;
    writeThemePreference(appearanceStorage(), next);
    setPreference(next);
  }

  return { preference, theme, setPreference: changePreference };
}
