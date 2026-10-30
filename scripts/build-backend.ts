import { mkdir } from "node:fs/promises";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";

const root = resolve(import.meta.dir, "..");
const nativeOS = process.platform === "win32" ? "windows" : process.platform === "darwin" ? "darwin" : "linux";
const nativeArch = process.arch === "arm64" ? "arm64" : "amd64";
const build = join(root, "build");
await mkdir(join(build, "server"), { recursive: true });
await mkdir(join(build, "workers"), { recursive: true });

function run(args: string[], env: Record<string, string>) {
  return new Promise<void>((ok, fail) => {
    const child = spawn("go", args, { cwd: root, stdio: "inherit", env: { ...process.env, ...env } });
    child.on("error", fail);
    child.on("exit", code => code === 0 ? ok() : fail(new Error(`go ${args.join(" ")} exited ${code}`)));
  });
}

await run(["build", "-trimpath", "-o", join(build, "server", `router-forge-server${nativeOS === "windows" ? ".exe" : ""}`), "./apps/server"], { CGO_ENABLED: "0" });
const targets = new Set([`${nativeOS}/${nativeArch}`, "linux/amd64", "linux/arm64"]);
for (const target of targets) {
  const [os, arch] = target.split("/");
  await run(["build", "-trimpath", "-o", join(build, "workers", `router-forge-worker-${os}-${arch}${os === "windows" ? ".exe" : ""}`), "./apps/worker"], { GOOS: os, GOARCH: arch, CGO_ENABLED: "0" });
}
