import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import { RiLayoutLeft2Line, RiLayoutLeftLine } from "@remixicon/react";
import { PageFrame } from "./page-frame";
import { ThemeSelector } from "./theme-selector";
import { Sidebar, SidebarProvider, SidebarTrigger } from "./ui/sidebar";

test("theme selection exposes three keyboard-accessible shadcn tabs without changing state on render", () => {
  let changes = 0;
  for (const value of ["light", "dark", "system"] as const) {
    const html = renderToStaticMarkup(<ThemeSelector value={value} onValueChange={() => { changes++; }} />);
    expect(html).toContain('role="tablist"'); expect(html).toContain('aria-label="主题模式"');
    expect(html.match(/role="tab"/g)).toHaveLength(3); expect(html.match(/aria-selected="true"/g)).toHaveLength(1);
    expect(html).toContain(`data-state="active"`); expect(html).toContain(`-${value}"`); expect(html).toContain('role="tabpanel"');
    for (const label of ["浅色", "深色", "跟随系统"]) expect(html).toContain(label);
    expect(html).toContain('aria-hidden="true"'); expect(html).not.toContain("aria-pressed");
  }
  expect(changes).toBe(0);
});

test("page title stays outside the keyboard-accessible vertical content viewport", () => {
  const html = renderToStaticMarkup(<PageFrame title="结果" leading={<button>Navigation</button>}><article>Result evidence</article></PageFrame>);
  const header = html.match(/<header\b[^>]*data-slot="page-header"[^>]*>([\s\S]*?)<\/header>/)?.[1];
  expect(header).toContain("<h1"); expect(header).toContain("结果"); expect(header).toContain("Navigation"); expect(header).not.toContain("Result evidence");
  expect(html.indexOf('data-slot="page-header"')).toBeLessThan(html.indexOf('data-slot="scroll-area-viewport"'));
  expect(html).toContain('aria-label="结果内容"'); expect(html).toContain('role="region"'); expect(html).toContain('tabindex="0"');
  expect(html).toContain("@container/workspace"); expect(html).toContain("Result evidence");
  const source = readFileSync(join(import.meta.dir, "ui", "scroll-area.tsx"), "utf8");
  expect(source).toContain('orientation = "vertical"'); expect(source).toContain('"h-full w-1.5 p-px"'); expect(source).not.toContain('orientation="horizontal"');
  const styles = readFileSync(join(import.meta.dir, "..", "styles.css"), "utf8");
  expect(styles).toContain('display: block !important; width: 100%; min-width: 0 !important;');
  const titlebar = styles.match(/\.desktop-titlebar\s*\{([^}]+)\}/)?.[1]; expect(titlebar).not.toContain("border-bottom");
});

test("Web code and numeric fonts retain the Chinese fallback after both mono families", () => {
  const styles = readFileSync(join(import.meta.dir, "..", "styles.css"), "utf8");
  const web = styles.match(/:root\[data-app="web"\]\s*\{([^}]+)\}/)?.[1] ?? "";
  const mono = web.match(/--app-font-mono:\s*([^;]+);/)?.[1] ?? "";
  expect(mono).toContain('"Geist Mono Variable", "Noto Sans Mono Variable", "Noto Sans SC Variable"');
});

test("sidebar state exposes the requested Remix expand and collapse glyphs and accessible state", () => {
  for (const open of [false, true]) {
    const html = renderToStaticMarkup(<SidebarProvider open={open} onOpenChange={() => {}}><SidebarTrigger aria-label="切换导航栏" /></SidebarProvider>);
    const Icon = open ? RiLayoutLeftLine : RiLayoutLeft2Line;
    const glyph = renderToStaticMarkup(<Icon aria-hidden="true" data-icon="inline-start" />);
    expect(html).toContain(glyph); expect(html).toContain(`aria-expanded="${open}"`);
    expect(html).toContain(`data-sidebar-action="${open ? "collapse" : "expand"}"`);
    expect(html.match(/data-slot="sidebar-trigger"/g)).toHaveLength(1);
  }
});

test("collapsed offcanvas navigation is inert while the Web icon rail remains interactive", () => {
  for (const collapsible of ["offcanvas", "icon"] as const) {
    for (const open of [false, true]) {
      const html = renderToStaticMarkup(<SidebarProvider open={open} onOpenChange={() => {}}><Sidebar collapsible={collapsible}><button>Navigation</button></Sidebar><SidebarTrigger /></SidebarProvider>);
      const container = html.match(/<div\b[^>]*data-slot="sidebar-container"[^>]*>/)?.[0] ?? "";
      expect(html).toContain(`data-state="${open ? "expanded" : "collapsed"}"`);
      expect(html).toContain(`data-collapsible="${open ? "" : collapsible}"`);
      expect(html).toContain("group-data-[collapsible=offcanvas]:w-0");
      expect(container).toContain("group-data-[collapsible=offcanvas]:left-[calc(var(--sidebar-width)*-1)]");
      if (!open && collapsible === "offcanvas") {
        expect(container).toContain('inert=""');
        expect(container).toContain('aria-hidden="true"');
      } else {
        expect(container).not.toContain("inert=");
        expect(container).not.toContain("aria-hidden=");
      }
      expect(html.match(/data-slot="sidebar-trigger"/g)).toHaveLength(1);
    }
  }
});
