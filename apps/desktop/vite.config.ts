import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { join } from "node:path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "./",
  server: {
    host: "127.0.0.1",
    port: 7789,
    strictPort: true,
    watch: { ignored: ["**/.mygo/**"] },
  },
  preview: { host: "127.0.0.1", port: 7791, strictPort: true },
  build: { outDir: join("..", "..", "build", "renderer"), emptyOutDir: true, sourcemap: false },
});
