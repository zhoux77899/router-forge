import { afterEach, describe, expect, it } from "bun:test";
import { lstat, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { cleanBuildOutputs, verifyBuildOutputs } from "./build-outputs";

const fixtures: string[] = [];
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "router-forge-build-"));
  fixtures.push(root);
  await writeFile(join(root, "package.json"), JSON.stringify({ name: "router-forge" }));
  return root;
}
async function file(root: string, path: string, text = "fixture") {
  const destination = join(root, path);
  await mkdir(dirname(destination), { recursive: true });
  await writeFile(destination, text);
}

afterEach(async () => { await Promise.all(fixtures.splice(0).map(root => rm(root, { recursive: true, force: true }))); });

describe("clean build output boundaries", () => {
  it("removes all known output roots and preserves sources, dependencies, caches and data", async () => {
    const root = await fixture();
    const outputs = [join("build", "desktop-old", "old.exe"), join("apps", "desktop", "build", "old.exe"), join("apps", "desktop", "dist", "index.html"), join("apps", "desktop", "mygo-out", "old.exe"), join("apps", "web", "dist", "index.html"), join("packages", "ui", "dist", "index.js")];
    const retained = [join("assets", "icon.svg"), join("apps", "desktop", "resources", "icon.png"), join("apps", "desktop", "src", "mygo.generated.ts"), join("node_modules", "cached.js"), join(".mygo", "cached.exe"), join(".cache", "build.log"), join("router-forge-data", "state.db"), join("packages", "ui", "src", "index.ts")];
    for (const path of [...outputs, ...retained]) await file(root, path);
    expect(await cleanBuildOutputs(root)).toHaveLength(6);
    for (const path of outputs) await expect(lstat(join(root, path))).rejects.toMatchObject({ code: "ENOENT" });
    for (const path of retained) expect(await readFile(join(root, path), "utf8")).toBe("fixture");
    expect(await cleanBuildOutputs(root)).toEqual([]);
  });

  it("rejects a non-repository root before deleting files", async () => {
    const root = await fixture();
    await writeFile(join(root, "package.json"), JSON.stringify({ name: "another-project" }));
    await file(root, join("build", "old.exe"));
    await expect(cleanBuildOutputs(root)).rejects.toThrow("repository root");
    expect(await readFile(join(root, "build", "old.exe"), "utf8")).toBe("fixture");
  });

  it("validates all targets before removing an earlier valid build directory", async () => {
    const root = await fixture();
    await file(root, join("build", "old.exe"));
    await file(root, join("apps", "desktop", "dist"));
    await expect(cleanBuildOutputs(root)).rejects.toThrow("Expected a build output directory");
    expect(await readFile(join(root, "build", "old.exe"), "utf8")).toBe("fixture");
  });

  for (const linkedPath of ["build", join("build", "nested"), join("apps", "desktop"), join("packages", "linked")]) {
    it(`refuses a symlink/junction at ${linkedPath} without touching its target`, async () => {
      const root = await fixture(), external = await fixture();
      await file(external, "preserved.txt");
      await mkdir(dirname(join(root, linkedPath)), { recursive: true });
      await symlink(external, join(root, linkedPath), process.platform === "win32" ? "junction" : "dir");
      await expect(cleanBuildOutputs(root)).rejects.toThrow(/link|junction/);
      expect(await readFile(join(external, "preserved.txt"), "utf8")).toBe("fixture");
    });
  }
});

describe("build completion", () => {
  it("rejects missing or empty artifacts rather than reporting a successful build", async () => {
    const root = await fixture();
    await expect(verifyBuildOutputs(root)).rejects.toThrow("Missing or empty build artifact");
    await file(root, join("build", "dist", "index.html"), "");
    await expect(verifyBuildOutputs(root)).rejects.toThrow("dist");
  });
});
