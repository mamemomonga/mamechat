import { useEffect, useRef, useState } from "react";

import { getMe } from "../lib/api";
import { whenPwaPromptResolved } from "../lib/pwaInstall";
import {
  enablePush,
  getPushConfig,
  isMacOSSafari,
  isPushEnabled,
  isPushEnvironmentSupported,
  isPushPromptDismissed,
  setPushPromptDismissed,
} from "../lib/pushNotify";

// Push通知を促すお知らせダイアログ。
// - Push利用可能な環境（VAPID設定済み・ログイン済み）で、まだ通知が無効なときだけ表示する。
// - 「このお知らせを表示しない」にチェックして閉じると以後表示しない。
// - PWA化を促すダイアログと条件がかぶった場合は、そちらが閉じられてから表示する。
export default function PushEnablePrompt() {
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [dontShowAgain, setDontShowAgain] = useState(false);
  const dontShowAgainRef = useRef(dontShowAgain);
  dontShowAgainRef.current = dontShowAgain;

  useEffect(() => {
    let cancelled = false;
    async function evaluate() {
      // macOS Safari は通知表示が不安定なため、当面この案内ダイアログは出さない。
      if (isMacOSSafari()) {
        return;
      }
      if (isPushPromptDismissed() || !isPushEnvironmentSupported()) {
        return;
      }
      const config = await getPushConfig();
      if (cancelled || !config.enabled) {
        return;
      }
      const me = await getMe().catch(() => ({ user: null }));
      if (cancelled || !me.user) {
        return; // 未ログインでは有効化できないので出さない
      }
      if (await isPushEnabled()) {
        return; // 既に有効
      }
      if (cancelled) {
        return;
      }
      // PWA案内ダイアログの決着を待ってから表示する（重なり回避）。
      whenPwaPromptResolved(() => {
        if (!cancelled) {
          setShow(true);
        }
      });
    }
    void evaluate();
    return () => {
      cancelled = true;
    };
  }, []);

  function close() {
    if (dontShowAgainRef.current) {
      setPushPromptDismissed(true);
    }
    setShow(false);
  }

  useEffect(() => {
    if (!show) {
      return;
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        close();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [show]);

  async function enable() {
    setBusy(true);
    setError("");
    try {
      await enablePush();
      setShow(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "通知の有効化に失敗しました");
    } finally {
      setBusy(false);
    }
  }

  if (!show) {
    return null;
  }

  return (
    <div className="pwaPromptOverlay" role="presentation" onClick={close}>
      <div
        className="pwaPrompt"
        role="dialog"
        aria-modal="true"
        aria-label="通知を有効にする"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="pwaPromptHeader">
          <h2 className="pwaPromptTitle">通知を有効にしよう！</h2>
          <button type="button" className="pwaPromptClose" aria-label="閉じる" onClick={close}>
            ×
          </button>
        </div>
        <p className="pwaPromptIntro">
          通知を有効にすると、チャンネルが営業中になったときにお知らせが届きます。
          各チャンネルの「通知」ボタンで、通知を受け取るチャンネルを選べます。
        </p>
        {error ? <p className="pwaPromptError">{error}</p> : null}
        <div className="pwaPromptFooter">
          <button
            type="button"
            className="pwaPromptCloseButton"
            onClick={() => void enable()}
            disabled={busy}
          >
            {busy ? "有効化中…" : "通知を有効にする"}
          </button>
          <label className="pwaPromptDontShow">
            <input
              type="checkbox"
              checked={dontShowAgain}
              onChange={(event) => setDontShowAgain(event.target.checked)}
            />
            このお知らせを表示しない
          </label>
        </div>
      </div>
    </div>
  );
}
