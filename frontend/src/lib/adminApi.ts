import type {
  AdminSession,
  AdminStats,
  AdminUser,
  Channel,
  TTSAutoDictionaryEntry,
  User,
} from "../types/chat";
import { request } from "./api";

// 管理(admin)アプリ専用のAPI。メインアプリからは import しないこと
// （これらの管理エンドポイントを通常ユーザーのJSバンドルへ含めないため）。

export function ownerLogin(input: { password: string }) {
  return request<{ user: User }>("/api/owner/login", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function getAdminStats() {
  return request<{ stats: AdminStats }>("/api/admin/stats");
}

export function listAdminSessions() {
  return request<{ sessions: AdminSession[] }>("/api/admin/sessions");
}

export function revokeAdminSession(sessionId: string) {
  return request<{ ok: boolean }>(`/api/admin/sessions/${encodeURIComponent(sessionId)}/revoke`, {
    method: "POST",
  });
}

// すべてのブラウザセッションを失効する（実行者自身のセッションも失効する）。
export function revokeAllAdminSessions() {
  return request<{ ok: boolean }>("/api/admin/sessions/revoke-all", {
    method: "POST",
  });
}

export function listAdminUsers() {
  return request<{ users: AdminUser[] }>("/api/admin/users");
}

export function updateAdminUser(
  userId: string,
  input: {
    displayName: string;
    handle?: string;
    avatarUrl?: string;
    status: string;
    role: string;
  },
) {
  return request<{ user: User }>(`/api/admin/users/${encodeURIComponent(userId)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deleteAdminUser(userId: string) {
  return request<{ ok: boolean }>(`/api/admin/users/${encodeURIComponent(userId)}`, {
    method: "DELETE",
  });
}

export function listTTSAutoDictionaryEntries() {
  return request<{ entries: TTSAutoDictionaryEntry[] }>("/api/admin/tts-dictionary");
}

export function upsertTTSAutoDictionaryEntry(input: { term: string; reading: string }) {
  return request<{ entry: TTSAutoDictionaryEntry }>("/api/admin/tts-dictionary", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteTTSAutoDictionaryEntry(termKey: string) {
  return request<{ ok: boolean }>(
    `/api/admin/tts-dictionary?termKey=${encodeURIComponent(termKey)}`,
    { method: "DELETE" },
  );
}

export function importTTSAutoDictionaryEntries(
  entries: Pick<TTSAutoDictionaryEntry, "term" | "reading">[],
) {
  return request<{ entries: TTSAutoDictionaryEntry[]; imported: number }>(
    "/api/admin/tts-dictionary/import",
    {
      method: "POST",
      body: JSON.stringify({ entries }),
    },
  );
}

// サーバ設定（サービスヘッダ画像・サーバ概要・ホワイトリスト機能）の取得・更新。
export function getServiceSettings() {
  return request<{ overview: string; headerImageUrl: string; whitelistEnabled: boolean }>(
    "/api/admin/service-settings",
  );
}

export function updateServiceOverview(overview: string) {
  return request<{ ok: boolean }>("/api/admin/service-settings/overview", {
    method: "PUT",
    body: JSON.stringify({ overview }),
  });
}

// ホワイトリスト機能のサーバ全体 有効/無効 を切り替える。
export function updateWhitelistEnabled(enabled: boolean) {
  return request<{ whitelistEnabled: boolean }>("/api/admin/service-settings/whitelist", {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  });
}

// ヘッダ画像のアップロード。multipart のため request ヘルパは使わない。
export async function uploadServiceHeader(file: File) {
  const form = new FormData();
  form.append("image", file);
  const res = await fetch("/api/admin/service-settings/header", {
    method: "POST",
    credentials: "include",
    body: form,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `アップロードに失敗しました: ${res.status}`);
  }
  return res.json() as Promise<{ headerImageUrl: string }>;
}

export function deleteServiceHeader() {
  return request<{ ok: boolean }>("/api/admin/service-settings/header", {
    method: "DELETE",
  });
}

export function createAdminChannel(input: { slug: string; title: string; description?: string }) {
  return request<{ channel: Channel }>("/api/admin/channels", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteAdminChannel(slug: string) {
  return request<{ ok: boolean }>(`/api/admin/channels/${encodeURIComponent(slug)}`, {
    method: "DELETE",
  });
}

// suspendRetentionHours: null=既定値、負値=無限、0以上=時間
export function setChannelSuspendRetention(slug: string, suspendRetentionHours: number | null) {
  return request<{ ok: boolean }>(`/api/admin/channels/${encodeURIComponent(slug)}/suspend`, {
    method: "PUT",
    body: JSON.stringify({ suspendRetentionHours }),
  });
}

// suspendGraceSeconds: null=既定値、負値=無期限（サスペンドしない）、0以上=秒
export function setChannelSuspendGrace(slug: string, suspendGraceSeconds: number | null) {
  return request<{ ok: boolean }>(`/api/admin/channels/${encodeURIComponent(slug)}/grace`, {
    method: "PUT",
    body: JSON.stringify({ suspendGraceSeconds }),
  });
}

// 「時間制限なし」チャンネルの切り替え（カウントダウンなし・開店/閉店のみ）。
export function setChannelOperatingUnlimited(slug: string, operatingUnlimited: boolean) {
  return request<{ ok: boolean }>(
    `/api/admin/channels/${encodeURIComponent(slug)}/operating-unlimited`,
    {
      method: "PUT",
      body: JSON.stringify({ operatingUnlimited }),
    },
  );
}
