export type User = {
  id: string;
  displayName: string;
  handle?: string;
  avatarUrl?: string;
  provider?: string;
  role: "user" | "admin" | "owner" | string;
  // status はユーザー状態。"suspended" のとき「現在ご利用になれません」ページへ誘導する。
  status?: "active" | "suspended" | string;
  profileUrl?: string;
  ttsVoicevoxSpeaker?: TTSVoicevoxSpeaker | null;
  // ghostMode 有効時はどのチャンネルにも入れるが書き込めない。
  ghostMode?: boolean;
  // canManage は「管理」設定（ゴーストモード）を使えるか（/api/me でのみ付与）。
  canManage?: boolean;
};

// LinkPreview は投稿に貼られたURLのOGP要約。タイトルか画像があれば表示する。
export type LinkPreview = {
  url: string;
  title?: string;
  description?: string;
  image?: string;
  siteName?: string;
};

export type TTSVoicevoxSpeaker = {
  uuid: string;
  name: string;
  url: string;
};

export type ChannelAccessMode = "none" | "whitelist" | "blacklist";

// ReactionUser はあるリアクションを付けたユーザー（長押しの「誰が」表示用）。
export type ReactionUser = { userId: string; handle?: string; displayName: string };
// ReactionGroup は投稿の1絵文字ごとのリアクション集計。
export type ReactionGroup = { emoji: string; count: number; users: ReactionUser[] };

// DeckColumn はDeck（PC/iPad横向きの複数カラム表示）の1カラム。
// list=チャンネル一覧、channel=指定slugのチャンネル。DBに手動保存する構成の要素。
// width はカラム幅(px)。未指定なら既定幅。
export type DeckColumn = ({ type: "list" } | { type: "channel"; slug: string }) & {
  width?: number;
};

// AccessEntry は入室許可リストの1エントリ。登録時にサーバが各SNSへ問い合わせて
// 安定ID（atprotoならDID、fediなら instance:accountID）に解決した結果を持つ。
export type AccessEntry = {
  provider?: string;
  subject?: string;
  handle?: string;
  displayName?: string;
  profileUrl?: string;
  raw?: string;
};

export type Channel = {
  id: string;
  slug: string;
  title: string;
  description?: string;
  ownerUserId?: string;
  suspended: boolean;
  suspendRetentionHours?: number;
  suspendGraceSeconds?: number;
  // operatingDeadline は営業の終了予定時刻（RFC3339）。準備中なら未設定。
  operatingDeadline?: string;
  // operatingUnlimited は「時間制限なし」チャンネル（カウントダウンなし・開店/閉店のみ）。
  operatingUnlimited?: boolean;
  // notifyEnabled は現在のユーザーがこのチャンネルの営業開始通知をオンにしているか。
  notifyEnabled?: boolean;
  // postTtlHours は投稿の寿命（時間）。6/24/72。経過した投稿は自動削除される。
  postTtlHours?: number;
  urlLinkifyEnabled: boolean;
  imageUploadEnabled: boolean;
  createdAt: string;
  accessMode: ChannelAccessMode;
  // accessList はオーナー/管理者がチャンネル設定を取得したときのみ含まれる。
  accessList?: AccessEntry[];
};

export type AdminStats = {
  usersCount: number;
  channelsCount: number;
  chatMessagesCount: number;
  activeSessionsCount: number;
};

export type AdminSession = {
  id: string;
  userId: string;
  userDisplayName: string;
  userHandle?: string;
  expiresAt: string;
  lastSeenAt?: string;
  userAgent?: string;
  ipPrefix?: string;
  createdAt: string;
};

export type AdminUser = {
  id: string;
  displayName: string;
  handle?: string;
  avatarUrl?: string;
  status: "active" | "suspended" | string;
  role: "user" | "admin" | "owner" | string;
  provider?: string;
  subject?: string;
  createdAt: string;
  updatedAt: string;
};

export type TTSAutoDictionaryEntry = {
  termKey: string;
  term: string;
  reading: string;
  registeredByUserId?: string;
  registeredByHandle?: string;
  registeredAt: string;
};

