import { afterAll, beforeAll, describe, expect, it } from "bun:test";
import { Resvg } from "@resvg/resvg-js";
import { randomUUID } from "node:crypto";
import { mkdir, readFile, rm, stat, unlink, utimes, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import desktop from "../apps/desktop/mygo.config";
import desktopPackage from "../apps/desktop/package.json";
import webPackage from "../apps/web/package.json";
import rootPackage from "../package.json";
import { AppIcon, appIconUrl } from "../packages/ui/src/components/app-icon";
import { desktopIconCornerRatio, desktopIconPath, faviconDarkPath, faviconLightPath, generateIcons, generateSvgIcons, iconMarkPath, iconSize, iconSourcePath, monochromeIcon, roundedDesktopIcon } from "./generate-icons";

const root = resolve(import.meta.dir, "..");
const desktopRoot = join(root, "apps", "desktop");
const testRoot = join(root, "build", "icon-generation-tests", randomUUID());

beforeAll(async () => { await mkdir(testRoot, { recursive: true }); });
afterAll(async () => { await rm(testRoot, { recursive: true, force: true }); });

describe("application icon generation", () => {
  it("preserves the original SVG bytes and modification time", async () => {
    const source = await readFile(iconSourcePath);
    const sourceStat = await stat(iconSourcePath);
    await generateIcons({ outputPath: join(testRoot, "preserved.png") });
    expect(await readFile(iconSourcePath)).toEqual(source);
    expect((await stat(iconSourcePath)).mtimeMs).toBe(sourceStat.mtimeMs);
  });

  it("refuses to overwrite an SVG source with generated output", async () => {
    const sourcePath = join(testRoot, "same-file.svg");
    const source = await readFile(iconSourcePath);
    await writeFile(sourcePath, source);
    await expect(generateIcons({ sourcePath, outputPath: sourcePath })).rejects.toThrow("Icon output must not overwrite the SVG source.");
    expect(await readFile(sourcePath)).toEqual(source);
  });

  it("produces the packaged 1024x1024 PNG from the original SVG", async () => {
    const outputPath = join(testRoot, "nested", "icon.png");
    expect(await generateIcons({ outputPath })).toBe(true);
    const png = await readFile(outputPath);
    expect(png.subarray(0, 8)).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));
    expect(png.toString("ascii", 12, 16)).toBe("IHDR");
    expect(png.readUInt32BE(16)).toBe(iconSize);
    expect(png.readUInt32BE(20)).toBe(iconSize);
    expect(await readFile(desktopIconPath)).toEqual(png);
  });

  it("regenerates identical bytes and does not rewrite unchanged output", async () => {
    const outputPath = join(testRoot, "deterministic.png");
    expect(await generateIcons({ outputPath })).toBe(true);
    const png = await readFile(outputPath);
    const previousTime = new Date("2000-01-01T00:00:00Z");
    await utimes(outputPath, previousTime, previousTime);
    const previousStat = await stat(outputPath);
    expect(await generateIcons({ outputPath })).toBe(false);
    expect((await stat(outputPath)).mtimeMs).toBe(previousStat.mtimeMs);
    await unlink(outputPath);
    expect(await generateIcons({ outputPath })).toBe(true);
    expect(await readFile(outputPath)).toEqual(png);
  });

  it("rejects non-square sources without replacing the previous PNG", async () => {
    const sourcePath = join(testRoot, "non-square.svg");
    const outputPath = join(testRoot, "existing.png");
    const png = await readFile(desktopIconPath);
    await writeFile(sourcePath, '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 2 1"><path d="M0 0h2v1H0z"/></svg>');
    await writeFile(outputPath, png);
    await expect(generateIcons({ sourcePath, outputPath })).rejects.toThrow("Application icon must be square; rendered 1024x512.");
    expect(await readFile(outputPath)).toEqual(png);
  });
});

