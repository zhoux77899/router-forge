import { lstat, readFile, readdir, rm } from "node:fs/promises";
import { isAbsolute, join, relative, resolve, sep } from "node:path";
import desktop from "../apps/desktop/mygo.config";

const repositoryRoot = resolve(import.meta.dir, "..");
const legacyOutputs = [
  ["apps", "desktop", "build"],
  ["apps", "desktop", "dist"],
  ["apps", "desktop", "mygo-out"],
  ["apps", "web", "dist"],
];

async function metadata(path: string) {
  return lstat(path).catch(error => {
    if (error.code === "ENOENT") return null;
    throw error;
  });
}

async function assertDirectory(path: string) {
  const info = await metadata(path);
  if (!info) return false;
  if (info.isSymbolicLink()) throw new Error(`Refusing to follow a symlink or junction: ${path}`);
  if (!info.isDirectory()) throw new Error(`Expected a build output directory: ${path}`);
  return true;
}

async function assertNoLinks(path: string): Promise<void> {
  const info = await lstat(path);
  if (info.isSymbolicLink()) throw new Error(`Refusing to clean a symlink or junction: ${path}`);
  if (info.isDirectory()) {
    for (const entry of await readdir(path)) await assertNoLinks(join(path, entry));
  } else if (!info.isFile()) {
    throw new Error(`Refusing to clean an unexpected filesystem entry: ${path}`);
  }
}

async function assertOutput(root: string, path: string) {
  const local = relative(root, path);
  if (!local || isAbsolute(local) || local === ".." || local.startsWith(`..${sep}`)) {
    throw new Error(`Build output must be a descendant of the repository: ${path}`);
  }
  let ancestor = root;
  for (const part of local.split(sep)) {
    ancestor = join(ancestor, part);
    if (!await assertDirectory(ancestor)) return false;
  }
  await assertNoLinks(path);
  return true;
}

export async function cleanBuildOutputs(root = repositoryRoot): Promise<string[]> {
  root = resolve(root);
  const manifest = JSON.parse(await readFile(join(root, "package.json"), "utf8"));
  if (manifest.name !== "router-forge") throw new Error("Clean builds require the Router Forge repository root.");

  const candidates = [join(root, "build"), ...legacyOutputs.map(parts => join(root, ...parts))];
  const packages = join(root, "packages");
  if (await assertDirectory(packages)) {
    for (const entry of await readdir(packages, { withFileTypes: true })) {
      if (entry.isSymbolicLink()) throw new Error(`Refusing a linked package directory: ${join(packages, entry.name)}`);
      if (entry.isDirectory()) candidates.push(join(packages, entry.name, "dist"));
    }
  }

  // Validate every target before deleting any output, including linked ancestors.
  const existing: string[] = [];
  for (const path of candidates) if (await assertOutput(root, path)) existing.push(path);
  for (const path of existing) await rm(path, { recursive: true, force: true });
  return existing.map(path => relative(root, path));
}

export async function verifyBuildOutputs(root = repositoryRoot): Promise<string[]> {
  const os = process.platform === "win32" ? "windows" : process.platform === "darwin" ? "darwin" : "linux";
  const arch = process.arch === "arm64" ? "arm64" : "amd64";
  const suffix = os === "windows" ? ".exe" : "";
  const targets = new Set([`${os}/${arch}`, "linux/amd64", "linux/arm64"]);
  const workers = [...targets].map(target => {
    const [targetOS, targetArch] = target.split("/");
    return join("workers", `router-forge-worker-${targetOS}-${targetArch}${targetOS === "windows" ? ".exe" : ""}`);
  });
  const resources = [join("dist", "index.html"), join("server", `router-forge-server${suffix}`), ...workers];
  const native = join("desktop", `${os}-${arch}`);
  const files = [...resources, join("renderer", "index.html")];
  if (os === "windows") {
    files.push(join(native, `${desktop.name}.exe`));
    files.push(join(native, `${desktop.slug}-setup-${desktop.version}-${arch}.exe`));
    files.push(...resources.map(path => join(native, path)));
  }
  for (const path of files) {
    const info = await metadata(join(root, "build", path));
    if (!info?.isFile() || info.size === 0) throw new Error(`Missing or empty build artifact: ${path}`);
  }
  const nativePath = join(root, "build", native);
  if (!await assertDirectory(nativePath) || (await readdir(nativePath)).length === 0) {
    throw new Error(`Missing native package: ${native}`);
  }
  return files;
}

if (import.meta.main) {
  try {
    const args = process.argv.slice(2);
    if (args.length !== 1 || !["clean", "verify"].includes(args[0]!)) {
      throw new Error("Usage: bun scripts/build-outputs.ts <clean|verify>");
    }
    const outputs = args[0] === "clean" ? await cleanBuildOutputs() : await verifyBuildOutputs();
    console.log(`${args[0] === "clean" ? "Removed" : "Verified"} ${outputs.length} build ${args[0] === "clean" ? "directories" : "artifacts"}.`);
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    console.error("Build stopped. Close applications using the output files or resolve the reported path before retrying.");
    process.exitCode = 1;
  }
}
