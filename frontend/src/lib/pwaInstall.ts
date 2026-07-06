// PWA（ホーム画面/Dock/アプリ）へのインストール手順を、OS・ブラウザごとに出し分ける。
// 対応: iOS+Safari / iOS+Chrome / Android+Chrome / Android+Firefox / Android+Edge /
//       macOS+Safari / macOS+Chrome / macOS+Edge / Windows+Chrome / Windows+Edge。
// これら以外（デスクトップのFirefoxなど、インストール手段がない環境）では null を返し、
// ダイアログを出さない。

export type PwaInstallGuide = {
  // 端末・ブラウザの識別子（ローカル保存のキーなどには使わないが、テスト/デバッグ用）。
  key: string;
  title: string;
  intro: string;
  steps: string[];
  // 追加の補足（任意）。
  note?: string;
};

type OS = "ios" | "android" | "macos" | "windows" | "other";
type Browser = "safari" | "chrome" | "edge" | "firefox" | "other";

// 「このお知らせを表示しない」を保存するキー。
const DISMISS_KEY = "pwaInstallHiddenV1";

// PWA案内ダイアログの決着（非表示確定 or 閉じた）を、Push通知ダイアログへ伝えるための
// 軽量な調停。イベントのタイミング競合を避けるためモジュール変数で管理する。
let pwaPromptResolved = false;
const pwaPromptWaiters: Array<() => void> = [];

// resolvePwaPrompt は PWA案内ダイアログが決着したことを記録し、待機中のコールバックを呼ぶ。
export function resolvePwaPrompt(): void {
  if (pwaPromptResolved) {
    return;
  }
  pwaPromptResolved = true;
  for (const waiter of pwaPromptWaiters.splice(0)) {
    waiter();
  }
}

// whenPwaPromptResolved は PWA案内ダイアログの決着後にコールバックを呼ぶ（既に決着済みなら即時）。
export function whenPwaPromptResolved(callback: () => void): void {
  if (pwaPromptResolved) {
    callback();
  } else {
    pwaPromptWaiters.push(callback);
  }
}

export function isDismissedForever(): boolean {
  try {
    return localStorage.getItem(DISMISS_KEY) === "1";
  } catch {
    return false;
  }
}

export function setDismissedForever(dismissed: boolean): void {
  try {
    if (dismissed) {
      localStorage.setItem(DISMISS_KEY, "1");
    } else {
      localStorage.removeItem(DISMISS_KEY);
    }
  } catch {
    // プライベートモード等で localStorage が使えない場合は保存しない（次回また表示される）。
  }
}

// 既にPWA（ホーム画面/Dock/インストール済みアプリ）として開かれているか。
export function isStandalone(): boolean {
  const mq = window.matchMedia?.("(display-mode: standalone)").matches;
  // iOS Safari は navigator.standalone を使う。
  const iosStandalone = (navigator as { standalone?: boolean }).standalone === true;
  return Boolean(mq) || iosStandalone;
}

type RelatedApp = { platform?: string; url?: string; id?: string };

// isAlreadyInstalled は、ブラウザのタブで開いていても、このPWAが既に
// インストール（ホーム画面に追加）済みかを判定する。
// Android Chrome / デスクトップChromium の getInstalledRelatedApps() を使う。
// この API が無い環境（iOS Safari 等）は判定できないため false を返す
// （呼び出し側は従来どおりダイアログを表示する）。
export async function isAlreadyInstalled(): Promise<boolean> {
  const nav = navigator as Navigator & {
    getInstalledRelatedApps?: () => Promise<RelatedApp[]>;
  };
  if (typeof nav.getInstalledRelatedApps !== "function") {
    return false;
  }
  try {
    const apps = await nav.getInstalledRelatedApps();
    // manifest の related_applications に自身（platform: "webapp"）を登録しており、
    // インストール済みならそれが返る。
    return apps.some((app) => app.platform === "webapp");
  } catch {
    return false;
  }
}

function detectOS(ua: string): OS {
  // iPadOS 13+ は Mac の UA を返すため、タッチ対応の Mac は iOS(iPadOS) とみなす。
  const isIPadOS = /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
  if (/iPhone|iPad|iPod/.test(ua) || isIPadOS) {
    return "ios";
  }
  if (/Android/.test(ua)) {
    return "android";
  }
  if (/Windows/.test(ua)) {
    return "windows";
  }
  if (/Macintosh|Mac OS X/.test(ua)) {
    return "macos";
  }
  return "other";
}

