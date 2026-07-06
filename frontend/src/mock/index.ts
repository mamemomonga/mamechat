// 開発用モックモード。VITE_MOCK_USER 指定時に main.tsx から動的importされ、
// window.fetch と window.WebSocket を差し替えてバックエンド無しで動かす。
// prodビルドには一切含まれない（呼び出し元が import.meta.env.DEV ガード内の
// 動的importなので、Rollupがprodでブランチごと除去する）。
import {
  mockChannelBySlug,
  mockChannels,
  mockLobbyPresence,
  mockMessages,
  mockPresence,
  mockResolveAccessEntry,
  mockUser,
  resolveScenario,
  setMockGhostMode,
  type MockScenario,
} from "./data";

const OPEN = 1;
const CLOSED = 3;

// アップロードされた動画(MP4)のオブジェクトURLを覚えておき、投稿エコー時に動画として表示する。
const mockVideoUrls = new Set<string>();

// 試聴用のごく短い無音WAV（モックでは音声合成しない）。
const SILENT_WAV =
  "data:audio/wav;base64,UklGRigAAABXQVZFZm10IBAAAAABAAEAESsAACJWAAACABAAZGF0YQQAAAAAAA==";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function route(
  path: string,
  method: string,
  init: RequestInit | undefined,
  scenario: MockScenario,
): Response | null {
  if (path === "/api/config") {
    // モックでは Push を無効（VAPID未設定）にしておく。
    return jsonResponse({
      messageMaxLength: 400,
      serviceName: "テストチャット",
      pushEnabled: false,
      vapidPublicKey: "",
      whitelistEnabled: true,
    });
  }
  if (path === "/api/push/subscribe" || path === "/api/push/unsubscribe") {
    return jsonResponse({ ok: true });
  }
  if (path === "/api/service-info") {
    return jsonResponse({
      serviceName: "テストチャット",
      version: __APP_VERSION__,
      headerImageUrl:
        "data:image/svg+xml;utf8," +
        encodeURIComponent(
          "<svg xmlns='http://www.w3.org/2000/svg' width='1200' height='300'><rect width='1200' height='300' fill='#1f9e94'/><text x='600' y='170' font-size='64' fill='#fff' text-anchor='middle'>Service Header</text></svg>",
        ),
      overviewHtml: "<h1>ようこそ</h1><p>これはモックの<strong>サーバ概要</strong>です。</p>",
      appInfoHtml:
        "<h2>アプリケーション情報</h2><p>バージョン: <code>" +
        __APP_VERSION__ +
        "</code></p><p>公開リポジトリやVOICEVOXの利用条件などがここに表示されます。</p>",
    });
  }
  if (path === "/api/admin/service-settings") {
    return jsonResponse({
      overview: "# サンプル概要\n\nMarkdownで書けます。",
      headerImageUrl: "",
      whitelistEnabled: true,
    });
  }
  if (path === "/api/admin/service-settings/whitelist" && method === "PUT") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    return jsonResponse({ whitelistEnabled: Boolean(body.enabled) });
  }
  if (path.startsWith("/api/admin/service-settings/")) {
    return jsonResponse({ ok: true });
  }
  const chNotify = /^\/api\/channels\/([^/]+)\/notify$/.exec(path);
  if (chNotify && method === "PUT") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    return jsonResponse({ enabled: Boolean(body.enabled) });
  }
  if (path === "/api/og") {
    // モックでは固定のサンプルプレビューを返す（url はカード側でpropにフォールバック）。
    return jsonResponse({
      url: "",
      title: "OGPプレビューのサンプルタイトル",
      description: "リンク先のOGP設定から取得した説明文がここに表示されます。",
      image:
        "data:image/svg+xml;utf8," +
        encodeURIComponent(
          "<svg xmlns='http://www.w3.org/2000/svg' width='200' height='120'><rect width='200' height='120' fill='#1f9e94'/><text x='100' y='66' font-size='20' fill='#fff' text-anchor='middle'>OGP</text></svg>",
        ),
      siteName: "example.com",
    });
  }
  if (path === "/api/me") {
    return jsonResponse({ user: mockUser(scenario) });
  }
  if (path === "/api/me/deck") {
    if (method === "PUT") return jsonResponse({ ok: true });
    return jsonResponse({ columns: [] });
  }
  if (path === "/api/channels") {
    if (method === "GET") return jsonResponse({ channels: mockChannels() });
    if (method === "POST") {
      const body = init?.body ? JSON.parse(init.body as string) : {};
      return jsonResponse({ channel: mockChannelBySlug(body.slug ?? "new-channel") });
    }
  }
  if (path === "/api/logout" && method === "POST") {
    return jsonResponse({ ok: true });
  }
  if (path === "/api/settings/ghost-mode" && method === "PUT") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    setMockGhostMode(!!body.enabled);
    return jsonResponse({ ghostMode: !!body.enabled });
  }
  if (path === "/api/settings/voicevox-speakers" && method === "GET") {
    return jsonResponse({
      speakers: [
        { uuid: "spk-1", name: "ずんだもん", url: "https://voicevox.hiroshiba.jp/" },
        { uuid: "spk-2", name: "四国めたん", url: "https://voicevox.hiroshiba.jp/" },
      ],
    });
  }
  if (path === "/api/settings/voicevox-speaker" && method === "PUT") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    if (!body.speakerUuid) {
      return jsonResponse({ speaker: null });
    }
    return jsonResponse({
      speaker: { uuid: String(body.speakerUuid ?? "spk-1"), name: "ずんだもん", url: "https://voicevox.hiroshiba.jp/" },
    });
  }
  if (path === "/api/settings/voicevox-speaker/preview" && method === "POST") {
    return jsonResponse({ audioUrl: SILENT_WAV });
  }
  const chRetention = /^\/api\/channels\/([^/]+)\/suspend-retention$/.exec(path);
  if (chRetention && method === "PUT") {
    return jsonResponse({ ok: true });
  }
  const chGrace = /^\/api\/channels\/([^/]+)\/suspend-grace$/.exec(path);
  if (chGrace && method === "PUT") {
    return jsonResponse({ ok: true });
  }
  const chOperating = /^\/api\/channels\/([^/]+)\/operating$/.exec(path);
  if (chOperating && method === "POST") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    activeChannelSocket?.triggerStartOperating(Number(body.durationMinutes ?? 60));
    return jsonResponse({ ok: true });
  }
  const chOpen = /^\/api\/channels\/([^/]+)\/operating\/open$/.exec(path);
  if (chOpen && method === "POST") {
    activeChannelSocket?.triggerOpenUnlimited();
    return jsonResponse({ ok: true });
  }
  const chDuration = /^\/api\/channels\/([^/]+)\/operating\/duration$/.exec(path);
  if (chDuration && method === "POST") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    activeChannelSocket?.triggerSetOperatingDuration(Number(body.durationMinutes ?? 60));
    return jsonResponse({ ok: true });
  }
  const chExtend = /^\/api\/channels\/([^/]+)\/operating\/extend$/.exec(path);
  if (chExtend && method === "POST") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    activeChannelSocket?.triggerExtendOperating(Number(body.minutes ?? 5));
    return jsonResponse({ ok: true });
  }
  const chRest = /^\/api\/channels\/([^/]+)\/rest$/.exec(path);
  if (chRest && method === "POST") {
    activeChannelSocket?.triggerSuspendNow();
    return jsonResponse({ ok: true });
  }
  const chMessages = /^\/api\/channels\/([^/]+)\/messages$/.exec(path);
  if (chMessages) {
    const slug = decodeURIComponent(chMessages[1]);
    if (method === "GET") return jsonResponse({ messages: mockMessages(slug) });
    if (method === "DELETE") return jsonResponse({ ok: true });
  }
  const chMessageDelete = /^\/api\/channels\/([^/]+)\/messages\/([^/]+)$/.exec(path);
  if (chMessageDelete && method === "DELETE") {
    return jsonResponse({ ok: true });
  }
  const chMessageTTS = /^\/api\/channels\/([^/]+)\/messages\/([^/]+)\/tts$/.exec(path);
  if (chMessageTTS && method === "GET") {
    // モックでは無音WAVを1パート返す（「ここから読み上げる」の動作確認用）。
    return jsonResponse({ parts: [{ partIndex: 0, audioUrl: SILENT_WAV }] });
  }
  const chSettings = /^\/api\/channels\/([^/]+)\/settings$/.exec(path);
  if (chSettings && method === "PUT") {
    const slug = decodeURIComponent(chSettings[1]);
    const body = init?.body ? JSON.parse(init.body as string) : {};
    return jsonResponse({ channel: { ...mockChannelBySlug(slug), ...body } });
  }
  const chAccessResolve = /^\/api\/channels\/([^/]+)\/access\/resolve$/.exec(path);
  if (chAccessResolve && method === "POST") {
    const body = init?.body ? JSON.parse(init.body as string) : {};
    return jsonResponse({ entry: mockResolveAccessEntry(String(body.entry ?? "")) });
  }
  const chImages = /^\/api\/channels\/([^/]+)\/images$/.exec(path);
  if (chImages && method === "POST") {
    // アップロードされたBlobをそのままプレビュー用URLにして、見た目を確認できるようにする。
    let url = "";
    let kind: "image" | "video" = "image";
    if (init?.body instanceof FormData) {
      const file = init.body.get("image");
      if (file instanceof Blob) {
        url = URL.createObjectURL(file);
        // MP4はサーバ変換相当として動画扱いにする（GIF/APNGはモックでは画像のまま）。
        if (file.type === "video/mp4") {
          kind = "video";
          mockVideoUrls.add(url);
        }
      }
    }
    return jsonResponse({ imageToken: url, url, width: 800, height: 600, mediaKind: kind });
  }
  const channel = /^\/api\/channels\/([^/]+)$/.exec(path);
  if (channel && method === "GET") {
    const ch = mockChannelBySlug(decodeURIComponent(channel[1]));
    // 準備中シナリオではオーナー所有チャンネルをサスペンド済みとして返す。
    if (scenario.suspended && ch.ownerUserId === mockUser(scenario).id) {
      ch.suspended = true;
    }
    return jsonResponse({ channel: ch });
  }
  if (channel && method === "DELETE") {
    return jsonResponse({ ok: true });
  }
  const adminDelete = /^\/api\/admin\/messages\/([^/]+)$/.exec(path);
  if (adminDelete && method === "DELETE") {
    return jsonResponse({ ok: true });
  }
  if (path.startsWith("/api/auth/")) {
    // ログイン開始はモックでは何もしない（ログイン画面の見た目確認用）。
    return jsonResponse({ authorizationUrl: "#mock-login" });
  }
  return null;
}

