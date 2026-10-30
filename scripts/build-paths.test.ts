import { describe, expect, it } from "bun:test";
import { join, resolve } from "node:path";
import desktop from "../apps/desktop/mygo.config";
import renderer from "../apps/desktop/vite.config";
import web from "../apps/web/vite.config";

const root = resolve(import.meta.dir, "..");
const build = join(root, "build");
const desktopRoot = join(root, "apps", "desktop");
const webRoot = join(root, "apps", "web");

describe("repository-root build outputs", () => {
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
