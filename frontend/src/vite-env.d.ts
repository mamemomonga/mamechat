/// <reference types="vite/client" />

declare const __APP_VERSION__: string;

interface ImportMetaEnv {
  // 開発用モックモードの有効化（例: VITE_MOCK_USER=1 / owner / admin）。
  readonly VITE_MOCK_USER?: string;
}
