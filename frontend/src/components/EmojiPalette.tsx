import { useEffect, useRef, useState } from "react";

type Props = {
  onSelect: (emoji: string) => void;
  onClose: () => void;
  // 追加ボタンの上下どちらへ開くか（スクロール枠での見切れを避ける）。
  direction?: "up" | "down";
};

// アプリのダーク配色に合わせて emoji-mart（Shadow DOM）へ渡すCSS変数。
// emoji-mart は RGB を "R, G, B" の数値並びで受け取る。
const PICKER_VARS: Record<string, string> = {
  "--rgb-background": "31, 36, 42", // --surface #1f242a
  "--rgb-input": "24, 28, 32", // --surface-sunken #181c20
  "--rgb-color": "216, 221, 226", // --text #d8dde2
  "--rgb-accent": "52, 211, 195", // --accent-bright #34d3c3
  "--color-border": "rgba(74, 82, 92, 0.55)", // --border-strong 半透明
  "--color-border-over": "rgba(74, 82, 92, 0.9)",
  "--border-radius": "10px",
  "--font-size": "14px",
};

// EmojiPalette は検索・カテゴリ・スキントーン・よく使う絵文字を備えたフルスペックの
// 絵文字ピッカー。emoji-mart のフレームワーク非依存 Picker（Shadow DOM のカスタム要素）を
// マウントして使う。データ・i18n はローカルからバンドルするため外部通信は発生しない
// （CSP script-src/connect-src 'self' に適合）。データは初回オープン時に動的読み込みする。
export default function EmojiPalette({ onSelect, onClose, direction = "up" }: Props) {
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [ready, setReady] = useState(false);
  // 最新のコールバックを参照し、マウント後の選択で古いクロージャを使わないようにする。
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // 枠外クリック・Escape で閉じる。Shadow DOM 内クリックはホスト要素へリターゲットされる
  // ため、wrap 要素に contains 判定で内側扱いになる。
  useEffect(() => {
    function onDown(event: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(event.target as Node)) {
        onCloseRef.current();
      }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onCloseRef.current();
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    let picker: HTMLElement | null = null;
    void (async () => {
      // データと本体は重いので、パレットを開いたときにだけ動的読み込みする。
      const [emojiMart, dataMod, i18nMod] = await Promise.all([
        import("emoji-mart"),
        import("@emoji-mart/data"),
        import("@emoji-mart/data/i18n/ja.json"),
      ]);
      if (cancelled || !hostRef.current) {
        return;
      }
      const Picker = emojiMart.Picker as unknown as new (props: Record<string, unknown>) => HTMLElement;
      picker = new Picker({
        data: dataMod.default,
        i18n: i18nMod.default, // 日本語UI（カテゴリ名・検索プレースホルダ等）をローカルから供給
        locale: "ja",
        theme: "dark",
        set: "native",
        previewPosition: "none",
        skinTonePosition: "search",
        navPosition: "top",
        perLine: 8,
        emojiButtonSize: 34,
        emojiSize: 22,
        maxFrequentRows: 2,
        autoFocus: true,
        dynamicWidth: false,
        onEmojiSelect: (emoji: { native?: string }) => {
          if (emoji?.native) {
            onSelectRef.current(emoji.native);
          }
        },
      });
      for (const [key, value] of Object.entries(PICKER_VARS)) {
        picker.style.setProperty(key, value);
      }
      hostRef.current.appendChild(picker);
      setReady(true);
    })();
    return () => {
      cancelled = true;
      picker?.remove();
    };
  }, []);

  return (
    <div className={`emojiPalette ${direction}`} ref={wrapRef} role="dialog" aria-label="絵文字を選ぶ">
      {!ready ? <div className="emojiPaletteLoading">絵文字を読み込み中…</div> : null}
      <div className="emojiPaletteHost" ref={hostRef} />
    </div>
  );
}
