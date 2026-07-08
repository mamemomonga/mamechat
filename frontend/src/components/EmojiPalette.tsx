import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

type Props = {
  onSelect: (emoji: string) => void;
  onClose: () => void;
  // パレットを寄せる基準となる「リアクション追加」ボタン要素。
  anchorEl: HTMLElement | null;
};

// 画面端の余白、アンカーとの隙間、パレットの最低高さ（px）。
const MARGIN = 8;
const GAP = 6;
const MIN_HEIGHT = 240;
// ピッカーの実測前に使う概算サイズ（読み込み中の仮配置用）。
const EST_WIDTH = 300;
const EST_HEIGHT = 400;

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

// アンカー矩形とパレットサイズから、常に画面内に収まる固定位置を求める。
// 下に十分な余白があれば下、無ければ上へフリップし、はみ出す分は画面内へクランプする。
function computePosition(
  anchor: DOMRect,
  width: number,
  naturalHeight: number,
): { top: number; left: number; height: number } {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const spaceBelow = vh - anchor.bottom - GAP - MARGIN;
  const spaceAbove = anchor.top - GAP - MARGIN;
  // 下に自然な高さが収まる、または下の方が広ければ下に置く。
  const placeBelow = spaceBelow >= naturalHeight || spaceBelow >= spaceAbove;
  const avail = placeBelow ? spaceBelow : spaceAbove;
  const height = Math.max(MIN_HEIGHT, Math.min(naturalHeight, Math.max(MIN_HEIGHT, avail)));
  let top = placeBelow ? anchor.bottom + GAP : anchor.top - GAP - height;
  top = Math.min(Math.max(MARGIN, top), Math.max(MARGIN, vh - MARGIN - height));
  const left = Math.min(Math.max(MARGIN, anchor.left), Math.max(MARGIN, vw - MARGIN - width));
  return { top, left, height };
}

// EmojiPalette は検索・カテゴリ・スキントーン・よく使う絵文字を備えたフルスペックの
// 絵文字ピッカー。emoji-mart のフレームワーク非依存 Picker（Shadow DOM のカスタム要素）を
// マウントして使う。データ・i18n はローカルからバンドルするため外部通信は発生しない
// （CSP script-src/connect-src 'self' に適合）。データは初回オープン時に動的読み込みする。
//
// 位置決めはメッセージ内の絶対配置ではなく、body直下へポータルした固定配置で行う。
// これにより、投稿が少なく上端に近いときの見切れや、スクロール枠のはみ出しクリップを避け、
// 常に画面内に収まる位置へ寄せる。スクロール・リサイズ時は追従して再配置する。
export default function EmojiPalette({ onSelect, onClose, anchorEl }: Props) {
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const hostRef = useRef<HTMLDivElement | null>(null);
  const pickerRef = useRef<HTMLElement | null>(null);
  // ピッカーの自然な高さ（一度だけ実測してキャッシュ）。
  const naturalHeightRef = useRef(0);
  const anchorRef = useRef(anchorEl);
  anchorRef.current = anchorEl;
  const [ready, setReady] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number }>(() => {
    if (!anchorEl) {
      return { top: MARGIN, left: MARGIN };
    }
    const { top, left } = computePosition(anchorEl.getBoundingClientRect(), EST_WIDTH, EST_HEIGHT);
    return { top, left };
  });
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const reposition = useCallback(() => {
    const anchor = anchorRef.current;
    const wrap = wrapRef.current;
    if (!anchor || !wrap) {
      return;
    }
    const rect = anchor.getBoundingClientRect();
    // アンカーが画面外へスクロールしたら閉じる（浮いたパレットが残らないように）。
    if (rect.bottom < 0 || rect.top > window.innerHeight) {
      onCloseRef.current();
      return;
    }
    const picker = pickerRef.current;
    if (picker && !naturalHeightRef.current) {
      picker.style.height = "";
      naturalHeightRef.current = picker.offsetHeight || EST_HEIGHT;
    }
    const naturalHeight = naturalHeightRef.current || EST_HEIGHT;
    const width = picker?.offsetWidth || wrap.offsetWidth || EST_WIDTH;
    const { top, left, height } = computePosition(rect, width, naturalHeight);
    // 収まらないときはピッカー自体を縮め、内部スクロールに任せる。
    if (picker) {
      picker.style.height = height < naturalHeight ? `${height}px` : "";
    }
    setPos({ top, left });
  }, []);

  // 外側クリック/Escで閉じる。スクロール（キャプチャでメッセージ枠のスクロールも拾う）・
  // リサイズでは追従して再配置する。
  useEffect(() => {
    function onDown(event: MouseEvent) {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target)) {
        return;
      }
      // アンカー（追加ボタン）自身のクリックはトグルに任せ、ここでは閉じない。
      if (anchorRef.current?.contains(target)) {
        return;
      }
      onCloseRef.current();
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onCloseRef.current();
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
    };
  }, [reposition]);

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
      pickerRef.current = picker;
      setReady(true);
    })();
    return () => {
      cancelled = true;
      picker?.remove();
      pickerRef.current = null;
    };
  }, []);

  // 準備完了後（＝実サイズ確定後）に、実測サイズで配置し直す。
  useLayoutEffect(() => {
    reposition();
  }, [ready, reposition]);

  return createPortal(
    <div
      className="emojiPalette"
      ref={wrapRef}
      role="dialog"
      aria-label="絵文字を選ぶ"
      style={{ top: pos.top, left: pos.left }}
    >
      {!ready ? <div className="emojiPaletteLoading">絵文字を読み込み中…</div> : null}
      <div className="emojiPaletteHost" ref={hostRef} />
    </div>,
    document.body,
  );
}