function installFetch(scenario: MockScenario) {
  const realFetch = window.fetch.bind(window);
  window.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = urlOf(input).replace(/^https?:\/\/[^/]+/, "").split("?")[0];
    if (!path.startsWith("/api/")) {
      return realFetch(input, init);
    }
    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    return route(path, method, init, scenario) ?? jsonResponse({ error: `mock未対応: ${method} ${path}` }, 404);
  }) as typeof fetch;
}

// アクティブなチャンネルソケット（営業中ボタンのモック操作で参照する）。
let activeChannelSocket: MockChannelSocket | null = null;

// チャンネル用WebSocketの差し替え。/ws/channels/ 以外（Vite HMR等）は本物に委譲する。
class MockChannelSocket extends EventTarget {
  readyState = 0;
  private slug: string;
  private suspendCountdown: boolean;
  private suspended: boolean;
  private restTimer: number | undefined;
  private operatingDeadline: string | undefined;

  constructor(url: string) {
    super();
    const match = /\/ws\/channels\/([^/?]+)/.exec(url);
    this.slug = match ? decodeURIComponent(match[1]) : "general";
    const scenario = resolveScenario();
    this.suspendCountdown = scenario.suspendCountdown;
    this.suspended = scenario.suspended;
    activeChannelSocket = this;
    setTimeout(() => this.handleOpen(), 60);
  }

