import { useCallback, useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router";

import PushEnablePrompt from "./components/PushEnablePrompt";
import PwaInstallPrompt from "./components/PwaInstallPrompt";
import SettingsPanel from "./components/SettingsPanel";
import UnavailableNotice from "./components/UnavailableNotice";
import UserAvatar from "./components/UserAvatar";
import { DeckProvider } from "./context/DeckContext";
import { getMe, isTransientError, logout, setUnauthorizedHandler } from "./lib/api";
import {
  consumeReturnTo,
  handleUnauthorized,
  setSuspended,
  UNAVAILABLE_PATH,
} from "./lib/authRedirect";
import { useServiceName } from "./lib/serviceName";
import type { WebSocketStatus } from "./lib/websocket";
import type { User } from "./types/chat";

// 全ページ共通の認証状態ポーリング間隔（ms）。開いたまま放置されたページでも
// セッション失効・停止を検知して誘導するため、App が定期的に /api/me を確認する。
const AUTH_POLL_MS = 20000;

export default function App() {
  const navigate = useNavigate();
  const serviceName = useServiceName();
  // undefined=判定中 / null=未ログイン / User=ログイン済み。
  const [user, setUser] = useState<User | null | undefined>(undefined);
  const [menuOpen, setMenuOpen] = useState(false);
  // 設定は画面遷移せずモーダルで開く（トップ・チャットどちらからでも）。
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [channelStatus, setChannelStatus] = useState<WebSocketStatus | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  // 以前に認証済みだったか（未ログイン検知時にセッション喪失かどうかの判定に使う）。
  const wasAuthedRef = useRef(false);

  const loadUser = useCallback(async () => {
    let res: { user: User | null };
    try {
      res = await getMe();
    } catch (err) {
      // サーバ再起動・ネットワーク断など（401以外の一時的障害）は、セッション切れと
      // 区別してログイン画面へ戻さない。状態を維持し、次回ポーリングで自動復帰する。
      if (isTransientError(err)) {
        return;
      }
      // 明示的な認証エラー（通常 /api/me は200で user:null を返すため稀）。
      res = { user: null };
    }
    const nextUser = res.user ?? null;
    setUser(nextUser);
    const suspended = !!nextUser && nextUser.status === "suspended";
    setSuspended(suspended);

    if (nextUser && !suspended) {
      // ログイン済み（有効）。再ログイン後の戻り先があれば復帰する。
      wasAuthedRef.current = true;
      const returnTo = consumeReturnTo();
      const here = window.location.pathname + window.location.search;
      if (returnTo && returnTo !== here) {
        navigate(returnTo, { replace: true });
      }
      return;
    }
    if (suspended) {
      // 停止ユーザーは「現在ご利用になれません」固定へ。
      wasAuthedRef.current = true;
      if (window.location.pathname !== UNAVAILABLE_PATH) {
        navigate(UNAVAILABLE_PATH, { replace: true });
      }
      return;
    }
    // 未ログイン。以前認証済みだったならセッション喪失としてログインへ誘導する。
    if (wasAuthedRef.current) {
      wasAuthedRef.current = false;
      handleUnauthorized(navigate);
    }
  }, [navigate]);

  useEffect(() => {
    void loadUser();
    window.addEventListener("auth-changed", loadUser);
    return () => window.removeEventListener("auth-changed", loadUser);
  }, [loadUser]);

  // 401 受信時（ポーリング/API）にログイン・利用不可ページへ誘導する。
  useEffect(() => {
    setUnauthorizedHandler(() => handleUnauthorized(navigate));
    return () => setUnauthorizedHandler(null);
  }, [navigate]);

  // 開いたまま放置されても検知できるよう、認証状態を定期的に確認する。
  useEffect(() => {
    const id = window.setInterval(() => {
      void loadUser();
    }, AUTH_POLL_MS);
    return () => window.clearInterval(id);
  }, [loadUser]);

  useEffect(() => {
    function updateVisualViewportHeight() {
      const height = window.visualViewport?.height ?? window.innerHeight;
      document.documentElement.style.setProperty("--visual-viewport-height", `${height}px`);
    }
    updateVisualViewportHeight();
    window.addEventListener("resize", updateVisualViewportHeight);
    window.visualViewport?.addEventListener("resize", updateVisualViewportHeight);
    window.visualViewport?.addEventListener("scroll", updateVisualViewportHeight);
    return () => {
      window.removeEventListener("resize", updateVisualViewportHeight);
      window.visualViewport?.removeEventListener("resize", updateVisualViewportHeight);
      window.visualViewport?.removeEventListener("scroll", updateVisualViewportHeight);
    };
  }, []);

  useEffect(() => {
    function closeMenu(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setMenuOpen(false);
      }
    }
    document.addEventListener("mousedown", closeMenu);
    return () => document.removeEventListener("mousedown", closeMenu);
  }, []);

  useEffect(() => {
    function updateChannelStatus(event: Event) {
      const status = (event as CustomEvent<WebSocketStatus | null>).detail;
      setChannelStatus(status);
    }
    window.addEventListener("channel-status-changed", updateChannelStatus);
    return () => window.removeEventListener("channel-status-changed", updateChannelStatus);
  }, []);

  // 設定モーダルはEscで閉じ、開いている間は背面スクロールを止める。
  useEffect(() => {
    if (!settingsOpen) {
      return;
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setSettingsOpen(false);
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [settingsOpen]);

  async function handleLogout() {
    await logout();
    setMenuOpen(false);
    setUser(null);
    setSuspended(false);
    wasAuthedRef.current = false;
    window.dispatchEvent(new Event("auth-changed"));
    navigate("/login");
  }

  const suspended = !!user && user.status === "suspended";
  const loading = user === undefined;

  return (
    <DeckProvider>
    <div className="appShell">
      <header className="topBar">
        <NavLink to="/info" className="brand">
          {serviceName}
        </NavLink>
        <nav>
          {isPrivileged(user) && !suspended ? <a href="/admin/">管理</a> : null}
          {/* 接続ステータスはログインユーザーの直左に1つだけ集約表示する。 */}
          {channelStatus ? (
            <span className={`statusPill topStatusPill ${channelStatus}`}>
              {statusLabel(channelStatus)}
            </span>
          ) : null}
          {user && !suspended ? (
            <div className="userMenu" ref={menuRef}>
              <button
                type="button"
                className="userMenuButton"
                onClick={() => setMenuOpen((current) => !current)}
                aria-label="ユーザーメニュー"
              >
                <UserAvatar displayName={user.displayName} avatarUrl={user.avatarUrl} />
                {user.ghostMode ? (
                  <span className="ghostBadge" title="ゴーストモード中（閲覧のみ）" aria-label="ゴーストモード">
                    👻
                  </span>
                ) : null}
              </button>
              {menuOpen ? (
                <div className="userDropdown">
                  <div className="userDropdownHeader">
                    <strong>{user.displayName}</strong>
                    {user.handle ? <span>@{user.handle}</span> : null}
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      setSettingsOpen(true);
                    }}
                  >
                    設定
                  </button>
                  <button type="button" onClick={handleLogout}>
                    ログアウト
                  </button>
                </div>
              ) : null}
            </div>
          ) : user === null ? (
            <NavLink to="/login" className="loginNavButton">
              ログイン
            </NavLink>
          ) : null}
        </nav>
      </header>
      <main>
        {loading ? (
          <div className="authLoading" role="status" aria-live="polite">
            読み込み中...
          </div>
        ) : suspended ? (
          // 停止ユーザーはどのURLでも「現在ご利用になれません」を表示し、
          // 通常ページ（ポーリング等）を一切マウントしない。
          <UnavailableNotice />
        ) : (
          <Outlet />
        )}
      </main>
      <PwaInstallPrompt />
      <PushEnablePrompt />
      {settingsOpen ? (
        <div
          className="settingsModalOverlay"
          role="presentation"
          onClick={() => setSettingsOpen(false)}
        >
          <div
            className="settingsModal"
            role="dialog"
            aria-modal="true"
            aria-label="設定"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="settingsModalHeader">
              <h2>設定</h2>
              <button
                type="button"
                className="settingsModalClose"
                aria-label="閉じる"
                onClick={() => setSettingsOpen(false)}
              >
                ×
              </button>
            </div>
            <div className="settingsModalBody">
              <SettingsPanel />
            </div>
          </div>
        </div>
      ) : null}
    </div>
    </DeckProvider>
  );
}

function isPrivileged(user: User | null | undefined) {
  return user?.role === "owner" || user?.role === "admin";
}

function statusLabel(status: WebSocketStatus) {
  if (status === "connecting") {
    return "接続中";
  }
  if (status === "open") {
    return "接続済み";
  }
  if (status === "reconnecting") {
    return "再接続中";
  }
  if (status === "error") {
    return "WebSocketエラー";
  }
  return "切断";
}
