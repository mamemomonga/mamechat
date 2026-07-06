import { useEffect, useRef, useState } from "react";

import {
  getPwaInstallGuide,
  isAlreadyInstalled,
  isDismissedForever,
  resolvePwaPrompt,
  setDismissedForever,
  type PwaInstallGuide,
} from "../lib/pwaInstall";

// PWAインストール手順のお知らせダイアログ。
// - PWA化できるブラウザで、まだPWAとして開いていないときだけ表示する。
// - 「このお知らせを表示しない」にチェックして閉じると、以後そのブラウザでは表示しない。
// - 非対応ブラウザ（手順を用意できない環境）では表示しない。
export default function PwaInstallPrompt() {
  const [guide, setGuide] = useState<PwaInstallGuide | null>(null);
  const [dontShowAgain, setDontShowAgain] = useState(false);
  // Escキーのハンドラから最新のチェック状態を参照するためのref。
  const dontShowAgainRef = useRef(dontShowAgain);
  dontShowAgainRef.current = dontShowAgain;
  // このダイアログの決着（非表示確定 or 閉じた）を一度だけ通知するためのフラグ。
  // Push通知ダイアログはこの完了を待ってから表示される。
  const doneDispatched = useRef(false);

  function dispatchDone() {
    if (doneDispatched.current) {
      return;
    }
    doneDispatched.current = true;
    resolvePwaPrompt();
  }

  useEffect(() => {
    if (isDismissedForever()) {
      dispatchDone();
      return;
    }
    const candidate = getPwaInstallGuide();
    if (!candidate) {
      dispatchDone();
      return;
    }
    let cancelled = false;
    // 既にインストール済み（ホーム画面に追加済み）なら表示しない。
    void isAlreadyInstalled().then((installed) => {
      if (cancelled) {
        return;
      }
      if (installed) {
        dispatchDone();
      } else {
        setGuide(candidate);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!guide) {
      return;
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        if (dontShowAgainRef.current) {
          setDismissedForever(true);
        }
        setGuide(null);
        dispatchDone();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [guide]);

  function close() {
    if (dontShowAgain) {
      setDismissedForever(true);
    }
    setGuide(null);
    dispatchDone();
  }

  if (!guide) {
    return null;
  }

  return (
    <div className="pwaPromptOverlay" role="presentation" onClick={close}>
      <div
        className="pwaPrompt"
        role="dialog"
        aria-modal="true"
        aria-label={guide.title}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="pwaPromptHeader">
          <h2 className="pwaPromptTitle">{guide.title}</h2>
          <button type="button" className="pwaPromptClose" aria-label="閉じる" onClick={close}>
            ×
          </button>
        </div>
        <p className="pwaPromptIntro">{guide.intro}</p>
        <ol className="pwaPromptSteps">
          {guide.steps.map((step, index) => (
            <li key={index}>{step}</li>
          ))}
        </ol>
        {guide.note ? <p className="pwaPromptNote">{guide.note}</p> : null}
        <div className="pwaPromptFooter">
          <button type="button" className="pwaPromptCloseButton" onClick={close}>
            閉じる
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