describe("source-derived icon variants", () => {
  it("clips Desktop corners to transparency while preserving its white canvas and central artwork", async () => {
    const source = await readFile(iconSourcePath, "utf8");
    const rounded = roundedDesktopIcon(source);
    const options = { fitTo: { mode: "width" as const, value: iconSize }, font: { loadSystemFonts: false } };
    const original = new Resvg(source, options).render();
    const image = new Resvg(rounded, options).render();
    const pixels = image.pixels; const originalPixels = original.pixels;
    expect(desktopIconCornerRatio).toBe(0.2); expect(rounded).toContain('rx="204.8"');
    expect(image.asPng()).toEqual(await readFile(desktopIconPath));
    for (const [x, y] of [[0, 0], [1023, 0], [0, 1023], [1023, 1023]]) expect(pixels[(y! * iconSize + x!) * 4 + 3]).toBe(0);
    expect(pixels.subarray((4 * iconSize + 512) * 4, (4 * iconSize + 512) * 4 + 4)).toEqual(Buffer.from([255, 255, 255, 255]));
    const centerUnchanged = Array.from({ length: 512 }, (_, index) => index + 256).every(y => {
      const start = (y * iconSize + 256) * 4;
      return pixels.subarray(start, start + 512 * 4).equals(originalPixels.subarray(start, start + 512 * 4));
    });
    expect(centerUnchanged).toBe(true);
  });

  it("uses only the original foreground paths for transparent themeable marks and fixed system favicons", async () => {
    const source = await readFile(iconSourcePath, "utf8");
    const originalPaths = [...source.matchAll(/<path\b[^>]*>/g)].map(match => match[0]).filter(path => /fill="#000000"/.test(path));
    const mark = monochromeIcon(source);
    expect(mark).toContain('fill="currentColor"'); expect(mark).not.toContain("#ffffff"); expect(mark).not.toContain("#808080");
    expect(mark.match(/<path\b/g)).toHaveLength(originalPaths.length);
    for (const path of originalPaths) expect(mark).toContain(path.match(/\bd="([\s\S]*?)"/)?.[1] ?? "missing path");
    for (const color of ["#0a0a0a", "#fafafa"] as const) {
      const svg = monochromeIcon(source, color);
      expect(svg).not.toMatch(/currentColor|prefers-color-scheme|clipPath|#ffffff/);
      const image = new Resvg(svg, { fitTo: { mode: "width", value: 64 }, font: { loadSystemFonts: false } }).render();
      const pixels = image.pixels;
      expect(pixels[3]).toBe(0);
      const channel = color === "#0a0a0a" ? 10 : 250;
      let solid = 0; let incorrect = 0;
      for (let offset = 0; offset < pixels.length; offset += 4) {
        if (pixels[offset + 3] !== 255) continue;
        solid++;
        if ([0, 1, 2].some(index => pixels[offset + index] !== channel)) incorrect++;
      }
      expect(solid).toBeGreaterThan(100); expect(incorrect).toBe(0);
    }
    expect(() => monochromeIcon('<svg viewBox="0 0 10 10"><path fill="#00ff00" d="M0 0h10v10z" /></svg>')).toThrow("black foreground paths");
  });

  it("generates reproducible SVG variants without touching the original source or unchanged outputs", async () => {
    const outputDirectory = join(testRoot, "svg-variants");
    const source = await readFile(iconSourcePath); const sourceStat = await stat(iconSourcePath);
    const generated = await generateSvgIcons({ outputDirectory });
    const packaged = [iconMarkPath, faviconLightPath, faviconDarkPath];
    expect(generated).toHaveLength(3);
    const previousTime = new Date("2000-01-01T00:00:00Z");
    for (let index = 0; index < generated.length; index++) {
      expect(await readFile(generated[index]!)).toEqual(await readFile(packaged[index]!));
      await utimes(generated[index]!, previousTime, previousTime);
    }
    const previousStats = await Promise.all(generated.map(path => stat(path)));
    expect(await generateSvgIcons({ outputDirectory })).toEqual([]);
    for (let index = 0; index < generated.length; index++) expect((await stat(generated[index]!)).mtimeMs).toBe(previousStats[index]!.mtimeMs);
    expect(await readFile(iconSourcePath)).toEqual(source); expect((await stat(iconSourcePath)).mtimeMs).toBe(sourceStat.mtimeMs);
  });

  it("refuses a SVG variant path that would overwrite its design source", async () => {
    const outputDirectory = join(testRoot, "svg-source-guard");
    await mkdir(outputDirectory, { recursive: true });
    const sourcePath = join(outputDirectory, "icon-mark.svg"); const source = await readFile(iconSourcePath);
    await writeFile(sourcePath, source);
    await expect(generateSvgIcons({ sourcePath, outputDirectory })).rejects.toThrow("must not overwrite the SVG source");
    expect(await readFile(sourcePath)).toEqual(source);
    await expect(stat(join(outputDirectory, "favicon-light.svg"))).rejects.toMatchObject({ code: "ENOENT" });
  });
});

describe("application icon integration", () => {
  it("uses repository-relative source and native packaging paths", () => {
    expect(iconSourcePath).toBe(join(root, "assets", "icon.svg"));
    expect(desktopIconPath).toBe(join(desktopRoot, "resources", "icon.png"));
    expect(resolve(desktopRoot, desktop.icon ?? "")).toBe(desktopIconPath);
    expect(rootPackage.scripts["generate:icons"]).toBe("bun scripts/generate-icons.ts");
    expect(rootPackage.devDependencies["@resvg/resvg-js"]).toBe("2.6.2");
  });

  it("generates icons before MyGo reads configuration in dev and build", () => {
    expect(desktopPackage.scripts.dev).toBe("bun run prepare:helpers && mygo dev");
    expect(desktopPackage.scripts["prepare:helpers"].split(" && ")[0]).toBe("bun run --cwd ../.. generate:icons");
    expect(desktopPackage.scripts.build).toBe("bun run --cwd ../.. generate:icons && mygo build");
    expect(webPackage.scripts.dev).toBe("bun run --cwd ../.. generate:icons && vite --host 127.0.0.1");
    expect(webPackage.scripts.build).toBe("bun run --cwd ../.. generate:icons && vite build");
  });

  it("embeds the same native PNG for the runtime window icon", async () => {
    const helper = await readFile(join(desktopRoot, "icon.go"), "utf8");
    const embeddedPath = helper.match(/\/\/go:embed ([^\r\n]+)/)?.[1];
    expect(resolve(desktopRoot, embeddedPath ?? "")).toBe(desktopIconPath);
    expect(helper).toContain("win.SetIcon(applicationIcon)");
  });

  it("renders an accessible transparent mark that inherits the current theme foreground", async () => {
    const { type, props } = AppIcon({});
    expect(type).toBe("span"); expect(props.role).toBe("img"); expect(props["aria-label"]).toBe("Router Forge");
    expect(props.draggable).toBe(false); expect(props.className).toBe("app-icon size-6 shrink-0");
    expect(props.style.maskImage).toBe(`url("${appIconUrl}")`); expect(props.style.WebkitMaskImage).toBe(props.style.maskImage);
    expect(appIconUrl).toContain("icon-mark.svg");
    const styles = await readFile(join(root, "packages", "ui", "src", "styles.css"), "utf8");
    const mark = styles.match(/\.app-icon\s*\{([^}]+)\}/)?.[1] ?? "";
    expect(mark).toContain("background-color: currentColor"); expect(mark).toContain("mask-size: contain");
  });
});
