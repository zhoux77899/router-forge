import { afterEach, expect, it } from "bun:test";
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import rootPackage from "../package.json";

const fixtures: string[] = [];
const bash = Bun.which("bash") ?? (() => { throw new Error("Build entry tests require Bash (Git Bash on Windows)."); })();
const root = resolve(import.meta.dir, "..");

async function runBuild({ fail = "", args = [], missingGo = false }: { fail?: string; args?: string[]; missingGo?: boolean } = {}) {
  const fixture = await mkdtemp(join(tmpdir(), "router-forge-shell-"));
  fixtures.push(fixture);
  const repo = join(fixture, "repository with spaces"), tools = join(fixture, "tools"), log = join(fixture, "commands.log");
  await mkdir(repo); await mkdir(tools);
  await copyFile(join(root, "build.sh"), join(repo, "build.sh"));
  for (const tool of missingGo ? ["bun", "bunx", "dirname"] : ["bun", "bunx", "go"]) {
    const script = tool === "dirname" ? '#!/bin/sh\nif [ "$1" = "--" ]; then shift; fi\nprintf "%s\\n" "${1%/*}"\n' : `#!/bin/sh\nprintf '%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n' '${tool}' "$*" "$PWD" "\${ROUTER_FORGE_HELPERS_ROOT-unset}" "\${GOOS-unset}" "\${GOARCH-unset}" >> "$BUILD_TEST_LOG"\nif [ "$*" = "$BUILD_TEST_FAIL" ]; then exit 23; fi\n`;
    await writeFile(join(tools, tool), script, { mode: 0o755 });
  }
  const child = Bun.spawn([bash, "--noprofile", "--norc", "-c", 'export PATH="$1${BUILD_TEST_PATH_SUFFIX:+:$PATH}"; exec "$2" "$3" "${@:4}"', "build-test", tools, bash, join(repo, "build.sh"), ...args], {
    cwd: fixture,
    env: { ...process.env, BUILD_TEST_LOG: log, BUILD_TEST_FAIL: fail, BUILD_TEST_PATH_SUFFIX: missingGo ? "" : "1", ROUTER_FORGE_HELPERS_ROOT: "external-helper-fixture", GOOS: "unexpected-os", GOARCH: "unexpected-arch" },
    stdout: "pipe", stderr: "pipe",
  });
  const [code, stdout, stderr] = await Promise.all([child.exited, new Response(child.stdout).text(), new Response(child.stderr).text()]);
  const commands = (await readFile(log, "utf8").catch(error => { if (error.code === "ENOENT") return ""; throw error; })).trim().split("\n").filter(Boolean).map(line => line.split("\t"));
  return { code, stdout, stderr, commands };
}

afterEach(async () => { await Promise.all(fixtures.splice(0).map(path => rm(path, { recursive: true, force: true }))); });

it("routes both full-build package commands to the root Bash entry", () => {
  expect(rootPackage.scripts.build).toBe("bash ./build.sh");
  expect(rootPackage.scripts["build:desktop"]).toBe("bun run build");
  expect(rootPackage.scripts["build:web"]).toBe("bun run --filter @router-forge/web build");
  expect(rootPackage.scripts["build:backend"]).toBe("bun scripts/build-backend.ts");
});

it("builds in order from the script's root even when invoked from another directory", async () => {
  const result = await runBuild();
  expect(result.code).toBe(0);
  expect(result.commands.map(command => `${command[0]} ${command[1]}`)).toEqual([
    "bun --version", "go version", "bunx --no-install mygo --version",
    'bun --no-install -e for (const name of ["vite", "mygo-cli", "@resvg/resvg-js"]) require.resolve(name)',
    "bun scripts/build-outputs.ts clean", "bun run build:web", "bun run build:backend", "bun run --cwd ./apps/desktop build", "bun scripts/build-outputs.ts verify",
  ]);
  expect(result.commands.every(command => command[2]!.endsWith("/repository with spaces"))).toBe(true);
  expect(result.commands.slice(4).every(command => command.slice(3).every(value => value === "unset"))).toBe(true);
  expect(result.stdout).toContain("Clean build completed");
});

for (const fail of ["--no-install mygo --version", "scripts/build-outputs.ts clean", "run build:web", "run build:backend", "run --cwd ./apps/desktop build", "scripts/build-outputs.ts verify"]) {
  it(`stops with the failing exit code at ${fail}`, async () => {
    const result = await runBuild({ fail });
    expect(result.code).toBe(23);
    expect(result.commands.at(-1)?.[1]).toBe(fail);
    expect(result.stdout).not.toContain("Clean build completed");
    if (fail === "--no-install mygo --version") expect(result.commands.some(command => command[1] === "scripts/build-outputs.ts clean")).toBe(false);
  });
}

it("rejects unsupported output overrides before touching tools or build outputs", async () => {
  const result = await runBuild({ args: ["--out", "external"] });
  expect(result.code).toBe(2); expect(result.commands).toEqual([]);
});

it("prints help without running a build", async () => {
  const result = await runBuild({ args: ["--help"] });
  expect(result.code).toBe(0); expect(result.commands).toEqual([]); expect(result.stdout).toContain("Usage:");
});

it("rejects a missing tool before cleanup rather than installing it", async () => {
  const result = await runBuild({ missingGo: true });
  expect(result.code).toBe(1); expect(result.commands).toEqual([]); expect(result.stderr).toContain("Missing build tool: go");
});
