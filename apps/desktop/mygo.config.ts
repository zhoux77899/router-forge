import { defineConfig } from "mygo-cli";
import { join } from "node:path";

const buildRoot = join("..", "..", "build");
const helperRoot = process.env.ROUTER_FORGE_HELPERS_ROOT || buildRoot;

export default defineConfig({
  name: "Router Forge",
  slug: "router-forge",
  identifier: "dev.routerforge.console",
  version: "0.1.0",
  main: ".",
  devUrl: "http://127.0.0.1:7789",
  devCommand: "bun run dev:renderer",
  buildCommand: "bun run build:renderer",
  frontendDist: join(buildRoot, "renderer"),
  bindings: "src/mygo.generated.ts",
  out: join(buildRoot, "desktop"),
  resources: [join(helperRoot, "server"), join(helperRoot, "workers"), join(buildRoot, "dist")],
});
