import { useEffect, useRef, useState } from "react";

import { getRecentEmojis } from "../lib/reactionRecents";

// 定番のリアクション絵文字。
const CURATED = [
  "👍", "❤️", "😂", "🎉", "😮", "😢", "🙏", "🔥",
  "👀", "✅", "💯", "😅", "🚀", "🙌", "🥳", "😭",
];

type Props = {
  onSelect: (emoji: string) => void;
  onClose: () => void;
  // 追加ボタンの上下どちらへ開くか（スクロール枠での見切れを避ける）。
  direction?: "up" | "down";
};

// EmojiPalette は「最近使った絵文字＋定番セット＋任意入力」の絵文字パレット。
export default function EmojiPalette({ onSelect, onClose, direction = "up" }: Props) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [custom, setCustom] = useState("");
  const [recents] = useState(getRecentEmojis);

  useEffect(() => {
    function onDown(event: MouseEvent) {
      if (ref.current && !ref.current.contains(event.target as Node)) {
        onClose();
      }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  function submitCustom() {
    const emoji = custom.trim();
    // コードポイント8個以内の1絵文字を想定（ZWJ連結を許容しつつ暴走を防ぐ）。
    if (!emoji || [...emoji].length > 8) {
      return;
    }
    onSelect(emoji);
  }

  return (
    <div className={`emojiPalette ${direction}`} ref={ref} role="dialog" aria-label="絵文字を選ぶ">
      {recents.length > 0 ? (
        <>
          <div className="emojiPaletteLabel">最近使った絵文字</div>
          <div className="emojiPaletteGrid">
            {recents.map((emoji) => (
              <button
                type="button"
                key={`recent-${emoji}`}
                className="emojiPaletteItem"
                onClick={() => onSelect(emoji)}
              >
                {emoji}
              </button>
            ))}
          </div>
        </>
      ) : null}
      <div className="emojiPaletteLabel">絵文字</div>
      <div className="emojiPaletteGrid">
        {CURATED.map((emoji) => (
          <button
            type="button"
            key={emoji}
            className="emojiPaletteItem"
            onClick={() => onSelect(emoji)}
          >
            {emoji}
          </button>
        ))}
      </div>
      <form
        className="emojiPaletteCustom"
        onSubmit={(event) => {
          event.preventDefault();
          submitCustom();
        }}
      >
        <input
          value={custom}
          onChange={(event) => setCustom(event.target.value)}
          placeholder="絵文字を入力"
          aria-label="任意の絵文字を入力"
          maxLength={16}
        />
        <button type="submit" className="buttonLink secondaryLink" disabled={!custom.trim()}>
          追加
        </button>
      </form>
    </div>
  );
}
