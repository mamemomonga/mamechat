import React from "react";
import ReactDOM from "react-dom/client";
import { createBrowserRouter, Navigate, RouterProvider, useParams } from "react-router";

import App from "./App";
import HomePage from "./routes/HomePage";
import SettingsPage from "./routes/SettingsPage";
import ServiceInfoPage from "./routes/ServiceInfoPage";
import UnavailablePage from "./routes/UnavailablePage";
import LoginPage from "./routes/LoginPage";
import ChannelPage from "./routes/ChannelPage";
import ChannelCreatePage from "./routes/ChannelCreatePage";
import ChannelSettingsPage from "./routes/ChannelSettingsPage";
import DeckPage from "./routes/DeckPage";
import { useDeckMode } from "./lib/deckMode";
import "./styles.css";

// DeckRoute は "/" と "/channels/:slug" の両方で使う単一コンポーネント。
// 同一の要素型を使うことで、両ルート間の遷移で DeckPage をアンマウントさせない
// （＝各チャンネルカラムのWebSocketを張り直さない）。
// スマホ/iPad縦（deckMode=false）は従来の単一ページ表示（HomePage / ChannelPage）。
function DeckRoute() {
  const deck = useDeckMode();
  const { slug } = useParams();
  if (!deck) {
    return slug ? <ChannelPage /> : <HomePage />;
  }
  return <DeckPage routeSlug={slug ?? null} />;
}

const router = createBrowserRouter([
  {
    path: "/",
    element: <App />,
    children: [
      { index: true, element: <DeckRoute /> },
      { path: "settings", element: <SettingsPage /> },
      { path: "info", element: <ServiceInfoPage /> },
      { path: "unavailable", element: <UnavailablePage /> },
      { path: "account", element: <Navigate to="/settings" replace /> },
      { path: "login", element: <LoginPage /> },
      { path: "channels/new", element: <ChannelCreatePage /> },
      { path: "channels/:slug", element: <DeckRoute /> },
      { path: "channels/:slug/settings", element: <ChannelSettingsPage /> },
    ],
  },
]);

async function bootstrap() {
  // アクセス時に最初にアプリのバージョン（X.X.X-{commitHash}）をログ出力する。
  // eslint-disable-next-line no-console
  console.info(`mamechat version: ${__APP_VERSION__}`);

  // 開発用モックモード。import.meta.env.DEV ガードにより、prodビルドでは
  // このブランチごと（動的importのチャンクも含めて）Rollupに除去される。
  if (import.meta.env.DEV && import.meta.env.VITE_MOCK_USER) {
    const { installMockMode } = await import("./mock");
    installMockMode();
  }

  // AndroidでPWA（standalone）としてインストール可能にするため、本番のみSWを登録する。
  // 開発時はViteのHMRと干渉しないよう登録しない。
  if (import.meta.env.PROD && "serviceWorker" in navigator) {
    window.addEventListener("load", () => {
      void navigator.serviceWorker
        .register("/sw.js")
        .then((reg) => {
          // 起動のたびに更新を確認する。push対応など新しい sw.js へ確実に入れ替える
          // （skipWaiting + clients.claim で即時有効化される）。
          void reg.update().catch(() => {});
        })
        .catch(() => {
          // 登録失敗（非HTTPS等）でもアプリ自体は通常どおり動作する。
        });
    });
  }

  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <RouterProvider router={router} />
    </React.StrictMode>,
  );
}

void bootstrap();
