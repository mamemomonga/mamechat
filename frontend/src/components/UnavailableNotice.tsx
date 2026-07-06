import { logout } from "../lib/api";

// 停止（suspended）ユーザーに表示する「現在ご利用になれません」通知。
// このセッションが有効な限り表示され続ける。別アカウントで入り直すためのログアウトのみ可能。
export default function UnavailableNotice() {
  async function handleLogout() {
    try {
      await logout();
    } catch {
      // 失敗しても続行（クッキーは失効しているはず）。
    }
    window.dispatchEvent(new Event("auth-changed"));
    // ログイン画面へ。停止セッションを消してから入り直せるようにする。
    window.location.assign("/login");
  }

  return (
    <section className="unavailablePage">
      <div className="unavailableCard">
        <h1>現在ご利用になれません</h1>
        <p>このアカウントは現在ご利用いただけません。</p>
        <p className="muted">
          状態が変更された場合は、一度ログアウトして入り直すと反映されます。
        </p>
        <button type="button" className="unavailableLogout" onClick={handleLogout}>
          ログアウト
        </button>
      </div>
    </section>
  );
}
