import { defineConfig, type Plugin, type ViteDevServer } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { validateProxyOrigin } from "./src/proxy";

const target = validateProxyOrigin(process.env.ROUTER_FORGE_API_URL);
const proxy = target ? { "/api": { target, changeOrigin: false } } : undefined;
function rejectMissingBackend(server: Pick<ViteDevServer, "middlewares">) {
  if (target) return;
  server.middlewares.use("/api", (_request, response) => {
    response.statusCode = 503;
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify({ error: "未设置本机后端地址。请设置 ROUTER_FORGE_API_URL 为桌面显示的 loopback URL 后重启 Web 服务。" }));
  });
}
const missingBackend: Plugin = {
  name: "local-backend-required",
  configureServer: rejectMissingBackend,
  configurePreviewServer: rejectMissingBackend,
};

export default defineConfig({
  plugins: [react(), tailwindcss(), missingBackend],
  base: "./",
  server: { host: "127.0.0.1", port: 5173, strictPort: true, proxy },
  preview: { host: "127.0.0.1", port: 4173, strictPort: true, proxy },
  build: { outDir: "dist", sourcemap: false },
});
