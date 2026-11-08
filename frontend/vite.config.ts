import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
export default defineConfig({
  plugins: [vue()],
  base: "/app/",
  build: { target: "es2022", modulePreload: { polyfill: false }, cssCodeSplit: false, outDir: "../internal/webui/app", emptyOutDir: true },
  server: { proxy: { "/api": "http://127.0.0.1:18089" } },
});
