import type {
  AccessEntry,
  Channel,
  ChannelAccessMode,
  DeckColumn,
  LinkPreview,
  ServerToClientMessage,
  TTSVoicevoxSpeaker,
  User,
} from "../types/chat";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || "";

// API呼び出しの失敗を表す。status でセッション切れ(401)とサーバ側/ネットワーク障害
// （5xx やサーバ再起動中の接続断＝status:0）を呼び出し側で区別できるようにする。
export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

// サーバ再起動・ネットワーク断など、認証とは無関係な一時的障害かどうかを判定する。
// これらのときはログイン画面へ戻さず、状態を維持して次回リトライで復帰させる。
export function isTransientError(err: unknown): boolean {
  return err instanceof ApiError && err.status !== 401;
}

// 認証エラー(401)を検知したときに呼ぶハンドラ。メインアプリ(App)が登録し、
// ログイン/「現在ご利用になれません」ページへ誘導する。未登録なら何もしない。
let unauthorizedHandler: (() => void) | null = null;
export function setUnauthorizedHandler(fn: (() => void) | null) {
  unauthorizedHandler = fn;
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${apiBaseUrl}${path}`, {
      ...init,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...(init?.headers ?? {}),
      },
    });
  } catch {
    // fetch自体の失敗（サーバ停止・再起動中の接続断、オフライン等）。
    // status:0 は「認証切れではない一時的な障害」を意味する。
    throw new ApiError(0, "サーバに接続できませんでした");
  }
  if (!res.ok) {
    // セッションが無効（失効・期限切れ・停止）なら、登録済みハンドラで誘導する。
    if (res.status === 401) {
      unauthorizedHandler?.();
    }
    const body = await res.json().catch(() => ({}));
    throw new ApiError(res.status, body.error ?? `リクエストに失敗しました: ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export function getMe() {
  return request<{ user: User | null }>("/api/me");
}

// Deck（複数カラム）レイアウトの取得・保存（1ユーザー1スロット、手動）。
export function getDeckLayout() {
  return request<{ columns: DeckColumn[] }>("/api/me/deck");
}

export function saveDeckLayout(columns: DeckColumn[]) {
  return request<{ ok: boolean }>("/api/me/deck", {
    method: "PUT",
    body: JSON.stringify({ columns }),
  });
}

// サービス情報ページ（サービス名クリックで開く）の公開情報。overviewHtml/appInfoHtml は
// サーバ側で Markdown を整形した HTML。
export function getServiceInfo() {
  return request<{
    serviceName: string;
    version: string;
    headerImageUrl: string;
    overviewHtml: string;
    appInfoHtml: string;
  }>("/api/service-info");
}

export function getPublicConfig() {
  return request<{
    messageMaxLength: number;
    serviceName?: string;
    activePollSeconds?: number;
    beaconSeconds?: number;
    pushEnabled?: boolean;
    vapidPublicKey?: string;
    // ホワイトリスト機能のサーバ全体 有効/無効（未設定=無効）。入室許可UI・バッヂの出し分けに使う。
    whitelistEnabled?: boolean;
  }>("/api/config");
}

// Push購読をサーバに保存する。sub は PushSubscription.toJSON() の結果。
export function subscribePush(sub: PushSubscriptionJSON) {
  return request<{ ok: boolean }>("/api/push/subscribe", {
    method: "POST",
    body: JSON.stringify(sub),
  });
}

// Push購読をサーバから削除する。
export function unsubscribePush(endpoint: string) {
  return request<{ ok: boolean }>("/api/push/unsubscribe", {
    method: "POST",
    body: JSON.stringify({ endpoint }),
  });
}

// 自分の全デバイスへテスト通知を送る（動作確認用）。
export function sendTestPush() {
  return request<{ sent: number; gone: number; failed: number }>("/api/push/test", {
    method: "POST",
  });
}

// チャンネルの営業開始通知のオン/オフを切り替える。
export function setChannelNotify(slug: string, enabled: boolean) {
  return request<{ enabled: boolean }>(`/api/channels/${encodeURIComponent(slug)}/notify`, {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  });
}

export function startAtprotoLogin(input: { identifier: string }) {
  return request<{ authorizationUrl: string }>("/api/auth/atproto/start", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function startMastodonLogin(input: { instanceUrl: string }) {
  return request<{ authorizationUrl: string }>("/api/auth/mastodon/start", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function startMisskeyLogin(input: { instanceUrl: string }) {
  return request<{ authorizationUrl: string }>("/api/auth/misskey/start", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function logout() {
  return request<{ ok: boolean }>("/api/logout", { method: "POST" });
}

// 管理者/オーナーがゴーストモードのオン/オフを切り替える。
export function setGhostMode(enabled: boolean) {
  return request<{ ghostMode: boolean }>("/api/settings/ghost-mode", {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  });
}

export function listVoicevoxSpeakers() {
  return request<{ speakers: TTSVoicevoxSpeaker[] }>("/api/settings/voicevox-speakers");
}

export function updateVoicevoxSpeaker(speakerUuid: string) {
  return request<{ speaker: TTSVoicevoxSpeaker | null }>("/api/settings/voicevox-speaker", {
    method: "PUT",
    body: JSON.stringify({ speakerUuid }),
  });
}

export function previewVoicevoxSpeaker(speakerUuid: string) {
  return request<{ audioUrl: string }>("/api/settings/voicevox-speaker/preview", {
    method: "POST",
    body: JSON.stringify({ speakerUuid }),
  });
}

export function listChannels() {
  return request<{ channels: Channel[] }>("/api/channels");
}

export function createChannel(input: { slug: string; title: string; description?: string }) {
  return request<{ channel: Channel }>("/api/channels", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function getChannel(slug: string) {
  return request<{ channel: Channel }>(`/api/channels/${encodeURIComponent(slug)}`);
}

export function listMessages(slug: string, afterId?: string) {
  const query = afterId ? `?afterId=${encodeURIComponent(afterId)}` : "";
  return request<{ messages: ServerToClientMessage[] }>(
    `/api/channels/${encodeURIComponent(slug)}/messages${query}`,
  );
}

// チャンネルオーナーがチャットメッセージを一括削除する（チャンネル設定）。
export function clearChannelMessages(slug: string) {
  return request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(slug)}/messages`, {
    method: "DELETE",
  });
}

// チャンネルオーナーが機能トグル（URLリンク化・画像アップロード）や
// 入室許可制御（モード・リスト）を更新する。
export function updateChannelSettings(
  slug: string,
  settings: {
    title?: string;
    description?: string;
    urlLinkifyEnabled?: boolean;
    imageUploadEnabled?: boolean;
    noConsecutivePosts?: boolean;
    postTtlHours?: number;
    accessMode?: ChannelAccessMode;
    accessList?: AccessEntry[];
  },
) {
  return request<{ channel: Channel }>(`/api/channels/${encodeURIComponent(slug)}/settings`, {
    method: "PUT",
    body: JSON.stringify(settings),
  });
}

// オーナー/管理者がチャンネルの「準備後の削除」（保持時間）を設定する。null=既定。
export function setChannelSuspendRetention(slug: string, suspendRetentionHours: number | null) {
  return request<{ ok: boolean }>(
    `/api/channels/${encodeURIComponent(slug)}/suspend-retention`,
    {
      method: "PUT",
      body: JSON.stringify({ suspendRetentionHours }),
    },
  );
}

// オーナー/管理者がチャンネルの「オーナー退出後の準備」（猶予）を設定する。null=既定。
export function setChannelSuspendGrace(slug: string, suspendGraceSeconds: number | null) {
  return request<{ ok: boolean }>(
    `/api/channels/${encodeURIComponent(slug)}/suspend-grace`,
    {
      method: "PUT",
      body: JSON.stringify({ suspendGraceSeconds }),
    },
  );
}

// オーナー/管理者の操作で営業を開始する（準備中→営業中、durationMinutes分の営業）。
// durationMinutes は 30 / 60 / 90 / 120 / 180 のいずれか。
export function startOperating(slug: string, durationMinutes: number) {
  return request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(slug)}/operating`, {
    method: "POST",
    body: JSON.stringify({ durationMinutes }),
  });
}

// オーナー/管理者の操作で、現在からdurationMinutes分後を営業終了予定時刻にする。
// 準備中なら営業開始、営業中なら残り時間の変更として扱われる。
export function setOperatingDuration(slug: string, durationMinutes: number) {
  return request<{ ok: boolean }>(
    `/api/channels/${encodeURIComponent(slug)}/operating/duration`,
    {
      method: "POST",
      body: JSON.stringify({ durationMinutes }),
    },
  );
}

// オーナー/管理者の操作で営業時間を延長する（minutes は 5 / 15 / 30 / 60 / 120）。
export function extendOperating(slug: string, minutes: number) {
  return request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(slug)}/operating/extend`, {
    method: "POST",
    body: JSON.stringify({ minutes }),
  });
}

// オーナー/管理者の操作で即座に準備中へ移行する（営業の途中終了）。
export function suspendChannelNow(slug: string) {
  return request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(slug)}/rest`, {
    method: "POST",
  });
}

// 「時間制限なし」チャンネルの開店（準備中→営業中、終了予定なし）。
export function openChannel(slug: string) {
  return request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(slug)}/operating/open`, {
    method: "POST",
  });
}

// 投稿に貼られたURLのOGP（リンクプレビュー）を取得する。取得不可でも空で返る。
export function fetchLinkPreview(url: string) {
  return request<LinkPreview>(`/api/og?url=${encodeURIComponent(url)}`);
}

// 入室許可リストに登録する1件を、サーバ側で各SNSへ問い合わせて安定IDに解決する。
export function resolveAccessEntry(slug: string, entry: string) {
  return request<{ entry: AccessEntry }>(
    `/api/channels/${encodeURIComponent(slug)}/access/resolve`,
    {
      method: "POST",
      body: JSON.stringify({ entry }),
    },
  );
}

// 画像/動画素材をアップロードし、投稿で参照するトークンを得る。
// 静止画はリサイズ済みJPEG(Blob)、アニメーション素材はGIF/APNG/MP4の生ファイルを渡す。
// multipart のため Content-Type はブラウザに任せる（request ヘルパは使わない）。
export async function uploadChannelImage(slug: string, blob: Blob, filename = "image.jpg") {
  const form = new FormData();
  form.append("image", blob, filename);
  const res = await fetch(`${apiBaseUrl}/api/channels/${encodeURIComponent(slug)}/images`, {
    method: "POST",
    credentials: "include",
    body: form,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `アップロードに失敗しました: ${res.status}`);
  }
  return res.json() as Promise<{
    imageToken: string;
    url: string;
    width: number;
    height: number;
    mediaKind: "image" | "video";
  }>;
}

// チャンネルオーナーがチャンネルを削除する（チャンネル設定）。
export function deleteChannel(slug: string) {
  return request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(slug)}`, {
    method: "DELETE",
  });
}

// 管理者がチャンネル閲覧中にメッセージを削除する。メインアプリ(ChannelPage)で利用するため
// ここに残す。サーバー側で admin 権限を要求する。
export function deleteAdminMessage(messageId: string) {
  return request<{ ok: boolean }>(`/api/admin/messages/${encodeURIComponent(messageId)}`, {
    method: "DELETE",
  });
}

// 「ここから読み上げる」用に、1メッセージ分の読み上げ音声パートURLを取得する。
// 読み上げ不可（無効・話者なし等）の場合は parts が空で返る。
export function fetchMessageTTS(slug: string, messageId: string) {
  return request<{ parts: { partIndex: number; audioUrl: string }[] }>(
    `/api/channels/${encodeURIComponent(slug)}/messages/${encodeURIComponent(messageId)}/tts`,
  );
}

// チャンネルページでメッセージを削除する。サーバー側で投稿者本人・チャンネルオーナー・
// 管理者のみ許可する。
export function deleteChannelMessage(slug: string, messageId: string) {
  return request<{ ok: boolean }>(
    `/api/channels/${encodeURIComponent(slug)}/messages/${encodeURIComponent(messageId)}`,
    { method: "DELETE" },
  );
}
