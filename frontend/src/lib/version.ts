const VERSION_RELOAD_KEY = "mamechat.versionReloaded";

// サーバから通知されたバージョンが、埋め込まれたフロントのバージョンと食い違えば
// （＝デプロイで更新された）ブラウザをリロードして最新を取得する。キャッシュ等で
// バージョンが揃わないときの無限リロードを避けるため、同一サーババージョンに対しては
// 一度だけリロードする。
export function handleServerVersion(serverVersion?: string) {
  if (!serverVersion || serverVersion === __APP_VERSION__) {
    return;
  }
  try {
    if (sessionStorage.getItem(VERSION_RELOAD_KEY) === serverVersion) {
      // eslint-disable-next-line no-console
      console.warn(
        `version mismatch persists (client ${__APP_VERSION__} / server ${serverVersion})`,
      );
      return;
    }
    sessionStorage.setItem(VERSION_RELOAD_KEY, serverVersion);
  } catch {
    // sessionStorageが使えない環境でも、最低一度はリロードを試みる。
  }
  // eslint-disable-next-line no-console
  console.info(
    `version mismatch (client ${__APP_VERSION__} / server ${serverVersion}); reloading`,
  );
  window.location.reload();
}
