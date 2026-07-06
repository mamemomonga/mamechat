// 開発用モックデータ。prodビルドには含まれない（main.tsx の import.meta.env.DEV
// ガード内から動的importされ、Rollupがprodでブランチごと除去するため）。
import type { AccessEntry, Channel, ServerToClientMessage, User } from "../types/chat";
import { SAMPLE_MP4_DATA_URI } from "./sampleVideo";

export type MockRole = "user" | "owner" | "admin";

export type MockScenario = {
  role: MockRole;
  // チャンネルページでサスペンドのカウントダウンバナーを表示する。
  suspendCountdown: boolean;
  // チャンネルを準備中（サスペンド済み）の状態で開く。
  suspended: boolean;
};

// VITE_MOCK_USER とURLクエリからシナリオを決める。
// 例: VITE_MOCK_USER=admin / ?mockRole=owner / ?mockSuspend=1 / ?mockSuspended=1
export function resolveScenario(): MockScenario {
  const params = new URLSearchParams(window.location.search);
  const envValue = String(import.meta.env.VITE_MOCK_USER ?? "").toLowerCase();
  const queryRole = (params.get("mockRole") ?? "").toLowerCase();
  const raw = queryRole || envValue;
  const role: MockRole = raw === "owner" ? "owner" : raw === "admin" ? "admin" : "user";
  return {
    role,
    suspendCountdown: params.get("mockSuspend") === "1",
    suspended: params.get("mockSuspended") === "1",
  };
}

function avatar(bg: string): string {
  return (
    "data:image/svg+xml;utf8," +
    encodeURIComponent(
      `<svg xmlns='http://www.w3.org/2000/svg' width='96' height='96'>` +
        `<rect width='96' height='96' fill='${bg}'/>` +
        `<circle cx='48' cy='38' r='18' fill='#ffffff'/>` +
        `<rect x='20' y='62' width='56' height='30' rx='15' fill='#ffffff'/>` +
        `</svg>`,
    )
  );
}

export const MOCK_USER_ID = "mock-user";

// ゴーストモードはページ再読み込みをまたいで保持する（実バックエンドのDB相当）。
const GHOST_KEY = "mock.ghostMode";
export function setMockGhostMode(enabled: boolean) {
  try {
    window.localStorage.setItem(GHOST_KEY, enabled ? "1" : "0");
  } catch {
    // 保存不可でも致命的ではない
  }
}
function getMockGhostMode(): boolean {
  try {
    return window.localStorage.getItem(GHOST_KEY) === "1";
  } catch {
    return false;
  }
}

export function mockUser(scenario: MockScenario): User {
  return {
    id: MOCK_USER_ID,
    displayName: "デザイン確認ユーザー",
    handle: "designer.bsky.social",
    avatarUrl: avatar("#4f8cff"),
    provider: "atproto",
    role: scenario.role,
    profileUrl: "https://bsky.app/profile/designer.bsky.social",
    // モックユーザーは general を所有しているため「管理」設定を使える。
    canManage: true,
    ghostMode: getMockGhostMode(),
  };
}

// オーナー所有チャンネル(general)・他人所有(design)・サスペンド中(archived)を並べ、
// 一覧と詳細の各状態をまとめて確認できるようにする。
export function mockChannels(): Channel[] {
  return [
    {
      id: "c-general",
      slug: "general",
      title: "general",
      description: "雑談チャンネル。自分がオーナー。",
      ownerUserId: MOCK_USER_ID,
      suspended: false,
      suspendGraceSeconds: 300,
      suspendRetentionHours: 24,
      postTtlHours: 24,
      urlLinkifyEnabled: true,
      imageUploadEnabled: true,
      createdAt: "2026-05-28T12:00:00Z",
      accessMode: "whitelist",
    },
    {
      id: "c-design",
      slug: "design",
      title: "design",
      description: "デザイン相談。他ユーザーがオーナー。",
      ownerUserId: "other-user",
      suspended: false,
      suspendGraceSeconds: -1,
      postTtlHours: 72,
      urlLinkifyEnabled: true,
      imageUploadEnabled: true,
      createdAt: "2026-06-01T09:30:00Z",
      // 準備後の削除を無効（無限保持）にして∞バッジの表示を確認する。
      suspendRetentionHours: -1,
      accessMode: "blacklist",
    },
    {
      id: "c-archived",
      slug: "archived",
      title: "archived",
      description: "準備中チャンネルの表示例。",
      ownerUserId: "other-user",
      suspended: true,
      suspendGraceSeconds: 60,
      suspendRetentionHours: 6,
      postTtlHours: 6,
      urlLinkifyEnabled: true,
      imageUploadEnabled: true,
      createdAt: "2026-04-15T18:45:00Z",
      accessMode: "none",
    },
  ];
}

