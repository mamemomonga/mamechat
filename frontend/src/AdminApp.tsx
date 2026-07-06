import { useEffect, useState } from "react";
import { Outlet, useNavigate } from "react-router";

import { getMe, logout } from "./lib/api";
import { useServiceName } from "./lib/serviceName";
import type { User } from "./types/chat";

export default function AdminApp() {
  const navigate = useNavigate();
  const serviceName = useServiceName("管理");
  const [user, setUser] = useState<User | null>(null);

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
          {user ? (
            <button type="button" className="loginNavButton" onClick={handleLogout}>
              ログアウト
            </button>
          ) : null}
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
      <footer className="appFooter">v{__APP_VERSION__}</footer>
    </div>
  );
}
