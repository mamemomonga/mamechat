// Web Push（通知）のクライアント側ヘルパ。対応環境の判定・購読・解除をまとめる。
import { getPublicConfig, subscribePush, unsubscribePush } from "./api";
import { isStandalone } from "./pwaInstall";

export type PushConfig = { enabled: boolean; vapidPublicKey: string };

let cachedConfig: PushConfig | null = null;

// getPushConfig は /api/config から Push 設定（有効可否・VAPID公開鍵）を取得しキャッシュする。
export async function getPushConfig(): Promise<PushConfig> {
  if (cachedConfig) {
    return cachedConfig;
  }
  try {
    const config = await getPublicConfig();
    cachedConfig = {
      enabled: Boolean(config.pushEnabled) && Boolean(config.vapidPublicKey),
      vapidPublicKey: config.vapidPublicKey ?? "",
    };
  } catch {
    cachedConfig = { enabled: false, vapidPublicKey: "" };
  }
  return cachedConfig;
}

// Push通知の案内ダイアログ「このお知らせを表示しない」の保存キー。
const PUSH_PROMPT_DISMISS_KEY = "pushPromptHiddenV1";

export function isPushPromptDismissed(): boolean {
  try {
    return localStorage.getItem(PUSH_PROMPT_DISMISS_KEY) === "1";
  } catch {
    return false;
  }
}

export function setPushPromptDismissed(dismissed: boolean): void {
  try {
    if (dismissed) {
      localStorage.setItem(PUSH_PROMPT_DISMISS_KEY, "1");
    } else {
      localStorage.removeItem(PUSH_PROMPT_DISMISS_KEY);
    }
  } catch {
    // localStorage が使えない場合は保存しない。
  }
}

function isIOS(): boolean {
  const ua = navigator.userAgent || "";
  const isIPadOS = /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
  return /iPhone|iPad|iPod/.test(ua) || isIPadOS;
}

// macOS の Safari系（Safari本体・Dockに追加したstandalone Web App）か。
// iPadOS の Safari は Mac の UA を返すため maxTouchPoints で除外する。
// 注意: 「Dockに追加」した standalone Web App は UA から Version/ や Safari/
// トークンが落ちることがあるため、それらの有無では判定できない。
// macOS かつ WebKit で、Chromium/Firefox 系でなければ Safari系とみなす。
export function isMacOSSafari(): boolean {
  const ua = navigator.userAgent || "";
  const isMac = /Macintosh|Mac OS X/.test(ua) && !(navigator.maxTouchPoints > 1);
  const isWebKit = /AppleWebKit/.test(ua);
  const isOtherBrowser = /Chrome\/|Chromium|CriOS|Edg\/|EdgiOS|Firefox\/|FxiOS|OPR\/|OPT\//.test(ua);
  return isMac && isWebKit && !isOtherBrowser;
}

// ブラウザにPush関連APIが揃っているか。
export function isPushApiAvailable(): boolean {
  return (
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator &&
    typeof window !== "undefined" &&
    "PushManager" in window &&
    "Notification" in window
  );
}

// 環境的にPushを使えるか。
// iOSはホーム画面に追加したPWA（standalone）でのみ利用可能。
// Android(Chrome/Firefox)・デスクトップ(Chrome/Firefox/Edge)はタブでも可能。
// VAPID未設定かどうかは getPushConfig().enabled で別途判定する。
export function isPushEnvironmentSupported(): boolean {
  if (!isPushApiAvailable()) {
    return false;
  }
  if (isIOS() && !isStandalone()) {
    return false;
  }
  return true;
}

async function getExistingRegistration(): Promise<ServiceWorkerRegistration | undefined> {
  if (!("serviceWorker" in navigator)) {
    return undefined;
  }
  return navigator.serviceWorker.getRegistration();
}

async function ensureRegistration(): Promise<ServiceWorkerRegistration> {
  const existing = await getExistingRegistration();
  const reg = existing ?? (await navigator.serviceWorker.register("/sw.js"));
  await navigator.serviceWorker.ready;
  return reg;
}

// 現在このブラウザでPush通知が有効（許可済み＋購読済み）か。
export async function isPushEnabled(): Promise<boolean> {
  if (!isPushApiAvailable() || Notification.permission !== "granted") {
    return false;
  }
  const reg = await getExistingRegistration();
  if (!reg) {
    return false;
  }
  const sub = await reg.pushManager.getSubscription();
  return Boolean(sub);
}

function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const normalized = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(normalized);
  const output = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i += 1) {
    output[i] = raw.charCodeAt(i);
  }
  return output;
}

// enablePush は通知許可を求め、Push購読を作成してサーバに保存する。
export async function enablePush(): Promise<void> {
  const config = await getPushConfig();
  if (!config.enabled) {
    throw new Error("この環境ではPush通知が利用できません");
  }
  const permission = await Notification.requestPermission();
  if (permission !== "granted") {
    throw new Error("通知が許可されませんでした");
  }
  const reg = await ensureRegistration();
  let sub = await reg.pushManager.getSubscription();
  if (!sub) {
    sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(config.vapidPublicKey),
    });
  }
  await subscribePush(sub.toJSON());
}

// disablePush は購読を解除し、サーバからも削除する。
export async function disablePush(): Promise<void> {
  const reg = await getExistingRegistration();
  if (!reg) {
    return;
  }
  const sub = await reg.pushManager.getSubscription();
  if (!sub) {
    return;
  }
  const { endpoint } = sub;
  await sub.unsubscribe().catch(() => {});
  await unsubscribePush(endpoint).catch(() => {});
}