export function mockChannelBySlug(slug: string): Channel {
  const found = mockChannels().find((c) => c.slug === slug);
  if (found) {
    return found;
  }
  // 未知のslugはオーナーとして入れる新規チャンネル扱いにする。
  return {
    id: `c-${slug}`,
    slug,
    title: slug,
    description: "モックチャンネル",
    ownerUserId: MOCK_USER_ID,
    suspended: false,
    postTtlHours: 24,
    urlLinkifyEnabled: true,
    imageUploadEnabled: true,
    createdAt: "2026-05-28T12:00:00Z",
    accessMode: "none",
  };
}

export function mockMessages(slug: string): ServerToClientMessage[] {
  const base = "2026-06-25T09:0";
  return [
    {
      type: "chat.message",
      id: `${slug}-1`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}0:00Z`,
      body: "あたらしいチャンネル作ったよ。よろしく！",
      user: {
        id: "u-alice",
        displayName: "Alice",
        handle: "alice.bsky.social",
        avatarUrl: avatar("#e0699a"),
        provider: "atproto",
        role: "user",
      },
      reactions: [
        {
          emoji: "👍",
          count: 2,
          users: [
            { userId: "u-alice", handle: "alice.bsky.social", displayName: "Alice" },
            { userId: "u-bob", handle: "bob@mastodon.social", displayName: "Bob" },
          ],
        },
        {
          emoji: "🎉",
          count: 1,
          users: [{ userId: "u-bob", handle: "bob@mastodon.social", displayName: "Bob" }],
        },
      ],
    },
    {
      type: "chat.message",
      id: `${slug}-2`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}1:30Z`,
      body: "やったー、さっそく参加しました 🎉",
      user: {
        id: "u-bob",
        displayName: "Bob",
        handle: "bob@mastodon.social",
        provider: "mastodon",
        role: "user",
      },
    },
    {
      type: "chat.message",
      id: `${slug}-3`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}2:10Z`,
      body: "音声機能も近いうちに来るらしいですね。詳しくは https://example.com/news を見てね。",
      user: {
        id: "u-sakura",
        displayName: "さくら",
        handle: "sakura@misskey.io",
        avatarUrl: avatar("#34b27b"),
        provider: "misskey",
        role: "user",
      },
    },
    {
      type: "chat.message",
      id: `${slug}-4`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}3:20Z`,
      body: "画像付きの投稿はこんな感じ。",
      imageUrl: avatar("#4f8cff"),
      imageWidth: 96,
      imageHeight: 96,
      user: {
        id: "u-alice",
        displayName: "Alice",
        handle: "alice.bsky.social",
        avatarUrl: avatar("#e0699a"),
        provider: "atproto",
        role: "user",
      },
    },
    {
      // YouTubeリンクの埋め込みプレイヤー表示を確認する。
      type: "chat.message",
      id: `${slug}-yt`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}3:50Z`,
      body: "この動画おすすめ https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=43s",
      user: {
        id: "u-sakura",
        displayName: "さくら",
        handle: "sakura@misskey.io",
        avatarUrl: avatar("#34b27b"),
        provider: "misskey",
        role: "user",
      },
    },
    {
      // GIF/APNG/MP4をサーバで音声なしMP4化したループ動画の表示確認。
      type: "chat.message",
      id: `${slug}-video`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}3:55Z`,
      body: "GIFアニメを貼るとループ動画になる",
      imageUrl: SAMPLE_MP4_DATA_URI,
      imageWidth: 160,
      imageHeight: 120,
      mediaKind: "video",
      user: {
        id: "u-alice",
        displayName: "Alice",
        handle: "alice.bsky.social",
        avatarUrl: avatar("#e0699a"),
        provider: "atproto",
        role: "user",
      },
    },
    {
      // 本文なしの画像のみ投稿（body は省略され undefined になるケース）。
      type: "chat.message",
      id: `${slug}-5`,
      channelId: `c-${slug}`,
      channelSlug: slug,
      createdAt: `${base}4:00Z`,
      imageUrl: avatar("#34b27b"),
      imageWidth: 96,
      imageHeight: 96,
      user: {
        id: "u-bob",
        displayName: "Bob",
        handle: "bob@mastodon.social",
        provider: "mastodon",
        role: "user",
      },
    },
  ];
}