function detectBrowser(ua: string): Browser {
  // 判定順が重要（Edge/Chrome は Safari 文字列も含むため先に判定する）。
  if (/Edg\/|EdgiOS|EdgA/.test(ua)) {
    return "edge";
  }
  if (/Firefox\/|FxiOS/.test(ua)) {
    return "firefox";
  }
  if (/CriOS/.test(ua) || (/Chrome\//.test(ua) && !/OPR\//.test(ua))) {
    return "chrome";
  }
  if (/Safari/.test(ua) && /Version\//.test(ua)) {
    return "safari";
  }
  return "other";
}

// getPwaInstallGuide は現在の環境向けのインストール手順を返す。
// 既にPWAで開いている・手順が用意できない環境では null（ダイアログを出さない）。
export function getPwaInstallGuide(): PwaInstallGuide | null {
  if (typeof navigator === "undefined") {
    return null;
  }
  if (isStandalone()) {
    return null;
  }
  const ua = navigator.userAgent || "";
  const os = detectOS(ua);
  const browser = detectBrowser(ua);
  return guideFor(os, browser);
}

function guideFor(os: OS, browser: Browser): PwaInstallGuide | null {
  // --- iOS ---
  if (os === "ios" && browser === "safari") {
    return {
      key: "ios-safari",
      title: "ホーム画面に追加しよう！",
      intro: "ホーム画面に追加すると、アプリのように全画面で使えて便利です。",
      steps: [
        "画面下部の「共有」ボタン（□に↑のアイコン）をタップ",
        "メニューが見つからなければ「表示を増やす」をタップ",
        "「ホーム画面に追加」をタップ",
        "「Webブラウザで開く」をオンにして、右上の「追加」をタップ",
      ],
    };
  }
  if (os === "ios" && browser === "chrome") {
    return {
      key: "ios-chrome",
      title: "ホーム画面に追加しよう！",
      intro: "ホーム画面に追加すると、アプリのように全画面で使えて便利です。",
      steps: [
        "画面右上の「共有」ボタン（□に↑のアイコン）をタップ",
        "「ホーム画面に追加」をタップ",
        "右上の「追加」をタップ",
      ],
    };
  }

  // --- Android ---
  if (os === "android" && browser === "chrome") {
    return {
      key: "android-chrome",
      title: "アプリをインストールしよう！",
      intro: "インストールすると、ホーム画面から独立したアプリのように使えます。",
      steps: [
        "画面右上の「⋮」メニューをタップ",
        "「アプリをインストール」または「ホーム画面に追加」をタップ",
        "案内に従って「インストール」／「追加」をタップ",
      ],
    };
  }
  if (os === "android" && browser === "edge") {
    return {
      key: "android-edge",
      title: "アプリをインストールしよう！",
      intro: "インストールすると、ホーム画面から独立したアプリのように使えます。",
      steps: [
        "画面下部または右上の「⋯」メニューをタップ",
        "「アプリ」→「このサイトをアプリとしてインストール」をタップ",
        "「インストール」をタップ",
      ],
    };
  }
  if (os === "android" && browser === "firefox") {
    return {
      key: "android-firefox",
      title: "ホーム画面に追加しよう！",
      intro: "ホーム画面に追加すると、アプリのように使えて便利です。",
      steps: [
        "画面右上の「⋮」メニューをタップ",
        "「アプリをインストール」または「ホーム画面へ追加」をタップ",
        "「追加」をタップ",
      ],
    };
  }

  // --- macOS ---
  if (os === "macos" && browser === "safari") {
    return {
      key: "macos-safari",
      title: "Dockに追加しよう！",
      intro: "Dockに追加すると、アプリのように独立したウィンドウで使えて便利です。",
      steps: [
        "メニューバーの「ファイル」をクリック",
        "「Dockに追加」をクリック",
        "「追加」をクリック",
      ],
      note: "Safari 17以降で利用できます。",
    };
  }
  if (os === "macos" && browser === "chrome") {
    return {
      key: "macos-chrome",
      title: "アプリとしてインストールしよう！",
      intro: "インストールすると、独立したウィンドウでアプリのように使えて便利です。",
      steps: [
        "アドレスバー右側のインストールアイコン（画面に↓のアイコン）をクリック",
        "見つからなければ右上の「⋮」→「保存して共有」→「ページをインストール」をクリック",
        "「インストール」をクリック",
      ],
    };
  }
  if (os === "macos" && browser === "edge") {
    return {
      key: "macos-edge",
      title: "アプリとしてインストールしよう！",
      intro: "インストールすると、独立したウィンドウでアプリのように使えて便利です。",
      steps: [
        "右上の「⋯」メニューをクリック",
        "「アプリ」→「このサイトをアプリとしてインストール」をクリック",
        "「インストール」をクリック",
      ],
    };
  }

  // --- Windows ---
  if (os === "windows" && browser === "chrome") {
    return {
      key: "windows-chrome",
      title: "アプリとしてインストールしよう！",
      intro: "インストールすると、独立したウィンドウでアプリのように使えて便利です。",
      steps: [
        "アドレスバー右側のインストールアイコン（画面に↓のアイコン）をクリック",
        "見つからなければ右上の「⋮」→「保存して共有」→「ページをインストール」をクリック",
        "「インストール」をクリック",
      ],
    };
  }
  if (os === "windows" && browser === "edge") {
    return {
      key: "windows-edge",
      title: "アプリとしてインストールしよう！",
      intro: "インストールすると、独立したウィンドウでアプリのように使えて便利です。",
      steps: [
        "右上の「⋯」メニューをクリック",
        "「アプリ」→「このサイトをアプリとしてインストール」をクリック",
        "「インストール」をクリック",
      ],
    };
  }

  // それ以外（デスクトップのFirefox等、インストール手段がない環境）はダイアログを出さない。
  return null;
}
