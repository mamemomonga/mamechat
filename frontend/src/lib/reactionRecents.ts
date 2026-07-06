// リアクションで最近使った絵文字をブラウザ(localStorage)に記憶する。

const KEY = "reaction.recent";
const MAX = 24;

export function getRecentEmojis(): string[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) {
      return [];
    }
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter((v): v is string => typeof v === "string").slice(0, MAX);
  } catch {
    return [];
  }
}

// recordRecentEmoji は使った絵文字を先頭に入れ、重複を除いて保存する。更新後の配列を返す。
export function recordRecentEmoji(emoji: string): string[] {
  const next = [emoji, ...getRecentEmojis().filter((e) => e !== emoji)].slice(0, MAX);
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // 保存失敗は無視（プライベートモード等）。
  }
  return next;
}
