// チャンネルが「準備中」のとき、オーナーへ「営業中にしよう」と促す噴きだしの
// 「今後このメッセージを表示しない」状態をブラウザ(localStorage)に保存する。
// 他のお知らせダイアログ（PWA案内など）と同じ方式。

const DISMISS_KEY = "operatingHintHiddenV1";

export function isOperatingHintDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISS_KEY) === "1";
  } catch {
    return false;
  }
}

export function setOperatingHintDismissed(dismissed: boolean): void {
  try {
    if (dismissed) {
      localStorage.setItem(DISMISS_KEY, "1");
    } else {
      localStorage.removeItem(DISMISS_KEY);
    }
  } catch {
    // プライベートモード等で localStorage が使えない場合は保存しない。
  }
}
