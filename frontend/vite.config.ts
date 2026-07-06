import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const configDir = dirname(fileURLToPath(import.meta.url));

// commitHash付きの version.full（make versionが生成）を優先し、
// 無ければベースの version（X.X.X）へフォールバックする。
function readAppVersion(): string {
  for (const name of ["../version.full", "../version"]) {
    try {
      return readFileSync(resolve(configDir, name), "utf8").trim();
    } catch {
      // 次の候補へ
    }
  }
  return "dev";
}

const appVersion = readAppVersion();

export default defineConfig({
  plugins: [react()],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion),
  },
  build: {
    rollupOptions: {
      input: {
        // メインアプリと管理アプリを別バンドルに分離（管理用UI/APIをメインに含めない）
        main: resolve(configDir, "index.html"),
        admin: resolve(configDir, "admin/index.html"),
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/healthz": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/oauth": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/ws": {
        target: "ws://localhost:8080",
        ws: true,
      },
    },
  },
});
