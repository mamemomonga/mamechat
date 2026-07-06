// 最小限のサービスワーカー。AndroidでPWA（standalone/WebAPK）としてインストール
// 可能にするための fetch ハンドラを持つ。キャッシュはせず素通しするため、
// アプリ更新が古いまま固定されることはない（バージョン不一致時の自動リロードに任せる）。
self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", () => {
  // respondWith を呼ばず、ブラウザの既定の取得に委ねる（キャッシュしない）。
});

// Web Push 受信。ペイロードは {title, body, url} のJSON。
self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = { title: event.data ? event.data.text() : "" };
  }
  const title = data.title || "通知";
  const options = {
    body: data.body || "",
    icon: "/icon-192.png",
    badge: "/icon-192.png",
    data: { url: data.url || "/" },
  };
  event.waitUntil(
    (async () => {
      // 診断用: pushがSWに届いたことを開いているページへ知らせる。
      const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const client of clients) {
        client.postMessage({ type: "push-received", title });
      }
      try {
        await self.registration.showNotification(title, options);
      } catch (err) {
        for (const client of clients) {
          client.postMessage({ type: "push-error", message: String(err) });
        }
      }
    })(),
  );
});

// 通知タップ／クリックで、対象チャンネルを開く（既存タブがあればそれを使う）。
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || "/";
  event.waitUntil(
    (async () => {
      const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const client of all) {
        if ("focus" in client) {
          await client.focus();
          if ("navigate" in client) {
            try {
              await client.navigate(url);
            } catch {
              // ナビゲーション不可でもフォーカスは済んでいるので無視。
            }
          }
          return;
        }
      }
      if (self.clients.openWindow) {
        await self.clients.openWindow(url);
      }
    })(),
  );
});
