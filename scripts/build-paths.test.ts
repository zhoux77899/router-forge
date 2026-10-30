import { describe, expect, it } from "bun:test";
import { join, resolve } from "node:path";
import desktop from "../apps/desktop/mygo.config";
import desktopPackage from "../apps/desktop/package.json";
import renderer from "../apps/desktop/vite.config";
import web from "../apps/web/vite.config";

const root = resolve(import.meta.dir, "..");
const build = join(root, "build");
const desktopRoot = join(root, "apps", "desktop");
const webRoot = join(root, "apps", "web");

describe("repository-root build outputs", () => {
  it("prepares helpers before MyGo starts waiting for the renderer", () => {
    expect(desktopPackage.scripts.dev).toBe("bun run prepare:helpers && mygo dev");
    expect(desktop.devCommand).toBe("bun run dev:renderer");
  });

  it("excludes native build resources from the renderer file watcher", () => {
    expect(renderer.server?.watch?.ignored).toContain("**/.mygo/**");
  });

  it("uses distinct nearby development and preview ports", () => {
    expect(renderer.server?.port).toBe(7789);
    expect(desktop.devUrl).toBe("http://127.0.0.1:7789");
    expect(web.server?.port).toBe(7790);
    expect(renderer.preview?.port).toBe(7791);
    expect(web.preview?.port).toBe(7792);
  });

  it("builds and cleans only the dedicated Web output", () => {
    expect(resolve(webRoot, web.build?.outDir ?? "")).toBe(join(build, "dist"));
    expect(web.build?.emptyOutDir).toBe(true);
    expect(web.build?.sourcemap).toBe(false);
  });

  it("uses the same dedicated renderer directory for Vite and native embedding", () => {
    expect(resolve(desktopRoot, renderer.build?.outDir ?? "")).toBe(join(build, "renderer"));
    expect(resolve(desktopRoot, desktop.frontendDist ?? "")).toBe(join(build, "renderer"));
    expect(renderer.build?.emptyOutDir).toBe(true);
    expect(renderer.build?.sourcemap).toBe(false);
  });

  it("packages native releases and read-only Web resources from the root build tree", () => {
    expect(resolve(desktopRoot, desktop.out ?? "")).toBe(join(build, "desktop"));
    expect(resolve(desktopRoot, desktop.resources?.at(-1) ?? "")).toBe(join(build, "dist"));
  });
});
