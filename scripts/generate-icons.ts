import { Resvg } from "@resvg/resvg-js";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, relative, resolve } from "node:path";

const root = resolve(import.meta.dir, "..");
const assetDirectory = resolve(root, "assets");
export const iconSourcePath = resolve(assetDirectory, "icon.svg");
export const desktopIconPath = resolve(root, "apps", "desktop", "resources", "icon.png");
export const iconMarkPath = resolve(assetDirectory, "icon-mark.svg");
export const faviconLightPath = resolve(assetDirectory, "favicon-light.svg");
export const faviconDarkPath = resolve(assetDirectory, "favicon-dark.svg");
export const iconSize = 1024;
export const desktopIconCornerRatio = 0.2;

type IconPaths = { sourcePath?: string; outputPath?: string };
type SvgIconPaths = { sourcePath?: string; outputDirectory?: string };

function iconBounds(source: string) {
  const svg = source.match(/<svg\b[^>]*>/)?.[0];
  const viewBox = svg?.match(/\bviewBox=(["'])(.*?)\1/s)?.[2];
  const bounds = viewBox?.trim().split(/[\s,]+/).map(Number);
  if (!svg || !bounds || bounds.length !== 4 || !bounds.every(Number.isFinite) || bounds[2]! <= 0 || bounds[3]! <= 0) {
    throw new Error("Application icon must have a valid SVG viewBox.");
  }
  return { svg, viewBox: bounds.join(" "), x: bounds[0]!, y: bounds[1]!, width: bounds[2]!, height: bounds[3]! };
}

export function roundedDesktopIcon(source: string): string {
  const { svg, x, y, width, height } = iconBounds(source);
  const radius = Math.min(width, height) * desktopIconCornerRatio;
  const clip = `<defs><clipPath id="router-forge-icon-corners"><rect x="${x}" y="${y}" width="${width}" height="${height}" rx="${radius}" /></clipPath></defs>`;
  return source.replace(svg, `${svg}\n${clip}\n<g clip-path="url(#router-forge-icon-corners)">`).replace("</svg>", "</g>\n</svg>");
}

export function monochromeIcon(source: string, color: "currentColor" | "#0a0a0a" | "#fafafa" = "currentColor"): string {
  const { viewBox } = iconBounds(source);
  // The supplied artwork separates its white canvas from its black glyph paths.
  const paths = [...source.matchAll(/<path\b[^>]*>/g)].map(match => match[0]).filter(path => /\bfill=(["'])#000000\1/i.test(path));
  if (paths.length === 0) throw new Error("Application icon must contain black foreground paths.");
  const foreground = paths.map(path => path.replace(/\bfill=(["'])#000000\1/i, `fill="${color}"`).replace(/\/?>$/, " />")).join("\n");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}">\n${foreground}\n</svg>\n`;
}

async function writeIcon(outputPath: string, content: Buffer | string): Promise<boolean> {
  const bytes = typeof content === "string" ? Buffer.from(content) : content;
  const previous = await readFile(outputPath).catch(error => {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return null;
    throw error;
  });
  if (previous?.equals(bytes)) return false;
  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, bytes);
  return true;
}

export async function generateIcons({ sourcePath = iconSourcePath, outputPath = desktopIconPath }: IconPaths = {}): Promise<boolean> {
  if (resolve(sourcePath) === resolve(outputPath)) {
    throw new Error("Icon output must not overwrite the SVG source.");
  }
  const source = await readFile(sourcePath, "utf8");
  const image = new Resvg(roundedDesktopIcon(source), {
    dpi: 96,
    fitTo: { mode: "width", value: iconSize },
    font: { loadSystemFonts: false },
  }).render();
  if (image.width !== iconSize || image.height !== iconSize) {
    throw new Error(`Application icon must be square; rendered ${image.width}x${image.height}.`);
  }
  return writeIcon(outputPath, image.asPng());
}

export async function generateSvgIcons({ sourcePath = iconSourcePath, outputDirectory = assetDirectory }: SvgIconPaths = {}): Promise<string[]> {
  const source = await readFile(sourcePath, "utf8");
  const outputs = [
    { path: resolve(outputDirectory, "icon-mark.svg"), content: monochromeIcon(source) },
    { path: resolve(outputDirectory, "favicon-light.svg"), content: monochromeIcon(source, "#0a0a0a") },
    { path: resolve(outputDirectory, "favicon-dark.svg"), content: monochromeIcon(source, "#fafafa") },
  ];
  if (outputs.some(output => resolve(sourcePath) === output.path)) {
    throw new Error("Icon output must not overwrite the SVG source.");
  }
  const changed = await Promise.all(outputs.map(async output => await writeIcon(output.path, output.content) ? output.path : null));
  return changed.filter((path): path is string => path !== null);
}

if (import.meta.main) {
  const [changed, variants] = await Promise.all([generateIcons(), generateSvgIcons()]);
  console.log(`${changed ? "Generated" : "Unchanged"} ${relative(root, desktopIconPath)} (${iconSize}x${iconSize}).`);
  console.log(`${variants.length} transparent SVG icon variant(s) generated.`);
}