  private handleOpen() {
    this.readyState = OPEN;
    this.dispatchEvent(new Event("open"));
    // サーバと同様、接続時にバージョンを通知する（モックでは一致させてリロードしない）。
    this.deliver({ type: "version", version: __APP_VERSION__, createdAt: new Date().toISOString() });
    this.deliverPresence();
    if (this.suspended) {
      this.deliver({ type: "channel.suspended", channelSlug: this.slug, createdAt: new Date().toISOString() });
    }
  }

  private deliverPresence(suspendDeadline?: string) {
    const { owner, members, activeCount, totalCount } = mockPresence();
    const presence: Record<string, unknown> = {
      type: "channel.presence",
      channelSlug: this.slug,
      owner,
      members,
      activeCount,
      totalCount,
      createdAt: new Date().toISOString(),
    };
    const deadline = suspendDeadline ?? (this.suspendCountdown ? new Date(Date.now() + 30_000).toISOString() : undefined);
    if (deadline) {
      presence.suspendDeadline = deadline;
    }
    this.deliver(presence);
  }

  // triggerStartOperating は「準備中→営業中（残りカウントダウン）」をモックで再現する。
  triggerStartOperating(durationMinutes: number) {
    if (this.restTimer) {
      window.clearTimeout(this.restTimer);
      this.restTimer = undefined;
    }
    this.suspended = false;
    const deadline = new Date(Date.now() + durationMinutes * 60_000).toISOString();
    this.operatingDeadline = deadline;
    this.deliver({ type: "channel.operating", channelSlug: this.slug, suspendDeadline: deadline, createdAt: new Date().toISOString() });
    this.deliverPresence(deadline);
  }

  // triggerSetOperatingDuration は現在時刻から指定分後を終了予定時刻にする。
  triggerSetOperatingDuration(durationMinutes: number) {
    this.triggerStartOperating(durationMinutes);
  }

