import { defineConfig } from "mygo-cli";
import { join } from "node:path";

const helperRoot = process.env.ROUTER_FORGE_HELPERS_ROOT || join("..", "..", "build");

export default defineConfig({
  name: "Router Forge",
  slug: "router-forge",
  identifier: "dev.routerforge.console",
  version: "0.1.0",
  main: ".",
  devUrl: "http://127.0.0.1:5174",
  devCommand: "bun run prepare:helpers && bun run dev:renderer",
  buildCommand: "bun run build:renderer",
  frontendDist: "dist",
  bindings: "src/mygo.generated.ts",
  out: "build",
  resources: [join(helperRoot, "server"), join(helperRoot, "workers"), join("..", "web", "dist")],
});