export type ClientToServerMessage =
  | {
      type: "chat.message";
      body: string;
      imageToken?: string;
    }
  | {
      type: "presence.beacon";
    }
  | {
      type: "reaction.toggle";
      messageId: string;
      emoji: string;
    }
  | {
      type: "ping";
    };

export type ServerToClientMessage =
  | {
      type: "chat.message";
      id: string;
      channelId: string;
      channelSlug: string;
      user: User;
      // 画像のみの投稿は本文が空でサーバが省略するため undefined になりうる。
      body?: string;
      createdAt: string;
      imageUrl?: string;
      imageWidth?: number;
      imageHeight?: number;
      // "video" のとき imageUrl は音声なしループMP4。既定は "image"。
      mediaKind?: "image" | "video";
      // この投稿の絵文字リアクション集計。
      reactions?: ReactionGroup[];
    }
  | {
      type: "chat.reaction";
      messageId: string;
      channelSlug: string;
      reactions: ReactionGroup[];
      createdAt?: string;
    }
  | {
      type: "chat.message.deleted";
      id: string;
      channelId: string;
      channelSlug: string;
      createdAt: string;
    }
  | {
      type: "channel.presence";
      channelSlug: string;
      owner?: PresenceMember;
      members: PresenceMember[];
      activeCount: number;
      totalCount: number;
      suspendDeadline?: string;
      createdAt: string;
    }
  | {
      type: "room.presence";
      roomSlug: string;
      members: PresenceMember[];
      activeCount: number;
      createdAt: string;
    }
  | {
      type: "lobby.presence";
      channels: LobbyChannelPresence[];
      createdAt: string;
    }
  | {
      type: "channel.suspended";
      channelSlug: string;
      createdAt: string;
    }
  | {
      type: "channel.resumed";
      channelSlug: string;
      createdAt: string;
    }
  | {
      type: "channel.operating";
      channelSlug: string;
      suspendDeadline?: string;
      createdAt: string;
    }
  | {
      type: "channel.operating.paused";
      channelSlug: string;
      suspendDeadline?: string;
      pausedRemainingSeconds?: number;
      createdAt: string;
    }
  | {
      type: "channel.operating.mode";
      channelSlug: string;
      operatingUnlimited?: boolean;
      createdAt: string;
    }
  | {
      type: "channel.kicked";
      channelSlug: string;
      createdAt: string;
    }
  | {
      type: "channel.cleared";
      channelSlug: string;
      createdAt: string;
    }
  | {
      type: "system.notice";
      body: string;
      createdAt: string;
    }
  | {
      type: "tts_queued";
      messageId: string;
      partCount: number;
      createdAt: string;
    }
  | {
      type: "tts_part_ready";
      messageId: string;
      partIndex: number;
      contentHash: string;
      audioUrl: string;
      mimeType: string;
      codec?: string;
      durationMs?: number;
      createdAt: string;
    }
  | {
      type: "tts_ready";
      messageId: string;
      parts: TTSPart[];
      createdAt: string;
    }
  | {
      type: "tts_skipped";
      messageId: string;
      reason: string;
      createdAt: string;
    }
  | {
      type: "tts_error";
      messageId: string;
      reason: string;
      createdAt: string;
    }
  | {
      type: "pong";
      createdAt: string;
    }
  | {
      type: "version";
      version: string;
      createdAt: string;
    }
  | {
      type: "error";
      message: string;
    };

export type PresenceMember = {
  user: User;
  active: boolean;
  isOwner?: boolean;
};

// LobbyChannelPresence はチャンネル一覧（ロビー）でリアルタイム表示する1チャンネル分の在室要約。
export type LobbyChannelPresence = {
  channelSlug: string;
  owner?: PresenceMember;
  members?: PresenceMember[];
  activeCount?: number;
  totalCount?: number;
  // suspendDeadline は準備中へ移行する予定時刻（営業終了 or オーナー離席の自動閉店の早い方）。
  // 一覧カードのカウントダウン表示に使う。営業中の終了予定・離席自動閉店の両方を含む。
  suspendDeadline?: string;
};

export type TTSPart = {
  partIndex: number;
  contentHash: string;
  audioUrl: string;
  mimeType: string;
  codec?: string;
  durationMs?: number;
};