  // triggerOpenUnlimited は時間制限なしチャンネルの開店（終了予定なし）を再現する。
  triggerOpenUnlimited() {
    if (this.restTimer) {
      window.clearTimeout(this.restTimer);
      this.restTimer = undefined;
    }
    this.suspended = false;
    this.operatingDeadline = undefined;
    this.deliver({ type: "channel.operating", channelSlug: this.slug, createdAt: new Date().toISOString() });
    this.deliverPresence();
  }

  // triggerExtendOperating は営業の終了予定時刻を延長する。
  triggerExtendOperating(minutes: number) {
    const base = this.operatingDeadline ? new Date(this.operatingDeadline).getTime() : Date.now();
    const deadline = new Date(base + minutes * 60_000).toISOString();
    this.operatingDeadline = deadline;
    this.deliver({ type: "channel.operating", channelSlug: this.slug, suspendDeadline: deadline, createdAt: new Date().toISOString() });
    this.deliverPresence(deadline);
  }

  // triggerSuspendNow は即座に準備中へ移行する。
  triggerSuspendNow() {
    if (this.restTimer) {
      window.clearTimeout(this.restTimer);
      this.restTimer = undefined;
    }
    this.suspended = true;
    this.operatingDeadline = undefined;
    this.deliver({ type: "channel.suspended", channelSlug: this.slug, createdAt: new Date().toISOString() });
  }

  private deliver(obj: unknown) {
    this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(obj) }));
  }

  send(raw: string) {
    let parsed: { type?: string; body?: string; imageToken?: string };
    try {
      parsed = JSON.parse(raw);
    } catch {
      return;
    }
    // 自分の投稿をエコーバックして即座に表示する。beacon/pingは無視。
    if (parsed.type === "chat.message") {
      const me = mockUser(resolveScenario());
      setTimeout(
        () =>
          this.deliver({
            type: "chat.message",
            id: `local-${Date.now()}`,
            channelId: `c-${this.slug}`,
            channelSlug: this.slug,
            createdAt: new Date().toISOString(),
            body: parsed.body ?? "",
            user: me,
            // モックでは imageToken をそのままプレビューURLとして扱う。
            ...(parsed.imageToken
              ? {
                  imageUrl: parsed.imageToken,
                  imageWidth: 800,
                  imageHeight: 600,
                  mediaKind: mockVideoUrls.has(parsed.imageToken) ? "video" : "image",
                }
              : {}),
          }),
        80,
      );
    }
  }

  close() {
    this.readyState = CLOSED;
    if (this.restTimer) {
      window.clearTimeout(this.restTimer);
      this.restTimer = undefined;
    }
    if (activeChannelSocket === this) {
      activeChannelSocket = null;
    }
    this.dispatchEvent(new Event("close"));
  }
}

// チャンネル一覧（ロビー）用WebSocketの差し替え。全チャンネルの在室要約を周期配信する。
class MockLobbySocket extends EventTarget {
  readyState = 0;
  private timer: number | undefined;

  constructor() {
    super();
    setTimeout(() => this.handleOpen(), 60);
  }

  private handleOpen() {
    this.readyState = OPEN;
    this.dispatchEvent(new Event("open"));
    this.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({
          type: "version",
          version: __APP_VERSION__,
          createdAt: new Date().toISOString(),
        }),
      }),
    );
    this.deliverPresence();
    // 実サーバ同様、一定間隔で再配信してリアルタイム更新を模倣する。
    this.timer = window.setInterval(() => this.deliverPresence(), 3000);
  }

  private deliverPresence() {
    this.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({
          type: "lobby.presence",
          channels: mockLobbyPresence(),
          createdAt: new Date().toISOString(),
        }),
      }),
    );
  }

  send() {
    // ロビーは購読のみ（ping等は無視）。
  }

  close() {
    this.readyState = CLOSED;
    if (this.timer) {
      window.clearInterval(this.timer);
      this.timer = undefined;
    }
    this.dispatchEvent(new Event("close"));
  }
}

function installWebSocket() {
  const RealWebSocket = window.WebSocket;
  window.WebSocket = new Proxy(RealWebSocket, {
    construct(target, args) {
      const url = String(args[0] ?? "");
      if (url.includes("/ws/lobby")) {
        return new MockLobbySocket() as unknown as WebSocket;
      }
      if (url.includes("/ws/channels/")) {
        return new MockChannelSocket(url) as unknown as WebSocket;
      }
      return Reflect.construct(target, args);
    },
  }) as typeof WebSocket;
}

export function installMockMode() {
  const scenario = resolveScenario();
  installFetch(scenario);
  installWebSocket();
  // eslint-disable-next-line no-console
  console.info(
    `[mock] enabled — role=${scenario.role}` +
      (scenario.suspendCountdown ? ", suspend countdown" : "") +
      " — knobs: ?mockRole=user|owner|admin, ?mockSuspend=1",
  );
}
