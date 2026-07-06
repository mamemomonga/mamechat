// セッション失効・停止ユーザー時の誘導を一元管理する。
// - 認証エラー(401)やセッション喪失 → ログインページへ（戻り先を保存）。
// - 停止ユーザー → 「現在ご利用になれません」ページ（固定）へ。
// 戻り先は sessionStorage に保存する（OAuthの同一タブ往復で保持され、タブを閉じると消える）。

import type { NavigateFunction } from "react-router";

const RETURN_TO_KEY = "mamechat.returnTo";
export const UNAVAILABLE_PATH = "/unavailable";
export const LOGIN_PATH = "/login";

// これらのパスは戻り先として保存しない（ログイン後にここへ戻す意味がないため）。
const RETURN_EXEMPT = new Set<string>([LOGIN_PATH, UNAVAILABLE_PATH]);

// 停止ユーザーかどうか。api.ts の 401 ハンドラが誘導先を判断するために参照する。
let suspended = false;
export function setSuspended(value: boolean) {
  suspended = value;
}
export function isSuspended() {
  return suspended;
}

function currentPath() {
  return window.location.pathname + window.location.search;
}

// 現在地を戻り先として保存する（対象外パスは保存しない）。
export function saveReturnTo() {
  if (RETURN_EXEMPT.has(window.location.pathname)) {
    return;
  }
  try {
    sessionStorage.setItem(RETURN_TO_KEY, currentPath());
  } catch {
    // sessionStorage 不可でも致命的ではない。
  }
}

// 保存済みの戻り先を取り出して消す（1回限り）。
export function consumeReturnTo(): string | null {
  try {
    const value = sessionStorage.getItem(RETURN_TO_KEY);
    if (value) {
      sessionStorage.removeItem(RETURN_TO_KEY);
    }
    return value;
  } catch {
    return null;
  }
}

// セッションが無効になったときの誘導。停止ユーザーは「現在ご利用になれません」固定、
// それ以外はログインページ（戻り先を保存）へ。
export function handleUnauthorized(navigate: NavigateFunction) {
  if (suspended) {
    if (window.location.pathname !== UNAVAILABLE_PATH) {
      navigate(UNAVAILABLE_PATH, { replace: true });
    }
    return;
  }
  if (window.location.pathname === LOGIN_PATH) {
    return;
  }
  saveReturnTo();
  navigate(LOGIN_PATH, { replace: true });
}