// 先頭はチャンネルオーナー、のべ人数(totalCount)は実際の表示人数より多めにして
// 「アクティブ数/のべ人数」の見た目を確認できるようにする。
export function mockPresence() {
  const owner = {
    user: {
      id: "u-alice",
      displayName: "Alice",
      handle: "alice.bsky.social",
      avatarUrl: avatar("#e0699a"),
      provider: "atproto",
      role: "user",
    },
    active: true,
    isOwner: true,
  };
  const members = [
    {
      user: {
        id: "u-bob",
        displayName: "Bob",
        handle: "bob@mastodon.social",
        provider: "mastodon",
        role: "user",
      },
      active: true,
    },
    {
      user: {
        id: "u-sakura",
        displayName: "さくら",
        handle: "sakura@misskey.io",
        avatarUrl: avatar("#34b27b"),
        provider: "misskey",
        role: "user",
      },
      active: false,
    },
    {
      user: { id: "u-dave", displayName: "Dave", role: "user" },
      active: false,
    },
  ];
  const activeCount = [owner, ...members].filter((m) => m.active).length;
  return { owner, members, activeCount, totalCount: 10 };
}

// mockLobbyPresence はチャンネル一覧（ロビー）のリアルタイム在室情報を模倣する。
// general はオーナー在室（明るい）、design はオーナー不在（暗い）、archived は準備中。
export function mockLobbyPresence() {
  const alice = {
    id: "u-alice",
    displayName: "Alice",
    handle: "alice.bsky.social",
    avatarUrl: avatar("#e0699a"),
    provider: "atproto",
    role: "user",
  };
  const otherOwner = {
    id: "other-user",
    displayName: "Owner",
    avatarUrl: avatar("#9b6dff"),
    provider: "atproto",
    role: "user",
  };
  const bob = {
    user: {
      id: "u-bob",
      displayName: "Bob",
      handle: "bob@mastodon.social",
      provider: "mastodon",
      role: "user",
    },
    active: true,
  };
  const sakura = {
    user: {
      id: "u-sakura",
      displayName: "さくら",
      handle: "sakura@misskey.io",
      avatarUrl: avatar("#34b27b"),
      provider: "misskey",
      role: "user",
    },
    active: false,
  };
  const dave = { user: { id: "u-dave", displayName: "Dave", role: "user" }, active: false };
  return [
    {
      channelSlug: "general",
      owner: { user: alice, active: true, isOwner: true },
      members: [bob, sakura, dave],
      activeCount: 2,
      totalCount: 10,
    },
    {
      channelSlug: "design",
      owner: { user: otherOwner, active: false, isOwner: true },
      members: [bob],
      activeCount: 1,
      totalCount: 4,
    },
    {
      channelSlug: "archived",
      owner: { user: otherOwner, active: false, isOwner: true },
      members: [],
      activeCount: 0,
      totalCount: 2,
    },
  ];
}

// mockResolveAccessEntry は入室許可リスト登録時のサーバ解決を模倣する。
// 実サーバ同様に入力を分類し、もっともらしい安定IDを持つエントリを返す。
export function mockResolveAccessEntry(raw: string): AccessEntry {
  const input = raw.trim();
  // fediverse: @user@host または user@host
  const handle = input.replace(/^@/, "");
  const fedi = /^([^@/\s]+)@([^@/\s]+)$/.exec(handle);
  if (fedi) {
    const [, user, host] = fedi;
    return {
      provider: "mastodon",
      subject: `https://${host}:${1000 + user.length}`,
      handle: `${user}@${host}`,
      displayName: user,
      profileUrl: `https://${host}/@${user}`,
      raw: input,
    };
  }
  // fediverse プロフィールURL https://host/@user
  const fediUrl = /^https?:\/\/([^/]+)\/@([^/?#]+)/.exec(input);
  if (fediUrl) {
    const [, host, user] = fediUrl;
    return {
      provider: "misskey",
      subject: `https://${host}:${2000 + user.length}`,
      handle: `${user}@${host}`,
      displayName: user,
      profileUrl: `https://${host}/@${user}`,
      raw: input,
    };
  }
  // bluesky プロフィールURL https://bsky.app/profile/<actor>
  const bskyUrl = /^https?:\/\/bsky\.app\/profile\/([^/?#]+)/.exec(input);
  const actor = bskyUrl ? bskyUrl[1] : handle;
  return {
    provider: "atproto",
    subject: `did:plc:${actor.replace(/[^a-z0-9]/gi, "").slice(0, 16) || "mock"}`,
    handle: actor.includes(".") ? actor : `${actor}.bsky.social`,
    displayName: actor.split(".")[0],
    profileUrl: `https://bsky.app/profile/${actor}`,
    raw: input,
  };
}
