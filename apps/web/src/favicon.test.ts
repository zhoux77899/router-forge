import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { applyTheme } from "@router-forge/ui/lib/appearance";
import { darkFaviconUrl, initializeSystemFavicon, lightFaviconUrl, subscribeSystemFavicon } from "./favicon";

function systemMedia(dark = false) {
  const listeners = new Set<(event: MediaQueryListEvent) => void>();
  const media = {
    matches: dark,
    addEventListener: (type: string, listener: (event: MediaQueryListEvent) => void) => { expect(type).toBe("change"); listeners.add(listener); },
    removeEventListener: (type: string, listener: (event: MediaQueryListEvent) => void) => { expect(type).toBe("change"); listeners.delete(listener); },
  } as MediaQueryList;
  return { media, listeners, change(matches: boolean) { for (const listener of listeners) listener({ matches } as MediaQueryListEvent); } };
}

test("transparent favicons initialize from the system and ignore manually applied page themes", () => {
  for (const dark of [false, true]) {
    const system = systemMedia(dark); const link = { href: "initial" };
    const stop = subscribeSystemFavicon(system.media, link);
    expect(link.href).toBe(dark ? darkFaviconUrl : lightFaviconUrl); expect(system.listeners.size).toBe(1);
    const root = { classList: { toggle: () => false }, style: { colorScheme: "" } };
    applyTheme(dark ? "light" : "dark", root);
    expect(link.href).toBe(dark ? darkFaviconUrl : lightFaviconUrl);
    system.change(!dark); expect(link.href).toBe(dark ? lightFaviconUrl : darkFaviconUrl);
    system.change(dark); expect(link.href).toBe(dark ? darkFaviconUrl : lightFaviconUrl);
    stop(); expect(system.listeners.size).toBe(0);
    system.change(!dark); expect(link.href).toBe(dark ? darkFaviconUrl : lightFaviconUrl);
  }
});

test("favicon startup handles a missing icon or unavailable media queries without reading preferences", () => {
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const originalDocument = Object.getOwnPropertyDescriptor(globalThis, "document");
  try {
    const link = { href: "initial" }; let queries = 0;
    Object.defineProperty(globalThis, "document", { configurable: true, value: { querySelector: () => link } });
    Object.defineProperty(globalThis, "window", { configurable: true, value: {} });
    initializeSystemFavicon()(); expect(link.href).toBe(lightFaviconUrl);
    const system = systemMedia(true);
    Object.defineProperty(globalThis, "window", { configurable: true, value: { matchMedia: (query: string) => { queries++; expect(query).toBe("(prefers-color-scheme: dark)"); return system.media; } } });
    const stop = initializeSystemFavicon(); expect(link.href).toBe(darkFaviconUrl); expect(queries).toBe(1);
    stop(); expect(system.listeners.size).toBe(0);
    Object.defineProperty(globalThis, "document", { configurable: true, value: { querySelector: () => null } });
    initializeSystemFavicon()(); expect(queries).toBe(1);
  } finally {
    for (const [key, descriptor] of [["window", originalWindow], ["document", originalDocument]] as const) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  }
  const source = readFileSync(join(import.meta.dir, "favicon.ts"), "utf8");
  expect(source).not.toMatch(/localStorage|sessionStorage|readThemePreference|mygo|APIClient/);
});

test("Web initializes a source-derived system favicon and cleans up the listener on hot reload", () => {
  const html = readFileSync(join(import.meta.dir, "..", "index.html"), "utf8");
  const main = readFileSync(join(import.meta.dir, "main.tsx"), "utf8");
  expect(html).toContain('rel="icon" type="image/svg+xml" href="../../assets/favicon-light.svg"');
  expect(html).not.toContain('href="../../assets/icon.svg"');
  expect(main).toContain("initializeSystemFavicon()"); expect(main).toContain("import.meta.hot.dispose(stopSystemFavicon)");
  expect(lightFaviconUrl).toContain("favicon-light.svg"); expect(darkFaviconUrl).toContain("favicon-dark.svg");
});
