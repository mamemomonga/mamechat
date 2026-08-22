import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { copyFileSync, mkdirSync, readFileSync, statSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const configDir = dirname(fileURLToPath(import.meta.url));

// ffmpeg.wasm（WebASM版ffmpeg）のシングルスレッド版コアを node_modules から
// public/ffmpeg/ へ複製する。ここに置けば dev では静的配信、build では dist へ同梱され、
// 同一オリジンから読み込める（CDN不要・COOP/COEP不要のST版）。31MBのwasmはgit管理せず、
// このプラグインで再生成する（.gitignore 参照）。
function copyFfmpegCore(): Plugin {
  const srcDir = resolve(configDir, "node_modules/@ffmpeg/core/dist/esm");
  const dstDir = resolve(configDir, "public/ffmpeg");
  const copy = () => {
    try {
      mkdirSync(dstDir, { recursive: true });
      for (const name of ["ffmpeg-core.js", "ffmpeg-core.wasm"]) {
        const src = resolve(srcDir, name);
        const dst = resolve(dstDir, name);
        // 既に最新（同じサイズ）ならスキップして無駄なコピーを避ける。
        try {
          if (statSync(dst).size === statSync(src).size) {
            continue;
          }
        } catch {
          // 未生成なのでコピーする。
        }
        copyFileSync(src, dst);
      }
    } catch (err) {
      // @ffmpeg/core 未インストール時は警告のみ（動画機能を使わないビルドを妨げない）。
      console.warn("[ffmpeg-core] コアの複製に失敗しました:", err);
    }
  };
  return {
    name: "copy-ffmpeg-core",
    buildStart: copy,
    configureServer: copy,
  };
}

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
  plugins: [react(), copyFfmpegCore()],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion),
  },
  // @ffmpeg/ffmpeg は内部Workerを new URL('./worker.js', import.meta.url) で読み込むため、
  // dev の依存最適化に載せると worker.js の解決に失敗する。事前バンドルから除外する。
  optimizeDeps: {
    exclude: ["@ffmpeg/ffmpeg", "@ffmpeg/util"],
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
