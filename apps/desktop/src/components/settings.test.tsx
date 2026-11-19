import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { AppearancePanel } from "./settings";
import type { DesktopFontSettings } from "../lib/appearance";

function fontSettings(overrides: Partial<DesktopFontSettings> = {}): DesktopFontSettings {
  return { families: [{ name: "Arial", monospaced: false }, { name: "Consolas", monospaced: true }], preferences: { interface: "Arial", code: "Consolas" }, invalid: {}, loading: false, error: null, reload: () => {}, onSelect: () => {}, ...overrides };
}

test("Desktop appearance keeps independent font selectors with monospaced code eligibility and no footer hint", () => {
  const html = renderToStaticMarkup(<AppearancePanel theme="system" onTheme={() => {}} fonts={fontSettings()} />);
  expect(html.match(/role="tablist"/g)).toHaveLength(1); expect(html.match(/role="combobox"/g)).toHaveLength(2);
  for (const kind of ["interface", "code"]) { expect(html).toContain(`for="${kind}-font"`); expect(html).toContain(`id="${kind}-font"`); }
  expect(html).toContain("界面字体"); expect(html).toContain("代码字体"); expect(html).not.toContain("重启应用");
  expect(html).not.toContain('data-slot="card-footer"'); expect(html).not.toContain("选择后立即生效");
  const source = readFileSync(join(import.meta.dir, "settings.tsx"), "utf8");
  expect(source).toContain("fontFamiliesForPreference(kind, fonts.families)"); expect(source).not.toContain("Geist Mono");
  expect(html.match(/<button\b[^>]*id="code-font"[^>]*>/)?.[0]).toContain('title="Consolas"');
});

test("missing, malformed, and proportional code preferences silently display the system fallback", () => {
  const fonts = fontSettings({ preferences: { interface: "Removed Font", code: "Arial" }, invalid: { interface: true } });
  const html = renderToStaticMarkup(<AppearancePanel theme="light" onTheme={() => {}} fonts={fonts} />);
  for (const kind of ["interface", "code"]) expect(html.match(new RegExp(`<button\\b[^>]*id="${kind}-font"[^>]*>`))?.[0]).toContain('title="系统默认"');
  expect(html).not.toContain("Removed Font"); expect(html).not.toContain("未安装"); expect(html).not.toContain("设置无效");
  expect(html).not.toContain('role="status"'); expect(html).not.toContain('role="alert"'); expect(html).not.toMatch(/font-fallback|aria-describedby/);
});

test("font enumeration failure silently leaves theme controls and the usable system defaults", () => {
  const html = renderToStaticMarkup(<AppearancePanel theme="dark" onTheme={() => {}} fonts={fontSettings({ families: [], error: new Error("Font service unavailable") })} />);
  expect(html).not.toContain('role="alert"'); expect(html).not.toContain("无法读取系统字体"); expect(html).not.toContain("Font service unavailable"); expect(html).not.toContain("重新读取");
  expect(html).toContain('role="tablist"'); expect(html.match(/role="combobox"/g)).toHaveLength(2); expect(html).not.toContain('disabled=""');
  expect(html.match(/title="系统默认"/g)).toHaveLength(2);
});
