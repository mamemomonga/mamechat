import { useEffect, useState } from "react";
import { Outlet, useNavigate } from "react-router";

import { getMe, getPublicConfig, logout } from "./lib/api";
import { useServiceName } from "./lib/serviceName";
import type { User } from "./types/chat";

export default function AdminApp() {
  const navigate = useNavigate();
  const serviceName = useServiceName("管理");
  const [user, setUser] = useState<User | null>(null);
  // undefined=判定中。サーバで管理ページが無効（ADMIN_ENABLED=false）なら画面を出さない。
  const [adminEnabled, setAdminEnabled] = useState<boolean | undefined>(undefined);

  useEffect(() => {
    getPublicConfig()
      .then((config) => setAdminEnabled(Boolean(config.adminEnabled)))
      .catch(() => setAdminEnabled(false));
  }, []);

  useEffect(() => {
    void load();
    window.addEventListener("auth-changed", load);
    return () => window.removeEventListener("auth-changed", load);
  }, []);

  async function load() {
    const res = await getMe().catch(() => ({ user: null }));
    setUser(res.user);
  }

  async function handleLogout() {
    await logout();
    setUser(null);
    window.dispatchEvent(new Event("auth-changed"));
    navigate("/login");
  }

  return (
    <div className="appShell">
      <header className="topBar">
        <a className="brand" href="/">
          {serviceName} 管理
        </a>
        <nav>
          <a href="/">メインサイト</a>
          {user && adminEnabled ? (
            <button type="button" className="loginNavButton" onClick={handleLogout}>
              ログアウト
            </button>
          ) : null}
        </nav>
      </header>
      <main>
        {adminEnabled === undefined ? null : adminEnabled ? (
          <Outlet />
        ) : (
          <section className="narrowPanel">
            <h1>管理ページは無効です</h1>
            <p>
              管理ページはサーバ設定で無効化されています。利用するには ADMIN_ENABLED=true
              を設定してサーバを再起動してください。
            </p>
          </section>
        )}
      </main>
      <footer className="appFooter">v{__APP_VERSION__}</footer>
    </div>
  );
}
