import { type CSSProperties, useEffect, useLayoutEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { Link, useNavigate, useParams } from "react-router";

import ChatInput from "../components/ChatInput";
import ChatMessageList from "../components/ChatMessageList";
import ChannelPresence from "../components/ChannelPresence";
import UserAvatar from "../components/UserAvatar";
import UserProfilePopup, { type PopupUser } from "../components/UserProfilePopup";
import { ChannelSettingsPanel } from "./ChannelSettingsPage";
import {
  deleteChannelMessage,
  fetchMessageTTS,
  getChannel,
  getMe,
  getPublicConfig,
  listMessages,
  startOperating,
  setOperatingDuration,
  extendOperating,
  suspendChannelNow,
  openChannel,
  setChannelNotify,
  uploadChannelImage,
} from "../lib/api";
import { getPushConfig, isPushEnvironmentSupported } from "../lib/pushNotify";
import { isOperatingHintDismissed, setOperatingHintDismissed } from "../lib/operatingHint";
import { resizeImageToJpeg } from "../lib/image";
import { isAnimatedMedia, MEDIA_MAX_BYTES } from "../lib/media";
import { labelChannelGrace, labelChannelRetention } from "../lib/suspendOptions";
import { handleServerVersion } from "../lib/version";
import { connectChannelSocket, type WebSocketStatus } from "../lib/websocket";
import type { Channel, PresenceMember, ServerToClientMessage, TTSPart, User } from "../types/chat";

type SocketHandle = ReturnType<typeof connectChannelSocket>;
type PendingTTSParts = {
  partCount?: number;
  nextIndex: number;
  parts: Map<number, TTSPart>;
};

const SILENT_AUDIO_URL =
  "data:audio/wav;base64,UklGRigAAABXQVZFZm10IBAAAAABAAEAESsAACJWAAACABAAZGF0YQQAAAAAAA==";
const BACKFILL_PAGE_SIZE = 200;
const BACKFILL_RETRY_MS = 5000;

// 最下部とみなすしきい値（px）と、自動スクロール有効に必要な最下部滞在時間（ms）。
const SCROLL_BOTTOM_THRESHOLD = 48;
const SCROLL_STABLE_MS = 1000;
// 追従を終了するまでの「変化がない時間」（ms）。OGP取得＋画像ロードの遅延を吸収する。
const SCROLL_FOLLOW_QUIET_MS = 2500;

// 画像/動画のレンダリング（高さ）が確定したか。
function isMediaLoaded(m: Element): boolean {
  if (m instanceof HTMLImageElement) {
    return m.complete;
  }
  if (m instanceof HTMLVideoElement) {
    return m.readyState >= 1;
  }
  return true;
}

// followBottom はスクロール要素を最下部へ貼り付け続ける。
// OGPカードや画像が遅れて挿入・ロードされて高さが変わっても、変化が落ち着くまで
// 最下部へ寄せ直す（＝OGP画像も見える状態にする）。ユーザーが上へスクロールしたら中止。
// 戻り値のクリーンアップで追従を止める。
function followBottom(el: HTMLElement): () => void {
  let active = true;
  let raf = 0;
  let quietTimer = 0;

  function stop() {
    if (!active) {
      return;
    }
    active = false;
    el.removeEventListener("scroll", onScroll);
    observer.disconnect();
    cancelAnimationFrame(raf);
    window.clearTimeout(quietTimer);
  }

  const scrollDown = () => {
    if (!active) {
      return;
    }
    el.scrollTop = el.scrollHeight;
    // 変化があったので追従を延長。一定時間変化がなければ終了する。
    window.clearTimeout(quietTimer);
    quietTimer = window.setTimeout(stop, SCROLL_FOLLOW_QUIET_MS);
  };

  const attachMedia = (root: Element) => {
    const media: Element[] = [];
    if (root instanceof HTMLImageElement || root instanceof HTMLVideoElement) {
      media.push(root);
    }
    root.querySelectorAll("img, video").forEach((m) => media.push(m));
    media.forEach((m) => {
      if (isMediaLoaded(m)) {
        return;
      }
      const onLoad = () => scrollDown();
      m.addEventListener("load", onLoad, { once: true });
      m.addEventListener("loadeddata", onLoad, { once: true });
      m.addEventListener("error", onLoad, { once: true });
    });
  };

  const onScroll = () => {
    // プログラム側のスクロールは最下部付近を保つため、しきい値超え＝ユーザーの上スクロール。
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distance > SCROLL_BOTTOM_THRESHOLD) {
      stop();
    }
  };

  const observer = new MutationObserver((mutations) => {
    for (const mut of mutations) {
      mut.addedNodes.forEach((n) => {
        if (n instanceof Element) {
          attachMedia(n);
        }
      });
    }
    scrollDown();
  });

  attachMedia(el);
  el.addEventListener("scroll", onScroll, { passive: true });
  observer.observe(el, { childList: true, subtree: true });
  raf = requestAnimationFrame(scrollDown);
  quietTimer = window.setTimeout(stop, SCROLL_FOLLOW_QUIET_MS);

  return stop;
}

// 「営業中」ボタンで設定できる営業時間。バックエンドの許可リストと一致させること。
const OPERATING_DURATIONS = [
  { minutes: 30, label: "30分" },
  { minutes: 60, label: "1時間" },
  { minutes: 90, label: "1時間30分" },
  { minutes: 120, label: "2時間" },
  { minutes: 180, label: "3時間" },
];

// 営業の延長で設定できる時間。
const EXTEND_DURATIONS = [
  { minutes: 5, label: "+5分" },
  { minutes: 15, label: "+15分" },
  { minutes: 30, label: "+30分" },
  { minutes: 60, label: "+1時間" },
  { minutes: 120, label: "+2時間" },
];

function timeInputValue(date: Date): string {
  return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}

function defaultEndTimeValue(addMinutes: number): string {
  return timeInputValue(new Date(Date.now() + addMinutes * 60_000));
}

function endTimeValueFromDeadline(deadline: string | null): string {
  if (!deadline) {
    return defaultEndTimeValue(60);
  }
  const date = new Date(deadline);
  if (Number.isNaN(date.getTime())) {
    return defaultEndTimeValue(60);
  }
  return timeInputValue(date);
}

function durationMinutesUntil(endTime: string): number | null {
  const match = /^(\d{2}):(\d{2})$/.exec(endTime);
  if (!match) {
    return null;
  }
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours > 23 || minutes > 59) {
    return null;
  }
  const now = new Date();
  const target = new Date(now);
  target.setHours(hours, minutes, 0, 0);
  if (target.getTime() <= now.getTime()) {
    target.setDate(target.getDate() + 1);
  }
  return Math.ceil((target.getTime() - now.getTime()) / 60_000);
}

function labelPostTtlHours(channel: Channel | null | undefined): string {
  const hours = channel?.postTtlHours ?? 24;
  if (hours === 72) {
    return "3日";
  }
  return `${hours}時間`;
}

function labelFeatureEnabled(enabled: boolean | undefined): string {
  if (enabled === undefined) {
    return "読み込み中";
  }
  return enabled ? "可" : "不可";
}

function shouldShowScrollBottomButton(el: HTMLElement): boolean {
  const maxScroll = el.scrollHeight - el.clientHeight;
  if (maxScroll <= 4) {
    return false;
  }
  const distanceFromBottom = maxScroll - el.scrollTop;
  if (distanceFromBottom <= SCROLL_BOTTOM_THRESHOLD) {
    return false;
  }
  return distanceFromBottom > el.clientHeight * 0.5;
}

// 残り営業時間を H:MM:SS / M:SS 形式に整形する。
function formatRemaining(totalSeconds: number): string {
  const s = Math.max(0, totalSeconds);
  const hours = Math.floor(s / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  const mm = String(minutes).padStart(2, "0");
  const ss = String(seconds).padStart(2, "0");
  if (hours > 0) {
    return `${hours}:${mm}:${ss}`;
  }
  return `${minutes}:${ss}`;
}

// カラム（Deck）またはビューポートの高さがこれを下回ると、インラインの書き込み欄を
// 隠してスマホと同じ「ボタン→フォーム」方式に切り替える。狭いカラムで投稿欄が
// メッセージ表示を圧迫しないようにするため。
const COMPACT_COMPOSER_MAX_HEIGHT = 800;

type ChannelViewProps = {
  slug: string;
  // 以下はDeck埋め込み用。未指定＝通常のフルページ表示（モバイル/クラシック）。
  embedded?: boolean;
  onExit?: () => void; // 「戻る」操作（Deckではカラムを一覧へ戻す）
  onSuspended?: (reason: "preparing" | "closed") => void; // 準備中/キックでこのチャンネルに居られないとき
  onStatus?: (status: WebSocketStatus) => void; // 接続状態の通知（Deckのカラムヘッダ表示用）
};

export function ChannelView({ slug, embedded, onExit, onSuspended, onStatus }: ChannelViewProps) {
  const navigate = useNavigate();
  // Deck用コールバック/フラグは、再接続を招かないよう ref 経由で参照する（大きなソケット設定
  // effect の依存に入れない）。
  const embeddedRef = useRef(embedded);
  embeddedRef.current = embedded;
  const onSuspendedRef = useRef(onSuspended);
  onSuspendedRef.current = onSuspended;
  const onStatusRef = useRef(onStatus);
  onStatusRef.current = onStatus;
  const [user, setUser] = useState<User | null>(null);
  const [channel, setChannel] = useState<Channel | null>(null);
  const [messages, setMessages] = useState<ServerToClientMessage[]>([]);
  const [owner, setOwner] = useState<PresenceMember | null>(null);
  const [members, setMembers] = useState<PresenceMember[]>([]);
  const [activeCount, setActiveCount] = useState(0);
  const [totalCount, setTotalCount] = useState(0);
  const [status, setStatus] = useState<WebSocketStatus>("connecting");
  const [suspended, setSuspended] = useState(false);
  // 時間制限なしチャンネル（カウントダウンなし・開店/閉店のみ）。
  const [operatingUnlimited, setOperatingUnlimited] = useState(false);
  // Push通知: この環境で利用可能か（＝「通知」ボタンを出すか） / このチャンネルの通知をオンにしているか。
  const [pushAvailable, setPushAvailable] = useState(false);
  const [notifyEnabled, setNotifyEnabled] = useState(false);
  const [notifyBusy, setNotifyBusy] = useState(false);
  const [suspendDeadline, setSuspendDeadline] = useState<string | null>(null);
  const [secondsLeft, setSecondsLeft] = useState(0);
  const [operatingBusy, setOperatingBusy] = useState(false);
  const [error, setError] = useState("");
  const [pendingImageUploads, setPendingImageUploads] = useState(0);
  const [imageUploadError, setImageUploadError] = useState("");
  const [ttsEnabled, setTtsEnabled] = useState(() => localStorage.getItem("ttsEnabled") === "true");
  const [ttsVolume, setTtsVolume] = useState(() => Number(localStorage.getItem("ttsVolume") || "0.8"));
  const [ttsDialogOpen, setTtsDialogOpen] = useState(false);
  // 営業時間設定ダイアログ・営業延長ダイアログの開閉。
  const [operatingDialogOpen, setOperatingDialogOpen] = useState(false);
  // 「準備中なので営業中にしよう」の噴きだし。永続非表示はブラウザ保存、
  // この表示中の「閉じる」はセッション内で閉じるだけ。
  const [operatingHintDismissedForever] = useState(isOperatingHintDismissed);
  const [operatingHintClosed, setOperatingHintClosed] = useState(false);
  const [operatingHintDontShow, setOperatingHintDontShow] = useState(false);
  const [extendDialogOpen, setExtendDialogOpen] = useState(false);
  const [operatingEndTimeDraft, setOperatingEndTimeDraft] = useState(() => defaultEndTimeValue(60));
  const [extendEndTimeDraft, setExtendEndTimeDraft] = useState(() => defaultEndTimeValue(60));
  // チャンネル設定ダイアログ（モーダル）の開閉。
  const [settingsDialogOpen, setSettingsDialogOpen] = useState(false);
  const [messageMaxLength, setMessageMaxLength] = useState(400);
  // サーバ全体のホワイトリスト機能フラグ。無効時はホワイトリスト有効バッヂを出さない。
  const [whitelistEnabled, setWhitelistEnabled] = useState(false);
  const [isHandheldOS] = useState(detectHandheldOS);
  const [usesChannelInfoDrawer] = useState(detectChannelInfoDrawerOS);
  const [mobileComposerOpen, setMobileComposerOpen] = useState(false);
  // カラム（またはビューポート）が低いとき true。スマホと同じボタン方式に切り替える。
  const [compactHeight, setCompactHeight] = useState(false);
  // モバイルでチャンネル名タップ時に開く詳細ダイアログと、その中のオーナープロフィール表示。
  const [infoDialogOpen, setInfoDialogOpen] = useState(false);
  const [infoPopupUser, setInfoPopupUser] = useState<PopupUser | null>(null);
  const [mobileDraft, setMobileDraft] = useState("");
  const [mobileKeyboardInset, setMobileKeyboardInset] = useState(0);
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);
  const socketRef = useRef<SocketHandle | null>(null);
  const messagesRef = useRef<ServerToClientMessage[]>([]);
  const backfillingRef = useRef(false);
  const needsBackfillRef = useRef(false);
  const lastSyncedMessageIdRef = useRef<string | null>(null);
  const resumeCursorRef = useRef<string | null>(null);
  const backfillRetryTimerRef = useRef<number | undefined>(undefined);
  const listRef = useRef<HTMLDivElement | null>(null);
  // 最下部に到達した時刻（0=最下部にいない）。上にスクロールすると0に戻る。
  // 新着の自動スクロールは「最下部に1秒以上いる」ときだけ有効。
  // 初期値は現在時刻（初回は最下部にいる想定。スクロールバーが無い間もこの値を保つ）。
  const atBottomSinceRef = useRef(Date.now());
  // 初回ロード時に一度だけ最下部へスクロールしたか。
  const initialScrolledRef = useRef(false);
  const mobileTextareaRef = useRef<HTMLTextAreaElement | null>(null);
  // 高さ計測の起点（chatPage）。埋め込み時は親カラム、単独時はビューポートで判定する。
  const chatPageRef = useRef<HTMLElement | null>(null);
  const ttsEnabledRef = useRef(ttsEnabled);
  const ttsVolumeRef = useRef(ttsVolume);
  const audioQueueRef = useRef<string[]>([]);
  const currentAudioRef = useRef<HTMLAudioElement | null>(null);
  const audioPlayingRef = useRef(false);
  const audioUnlockingRef = useRef(false);
  const pendingTTSPartsRef = useRef<Map<string, PendingTTSParts>>(new Map());
  // キュークリア（黙れ）のたびに増やし、進行中の「ここから読み上げる」ループを失効させる。
  const ttsGenerationRef = useRef(0);

  const isOwner = !!user && !!channel && channel.ownerUserId === user.id;
  // ゴーストモード中は閲覧のみ（書き込み不可）。
  const ghostMode = !!user?.ghostMode;
  // スマホ、または低いカラム/ウィンドウでは、インライン投稿欄を隠して
  // 「書き込み」ボタン→フォーム（オーバーレイ）方式にする。
  const useComposerButton = isHandheldOS || compactHeight;
  // 営業中（終了予定時刻あり＝カウントダウン中）。時間制限なしのときはカウントダウンしない。
  const operating = !suspended && !operatingUnlimited && !!suspendDeadline;
  // 営業状態ボタンのLED: 営業中=緑点灯 / 準備中=緑消灯。
  const operatingLed = suspended ? "operatingOff" : "operatingOn";
  // 営業状態ダイアログのタイトル。準備中は開始系、営業中は終了。
  const operatingDialogTitle = !suspended
    ? "営業を終了"
    : operatingUnlimited
      ? "営業を開始"
      : "営業時間を設定";
  // 入室許可バッヂはチャンネルページでは一般ユーザにも表示する。
  const accessBadge =
    channel?.accessMode === "whitelist" && whitelistEnabled
      ? "ホワイトリスト有効"
      : channel?.accessMode === "blacklist"
        ? "ブラックリスト有効"
        : null;

  useEffect(() => {
    let cancelled = false;
    async function start() {
      setError("");
      try {
        const config = await getPublicConfig();
        if (cancelled) {
          return;
        }
        setMessageMaxLength(config.messageMaxLength);
        setWhitelistEnabled(!!config.whitelistEnabled);
        const me = await getMe();
        if (!me.user) {
          navigate("/login");
          return;
        }
        setUser(me.user);
        const channelRes = await getChannel(slug);
        if (cancelled) {
          return;
        }
        // サスペンド中はオーナー以外入室できない。Deckではカラムを準備中カードに切替、
        // 通常表示では一覧へ戻して開店通知の案内を出す。
        if (channelRes.channel.suspended && channelRes.channel.ownerUserId !== me.user.id) {
          if (embeddedRef.current) {
            onSuspendedRef.current?.("preparing");
          } else {
            navigate("/", { state: { notifyPrompt: { slug, reason: "preparing" } } });
          }
          return;
        }
        setChannel(channelRes.channel);
        setSuspended(channelRes.channel.suspended);
        setOperatingUnlimited(channelRes.channel.operatingUnlimited ?? false);
        // 営業中なら終了予定時刻を反映してカウントダウンを開始する（WSのプレゼンス到着前でも表示）。
        if (!channelRes.channel.suspended && channelRes.channel.operatingDeadline) {
          setSuspendDeadline(channelRes.channel.operatingDeadline);
        }
        setNotifyEnabled(channelRes.channel.notifyEnabled ?? false);
        // Push通知が利用可能な環境（VAPID設定済み・対応ブラウザ）なら「通知」ボタンを出す。
        // 実際に受け取るにはPush有効化が必要だが、登録自体はここで行える。
        if (isPushEnvironmentSupported() && (await getPushConfig()).enabled) {
          if (!cancelled) {
            setPushAvailable(true);
          }
        }
        const messagesRes = await listMessages(slug);
        if (cancelled) {
          return;
        }
        setMessages(messagesRes.messages);
        messagesRef.current = messagesRes.messages;
        const initialCursor = latestChatMessageId(messagesRes.messages);
        lastSyncedMessageIdRef.current = initialCursor;
        resumeCursorRef.current = initialCursor;
        needsBackfillRef.current = false;
        socketRef.current = connectChannelSocket(
          slug,
          (message) => {
            switch (message.type) {
              case "pong":
                return;
              case "version":
                handleServerVersion(message.version);
                return;
              case "chat.message.deleted":
                setMessages((current) =>
                  current.filter((item) => item.type !== "chat.message" || item.id !== message.id),
                );
                return;
              case "channel.presence":
                // メンバーやカウントは空のとき省略されることがあるため既定値で補う。
                setOwner(message.owner ?? null);
                setMembers(message.members ?? []);
                setActiveCount(message.activeCount ?? 0);
                setTotalCount(message.totalCount ?? 0);
                setSuspendDeadline(message.suspendDeadline ?? null);
                return;
              case "channel.cleared":
                setMessages([]);
                messagesRef.current = [];
                lastSyncedMessageIdRef.current = null;
                resumeCursorRef.current = null;
                needsBackfillRef.current = false;
                pendingTTSPartsRef.current.clear();
                return;
              case "channel.suspended":
                setSuspended(true);
                setSuspendDeadline(null);
                return;
              case "channel.resumed":
                setSuspended(false);
                return;
              case "channel.operating":
                // 「営業中」ボタンによる営業開始／延長、またはオーナー復帰による営業再開。
                // 営業中に戻し、終了予定時刻を反映する。
                setSuspended(false);
                setSuspendDeadline(message.suspendDeadline ?? null);
                return;
              case "channel.operating.paused":
                // オーナー退出で営業時間を一時停止。自動閉店（猶予）カウントダウンへ切り替える。
                setSuspended(false);
                setSuspendDeadline(message.suspendDeadline ?? null);
                return;
              case "channel.operating.mode":
                // 「時間制限なし」設定の変更。有効化されたら残りカウントダウンを消す。
                setOperatingUnlimited(message.operatingUnlimited ?? false);
                if (message.operatingUnlimited) {
                  setSuspendDeadline(null);
                }
                return;
              case "channel.kicked":
                // 準備中への切り替えでキックされた。Deckではカラムを準備中カードに切替、
                // 通常表示では一覧へ戻して開店通知の案内を出す。
                if (embeddedRef.current) {
                  onSuspendedRef.current?.("closed");
                } else {
                  navigate("/", { state: { notifyPrompt: { slug, reason: "closed" } } });
                }
                return;
              case "tts_part_ready":
                handleTTSPartReady(message);
                return;
              case "tts_ready":
                handleTTSReady(message.messageId, message.parts);
                return;
              case "tts_queued":
                handleTTSQueued(message.messageId, message.partCount);
                return;
              case "tts_skipped":
              case "tts_error":
                pendingTTSPartsRef.current.delete(message.messageId);
                return;
              case "chat.reaction":
                // 該当投稿のリアクション集計を差し替える。
                setMessages((current) =>
                  current.map((m) =>
                    m.type === "chat.message" && m.id === message.messageId
                      ? { ...m, reactions: message.reactions }
                      : m,
                  ),
                );
                return;
              default:
                setMessages((current) => {
                  // 補完で先に取り込んだ投稿との重複を避ける。
                  if (
                    message.type === "chat.message" &&
                    current.some((m) => m.type === "chat.message" && m.id === message.id)
                  ) {
                    return current;
                  }
                  if (message.type === "chat.message" && !needsBackfillRef.current) {
                    advanceSyncedCursor(message.id);
                  }
                  return [...current, message].slice(-200);
                });
            }
          },
          setStatus,
          config.beaconSeconds && config.beaconSeconds > 0
            ? config.beaconSeconds * 1000
            : undefined,
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : "チャンネルの読み込みに失敗しました");
      }
    }
    void start();
    return () => {
      cancelled = true;
      socketRef.current?.close();
      socketRef.current = null;
      clearBackfillRetry();
      currentAudioRef.current?.pause();
      currentAudioRef.current?.removeAttribute("src");
      currentAudioRef.current = null;
      audioPlayingRef.current = false;
      audioUnlockingRef.current = false;
      audioQueueRef.current = [];
      pendingTTSPartsRef.current.clear();
    };
  }, [navigate, slug]);

  useEffect(() => {
    ttsEnabledRef.current = ttsEnabled;
    localStorage.setItem("ttsEnabled", String(ttsEnabled));
    if (!ttsEnabled) {
      clearAudioQueue();
    }
  }, [ttsEnabled]);

  useEffect(() => {
    // Deck埋め込み時はグローバルの接続状態ピル（topBar）を汚さず、カラムへ通知する。
    if (embeddedRef.current) {
      onStatusRef.current?.(status);
      return;
    }
    window.dispatchEvent(new CustomEvent("channel-status-changed", { detail: status }));
  }, [status]);

  useEffect(() => {
    return () => {
      if (embeddedRef.current) {
        return;
      }
      window.dispatchEvent(new CustomEvent("channel-status-changed", { detail: null }));
    };
  }, []);

  useEffect(() => {
    ttsVolumeRef.current = ttsVolume;
    localStorage.setItem("ttsVolume", String(ttsVolume));
    if (currentAudioRef.current) {
      currentAudioRef.current.volume = ttsVolume;
    }
  }, [ttsVolume]);

  useEffect(() => {
    if (!ttsEnabled) {
      return;
    }
    const handleInteraction = () => {
      if (audioQueueRef.current.length > 0) {
        playNextAudio();
      } else {
        unlockAudioPlayback();
      }
    };
    window.addEventListener("pointerdown", handleInteraction, { capture: true, passive: true });
    window.addEventListener("touchend", handleInteraction, { capture: true, passive: true });
    window.addEventListener("keydown", handleInteraction, { capture: true });
    return () => {
      window.removeEventListener("pointerdown", handleInteraction, { capture: true });
      window.removeEventListener("touchend", handleInteraction, { capture: true });
      window.removeEventListener("keydown", handleInteraction, { capture: true });
    };
  }, [ttsEnabled]);

  // スクロール操作を監視し、最下部に到達した時刻を記録する。
  // 最下部から離れたら 0 に戻し、最下部に来たらそのときの時刻を記録する。
  useEffect(() => {
    const el = listRef.current;
    if (!el) {
      return;
    }
    const handleScroll = () => {
      // 端数や行の高さで完全一致しないため、しきい値（48px）で「最下部」とみなす。
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      if (distanceFromBottom <= SCROLL_BOTTOM_THRESHOLD) {
        if (atBottomSinceRef.current === 0) {
          atBottomSinceRef.current = Date.now();
        }
      } else {
        atBottomSinceRef.current = 0;
      }
      setShowScrollToBottom(shouldShowScrollBottomButton(el));
    };
    handleScroll();
    el.addEventListener("scroll", handleScroll, { passive: true });
    const resizeObserver = new ResizeObserver(handleScroll);
    resizeObserver.observe(el);
    return () => {
      el.removeEventListener("scroll", handleScroll);
      resizeObserver.disconnect();
    };
  }, []);

  useLayoutEffect(() => {
    const el = listRef.current;
    if (!el) {
      return;
    }
    const raf = requestAnimationFrame(() => {
      setShowScrollToBottom(shouldShowScrollBottomButton(el));
    });
    return () => cancelAnimationFrame(raf);
  }, [messages.length, mobileComposerOpen, pendingImageUploads]);

  // 初回ロード・新着ポスト時に最下部へスクロールする。
  // - 初回ロード時は一度だけ最下部へ寄せる。
  // - 新着時は「スクロールバーがある」かつ「最下部に1秒以上いる」ときだけ自動スクロール。
  // - 画像/OGPのロードで高さが変わるため、レンダリング完了（画像ロード）後にスクロールする。
  //   その待機中に手動でスクロールされたらキャンセルする。
  useLayoutEffect(() => {
    const el = listRef.current;
    if (!el) {
      return;
    }

    const isFirstScroll = !initialScrolledRef.current;
    if (isFirstScroll) {
      if (messages.length === 0) {
        return;
      }
      initialScrolledRef.current = true;
    } else {
      // 新着ポスト。条件（スクロール可能・最下部に1秒以上）を満たさなければ何もしない。
      const scrollable = el.scrollHeight > el.clientHeight + 4;
      const since = atBottomSinceRef.current;
      const stable = since !== 0 && Date.now() - since >= SCROLL_STABLE_MS;
      if (!scrollable || !stable) {
        return;
      }
    }

    // 最下部へ追従する。OGPは取得後に<img>が遅れて挿入され高さが変わるため、
    // 変化（DOM挿入・画像ロード）が落ち着くまで最下部へ貼り付ける。上へスクロールで中止。
    return followBottom(el);
  }, [messages]);

  useEffect(() => {
    if (mobileComposerOpen) {
      return;
    }
    const el = listRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
      atBottomSinceRef.current = Date.now();
    }
  }, [mobileComposerOpen]);

  useEffect(() => {
    if (!isHandheldOS || !mobileComposerOpen) {
      setMobileKeyboardInset(0);
      return;
    }
    function updateKeyboardInset() {
      const viewport = window.visualViewport;
      if (!viewport) {
        setMobileKeyboardInset(0);
        return;
      }
      setMobileKeyboardInset(Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop));
    }
    updateKeyboardInset();
    window.visualViewport?.addEventListener("resize", updateKeyboardInset);
    window.visualViewport?.addEventListener("scroll", updateKeyboardInset);
    const id = window.requestAnimationFrame(() => {
      focusMobileTextarea();
      updateKeyboardInset();
    });
    return () => {
      window.cancelAnimationFrame(id);
      window.visualViewport?.removeEventListener("resize", updateKeyboardInset);
      window.visualViewport?.removeEventListener("scroll", updateKeyboardInset);
    };
  }, [isHandheldOS, mobileComposerOpen]);

  // カラム（埋め込み時）またはビューポート（単独時）の高さを監視し、
  // COMPACT_COMPOSER_MAX_HEIGHT を下回るあいだ compactHeight を立てる。
  useEffect(() => {
    const el = chatPageRef.current;
    if (!el) {
      return;
    }
    // Deck埋め込み時は親カラム（.deckModule）の高さ、単独表示時はビューポート高さで判定する。
    const column = el.closest(".deckModule") as HTMLElement | null;
    if (column) {
      const observer = new ResizeObserver((entries) => {
        for (const entry of entries) {
          setCompactHeight(entry.contentRect.height < COMPACT_COMPOSER_MAX_HEIGHT);
        }
      });
      observer.observe(column);
      return () => observer.disconnect();
    }
    const update = () => setCompactHeight(window.innerHeight < COMPACT_COMPOSER_MAX_HEIGHT);
    update();
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);

  // ボタン方式でなくなったら（背高に戻る/PC化）開きかけの投稿フォームを閉じておく。
  // これをしないと再び低くしたとき即座にオーバーレイが開いてしまう。
  useEffect(() => {
    if (!useComposerButton) {
      setMobileComposerOpen(false);
      setMobileKeyboardInset(0);
    }
  }, [useComposerButton]);

  // サスペンドまでの残り秒数を1秒ごとに更新する。
  useEffect(() => {
    if (!suspendDeadline) {
      setSecondsLeft(0);
      return;
    }
    const target = new Date(suspendDeadline).getTime();
    function tick() {
      setSecondsLeft(Math.max(0, Math.ceil((target - Date.now()) / 1000)));
    }
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [suspendDeadline]);

  // 取りこぼし補完のため、表示中メッセージの最新IDを常に参照できるようにする。
  useEffect(() => {
    messagesRef.current = messages;
  }, [messages]);

  function captureResumeCursor() {
    const cursor = lastSyncedMessageIdRef.current ?? latestChatMessageId(messagesRef.current);
    if (!cursor) {
      return;
    }
    resumeCursorRef.current = cursor;
    needsBackfillRef.current = true;
  }

  function clearBackfillRetry() {
    if (backfillRetryTimerRef.current) {
      window.clearTimeout(backfillRetryTimerRef.current);
      backfillRetryTimerRef.current = undefined;
    }
  }

  function scheduleBackfillRetry() {
    if (backfillRetryTimerRef.current || document.visibilityState !== "visible") {
      return;
    }
    backfillRetryTimerRef.current = window.setTimeout(() => {
      backfillRetryTimerRef.current = undefined;
      void backfillMessages(true);
    }, BACKFILL_RETRY_MS);
  }

  function advanceSyncedCursor(messageId: string | null) {
    const next = maxMessageId(lastSyncedMessageIdRef.current, messageId);
    if (!next) {
      return;
    }
    lastSyncedMessageIdRef.current = next;
    if (!needsBackfillRef.current) {
      resumeCursorRef.current = next;
    }
  }

  // スリープ復帰や再接続時に、休止前のカーソル以降を取得して取りこぼしを補完する。
  async function backfillMessages(force = false) {
    const startCursor =
      resumeCursorRef.current ?? lastSyncedMessageIdRef.current ?? latestChatMessageId(messagesRef.current);
    if (!startCursor || backfillingRef.current || (!needsBackfillRef.current && !force)) {
      return;
    }
    clearBackfillRetry();
    backfillingRef.current = true;
    let cursor = startCursor;
    const incoming: ServerToClientMessage[] = [];
    try {
      for (;;) {
        const res = await listMessages(slug, cursor);
        if (res.messages.length === 0) {
          break;
        }
        incoming.push(...res.messages);
        const nextCursor = latestChatMessageId(res.messages);
        if (!nextCursor || nextCursor === cursor) {
          break;
        }
        cursor = nextCursor;
        if (res.messages.length < BACKFILL_PAGE_SIZE) {
          break;
        }
      }
      const fetchedLatest = latestChatMessageId(incoming);
      const visibleLatest = latestChatMessageId(messagesRef.current);
      if (incoming.length > 0) {
        setMessages((current) => mergeMessages(current, incoming));
      }
      needsBackfillRef.current = false;
      advanceSyncedCursor(maxMessageId(visibleLatest, fetchedLatest));
    } catch {
      // 補完に失敗しても致命的ではない。表示中なら少し待って再試行する。
      scheduleBackfillRetry();
    } finally {
      backfillingRef.current = false;
    }
  }

  // スマホでアプリ切替・長時間放置からの復帰時に取りこぼしを補完する。
  useEffect(() => {
    function onVisibilityChange() {
      if (document.visibilityState === "visible") {
        void backfillMessages(true);
      } else {
        captureResumeCursor();
      }
    }
    function onResume() {
      if (document.visibilityState === "visible") {
        void backfillMessages(true);
      }
    }
    function onPageShow(event: PageTransitionEvent) {
      if (event.persisted) {
        void backfillMessages(true);
        return;
      }
      onResume();
    }
    document.addEventListener("visibilitychange", onVisibilityChange);
    window.addEventListener("pagehide", captureResumeCursor);
    window.addEventListener("pageshow", onPageShow);
    window.addEventListener("focus", onResume);
    window.addEventListener("online", onResume);
    return () => {
      document.removeEventListener("visibilitychange", onVisibilityChange);
      window.removeEventListener("pagehide", captureResumeCursor);
      window.removeEventListener("pageshow", onPageShow);
      window.removeEventListener("focus", onResume);
      window.removeEventListener("online", onResume);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug]);

  // WebSocketが（再）接続して open になったタイミングでも補完する。
  // 復帰直後の補完とWS再購読の間に届いたメッセージの取りこぼしを防ぐ。
  const prevStatusRef = useRef<WebSocketStatus>(status);
  useEffect(() => {
    if (status !== "open" && prevStatusRef.current === "open") {
      captureResumeCursor();
    }
    if (status === "open" && prevStatusRef.current !== "open") {
      void backfillMessages();
    }
    prevStatusRef.current = status;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  function send(body: string) {
    socketRef.current?.send({ type: "chat.message", body });
  }

  // 投稿への絵文字リアクションをトグルする（サーバ側で追加/解除を判定）。
  function toggleReaction(messageId: string, emoji: string) {
    socketRef.current?.send({ type: "reaction.toggle", messageId, emoji });
  }

  // 画像/動画のアップロードを非同期で行い、完了後にトークン付きで投稿する。
  // 処理中もユーザーは別の投稿を続けられる（送信欄は即座にクリアされる）。
  // 静止画はクライアントでリサイズ・JPEG化し、GIF/APNG/MP4は生のままサーバへ送る
  //（サーバ側で音声なしMP4へ変換される）。
  async function sendImage(file: File, caption: string) {
    setImageUploadError("");
    setPendingImageUploads((n) => n + 1);
    try {
      const animated = await isAnimatedMedia(file);
      if (animated && file.size > MEDIA_MAX_BYTES) {
        throw new Error("ファイルが大きすぎます（最大20MB）");
      }
      const uploaded = animated
        ? await uploadChannelImage(slug, file, file.name)
        : await uploadChannelImage(slug, await resizeImageToJpeg(file));
      socketRef.current?.send({
        type: "chat.message",
        body: caption,
        imageToken: uploaded.imageToken,
      });
    } catch (err) {
      setImageUploadError(
        err instanceof Error ? err.message : "送信に失敗しました",
      );
    } finally {
      setPendingImageUploads((n) => Math.max(0, n - 1));
    }
  }

  function openMobileComposer() {
    flushSync(() => {
      setMobileComposerOpen(true);
    });
    focusMobileTextarea();
  }

  function scrollToBottom() {
    const el = listRef.current;
    if (!el) {
      return;
    }
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    atBottomSinceRef.current = Date.now();
    setShowScrollToBottom(false);
  }

  function closeMobileComposer() {
    mobileTextareaRef.current?.blur();
    setMobileKeyboardInset(0);
    setMobileComposerOpen(false);
  }

  function focusMobileTextarea() {
    const textarea = mobileTextareaRef.current;
    if (!textarea) {
      return;
    }
    try {
      textarea.focus({ preventScroll: true });
    } catch {
      textarea.focus();
    }
    const cursorPosition = textarea.value.length;
    textarea.setSelectionRange(cursorPosition, cursorPosition);
  }

  // cap は保持する最大キュー長。ライブ読み上げは古い投稿の洪水を避けるため20、
  // 「ここから読み上げる」は順序を保つため大きめにする。
  function enqueueAudio(urls: string[], cap = 20) {
    if (!ttsEnabledRef.current) {
      return;
    }
    audioQueueRef.current = [...audioQueueRef.current, ...urls].slice(-cap);
    playNextAudio();
  }

  // 「ここから読み上げる」: 指定メッセージから最新まで、各投稿の音声を順に取得して再生する。
  async function readFromMessage(messageId: string) {
    if (!ttsEnabledRef.current) {
      return;
    }
    const chatMessages = messagesRef.current.filter((m) => m.type === "chat.message");
    const startIndex = chatMessages.findIndex((m) => m.id === messageId);
    if (startIndex < 0) {
      return;
    }
    const targets = chatMessages.slice(startIndex);
    // いま再生中の音声を止めてここから読み直す。クリックはユーザー操作なので解錠も試みる。
    clearAudioQueue();
    // clearAudioQueue 後の世代を記録し、途中で「黙れ」や別の読み上げが入ったら中断する。
    const generation = ttsGenerationRef.current;
    unlockAudioPlayback();
    for (const message of targets) {
      try {
        const res = await fetchMessageTTS(slug, message.id);
        if (ttsGenerationRef.current !== generation) {
          return;
        }
        const urls = [...res.parts]
          .sort((a, b) => a.partIndex - b.partIndex)
          .map((part) => part.audioUrl);
        if (urls.length > 0) {
          enqueueAudio(urls, 1000);
        }
      } catch {
        // 1件の失敗で全体を止めない。
      }
    }
  }

  function handleTTSQueued(messageId: string, partCount: number) {
    const pending = pendingTTSPartsRef.current.get(messageId);
    if (pending) {
      pending.partCount = partCount;
      flushReadyTTSParts(messageId, pending);
      return;
    }
    pendingTTSPartsRef.current.set(messageId, {
      partCount,
      nextIndex: 0,
      parts: new Map(),
    });
  }

  function handleTTSPartReady(part: TTSPart & { messageId: string }) {
    const pending =
      pendingTTSPartsRef.current.get(part.messageId) ??
      ({
        nextIndex: 0,
        parts: new Map(),
      } satisfies PendingTTSParts);
    pending.parts.set(part.partIndex, {
      partIndex: part.partIndex,
      contentHash: part.contentHash,
      audioUrl: part.audioUrl,
      mimeType: part.mimeType,
      codec: part.codec,
      durationMs: part.durationMs,
    });
    pendingTTSPartsRef.current.set(part.messageId, pending);
    flushReadyTTSParts(part.messageId, pending);
  }

  function handleTTSReady(messageId: string, parts: TTSPart[]) {
    const pending = pendingTTSPartsRef.current.get(messageId);
    const nextIndex = pending?.nextIndex ?? 0;
    pendingTTSPartsRef.current.delete(messageId);
    enqueueAudio(
      [...parts]
        .filter((part) => part.partIndex >= nextIndex)
        .sort((a, b) => a.partIndex - b.partIndex)
        .map((part) => part.audioUrl),
    );
  }

  function flushReadyTTSParts(messageId: string, pending: PendingTTSParts) {
    const readyURLs: string[] = [];
    while (pending.parts.has(pending.nextIndex)) {
      const part = pending.parts.get(pending.nextIndex);
      if (!part) {
        break;
      }
      readyURLs.push(part.audioUrl);
      pending.parts.delete(pending.nextIndex);
      pending.nextIndex += 1;
    }
    if (readyURLs.length > 0) {
      enqueueAudio(readyURLs);
    }
    if (pending.partCount !== undefined && pending.nextIndex >= pending.partCount) {
      pendingTTSPartsRef.current.delete(messageId);
    }
  }

  function getAudioPlayer() {
    if (currentAudioRef.current) {
      currentAudioRef.current.volume = ttsVolumeRef.current;
      return currentAudioRef.current;
    }
    const audio = new Audio();
    audio.preload = "auto";
    audio.volume = ttsVolumeRef.current;
    audio.addEventListener("ended", () => {
      audioPlayingRef.current = false;
      audio.removeAttribute("src");
      audio.load();
      playNextAudio();
    });
    audio.addEventListener("error", () => {
      audioPlayingRef.current = false;
      audio.removeAttribute("src");
      audio.load();
      playNextAudio();
    });
    currentAudioRef.current = audio;
    return audio;
  }

  function unlockAudioPlayback() {
    if (!ttsEnabledRef.current || audioPlayingRef.current || audioUnlockingRef.current) {
      return;
    }
    const audio = getAudioPlayer();
    audioUnlockingRef.current = true;
    const previousVolume = audio.volume;
    audio.volume = 0;
    audio.src = SILENT_AUDIO_URL;
    audio.load();
    void audio
      .play()
      .then(() => {
        audio.pause();
        try {
          audio.currentTime = 0;
        } catch {
          // Safariでは読み込み状態によって currentTime の変更に失敗することがある。
        }
        audio.removeAttribute("src");
        audio.load();
        playNextAudio();
      })
      .catch(() => {
        // ユーザー操作扱いにならない環境では、次の操作で再試行する。
      })
      .finally(() => {
        audio.volume = previousVolume;
        audioUnlockingRef.current = false;
      });
  }

  function playNextAudio() {
    if (
      !ttsEnabledRef.current ||
      audioPlayingRef.current ||
      audioUnlockingRef.current ||
      audioQueueRef.current.length === 0
    ) {
      return;
    }
    const url = audioQueueRef.current.shift();
    if (!url) {
      return;
    }
    const audio = getAudioPlayer();
    audioPlayingRef.current = true;
    audio.src = url;
    audio.volume = ttsVolumeRef.current;
    audio.load();
    void audio.play().catch((err: unknown) => {
      audioPlayingRef.current = false;
      audio.pause();
      audio.removeAttribute("src");
      audio.load();
      if (isAudioPlaybackBlocked(err)) {
        audioQueueRef.current = [url, ...audioQueueRef.current].slice(0, 20);
        return;
      }
      playNextAudio();
    });
  }

  // clearAudioQueue はブラウザ内の読み上げキューをすべてクリアし、再生中も停止する。
  // 進行中の「ここから読み上げる」も失効させる。読み上げ自体は無効化しないため、
  // 以降に届く投稿は引き続き読み上げる。
  function clearAudioQueue() {
    ttsGenerationRef.current += 1;
    audioQueueRef.current = [];
    pendingTTSPartsRef.current.clear();
    if (currentAudioRef.current) {
      currentAudioRef.current.pause();
      currentAudioRef.current.removeAttribute("src");
      currentAudioRef.current.load();
    }
    audioPlayingRef.current = false;
    audioUnlockingRef.current = false;
  }

  function enableTTS() {
    ttsEnabledRef.current = true;
    setTtsEnabled(true);
    if (audioQueueRef.current.length > 0) {
      playNextAudio();
    } else {
      unlockAudioPlayback();
    }
  }

  function disableTTS() {
    ttsEnabledRef.current = false;
    setTtsEnabled(false);
    clearAudioQueue();
  }

  function openTTSDialog() {
    setTtsDialogOpen(true);
  }

  // 営業状態ボタンの押下。無期限/時間制限あり・営業中/準備中のいずれでも
  // ダイアログを開き、状況に応じた操作（時間選択・営業開始・準備中への切替）を提示する。
  function handleOperatingButton() {
    setOperatingEndTimeDraft(defaultEndTimeValue(60));
    setOperatingDialogOpen(true);
  }

  function openExtendDialog() {
    setExtendEndTimeDraft(endTimeValueFromDeadline(suspendDeadline));
    setExtendDialogOpen(true);
  }

  // 時間制限なしチャンネルの開店（終了予定なしで営業中へ）。
  async function openUnlimitedChannel() {
    if (operatingBusy) {
      return;
    }
    setOperatingBusy(true);
    setError("");
    try {
      await openChannel(slug);
      setOperatingDialogOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "開店に失敗しました");
    } finally {
      setOperatingBusy(false);
    }
  }

  // 選んだ営業時間（分）で営業を開始する。状態反映はWebSocket（channel.operating）に任せる。
  async function startOperatingWith(durationMinutes: number) {
    if (operatingBusy) {
      return;
    }
    setOperatingBusy(true);
    setError("");
    try {
      await startOperating(slug, durationMinutes);
      setOperatingDialogOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "営業の開始に失敗しました");
    } finally {
      setOperatingBusy(false);
    }
  }

  async function setOperatingEndTime(endTime: string, close: "start" | "extend") {
    if (operatingBusy) {
      return;
    }
    const durationMinutes = durationMinutesUntil(endTime);
    if (!durationMinutes) {
      setError("終了時刻を入力してください");
      return;
    }
    setOperatingBusy(true);
    setError("");
    try {
      await setOperatingDuration(slug, durationMinutes);
      if (close === "start") {
        setOperatingDialogOpen(false);
      } else {
        setExtendDialogOpen(false);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "終了時刻の設定に失敗しました");
    } finally {
      setOperatingBusy(false);
    }
  }

  // 営業を即座に終了して準備中へ移行する。
  async function suspendNow() {
    if (operatingBusy) {
      return;
    }
    setOperatingBusy(true);
    setError("");
    try {
      await suspendChannelNow(slug);
      setOperatingDialogOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "準備中への移行に失敗しました");
    } finally {
      setOperatingBusy(false);
    }
  }

  // 営業時間を延長する。
  async function extendOperatingBy(minutes: number) {
    if (operatingBusy) {
      return;
    }
    setOperatingBusy(true);
    setError("");
    try {
      await extendOperating(slug, minutes);
      setExtendDialogOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "営業の延長に失敗しました");
    } finally {
      setOperatingBusy(false);
    }
  }

  // このチャンネルの営業開始通知をオン/オフする。
  async function toggleNotify() {
    if (notifyBusy) {
      return;
    }
    setNotifyBusy(true);
    setError("");
    try {
      const res = await setChannelNotify(slug, !notifyEnabled);
      setNotifyEnabled(res.enabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : "通知設定の変更に失敗しました");
    } finally {
      setNotifyBusy(false);
    }
  }

  function turnOffTTSFromDialog() {
    disableTTS();
    setTtsDialogOpen(false);
  }

  function turnOnTTSFromDialog() {
    enableTTS();
    setTtsDialogOpen(false);
  }

  async function deleteMessage(messageId: string) {
    if (!window.confirm("このメッセージを削除しますか？")) {
      return;
    }
    setError("");
    try {
      await deleteChannelMessage(slug, messageId);
      setMessages((current) =>
        current.filter((message) => message.type !== "chat.message" || message.id !== messageId),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "メッセージの削除に失敗しました");
    }
  }

  // 準備中（＝営業中でない）ときだけ、オーナーへ営業中への切替を促す噴きだしを出す。
  const showOperatingHint =
    isOwner && suspended && !operatingHintDismissedForever && !operatingHintClosed;

  function closeOperatingHint() {
    if (operatingHintDontShow) {
      setOperatingHintDismissed(true);
    }
    setOperatingHintClosed(true);
  }

  // 操作ボタン群。順番は「営業中・読み上げ・通知」を左に並べ、「設定」だけ右寄せ。
  const actionButtons = (
    <>
      {isOwner ? (
        <div className="ttsControls operatingControl" aria-label="営業状態">
          <button
            type="button"
            className="ttsToggle operatingToggle"
            onClick={handleOperatingButton}
            disabled={operatingBusy}
            title="クリックで営業状態を切り替えます"
          >
            <span className={`ttsLed ${operatingLed}`} aria-hidden="true" />
            営業中
          </button>
          {showOperatingHint ? (
            <div className="operatingHintBubble" role="dialog" aria-label="準備中のお知らせ">
              <p className="operatingHintText">
                現在このチャンネルは「準備中」になっているため、あなた以外の人が入ってくることができません。「営業中」に設定して、チャンネルをオープンにしましょう。
              </p>
              <div className="operatingHintFooter">
                <label className="operatingHintDontShow">
                  <input
                    type="checkbox"
                    checked={operatingHintDontShow}
                    onChange={(event) => setOperatingHintDontShow(event.target.checked)}
                  />
                  今後このメッセージを表示しない
                </label>
                <button
                  type="button"
                  className="operatingHintClose"
                  onClick={closeOperatingHint}
                >
                  閉じる
                </button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="ttsControls" aria-label="読み上げ設定">
        <button
          type="button"
          className={ttsEnabled ? "ttsToggle enabled" : "ttsToggle"}
          onClick={openTTSDialog}
        >
          <span className={ttsEnabled ? "ttsLed on" : "ttsLed off"} aria-hidden="true" />
          読み上げ
        </button>
      </div>
      {pushAvailable ? (
        <div className="ttsControls" aria-label="通知設定">
          <button
            type="button"
            className={notifyEnabled ? "ttsToggle enabled" : "ttsToggle"}
            onClick={() => void toggleNotify()}
            disabled={notifyBusy}
            title="このチャンネルが営業中になったら通知します"
          >
            <span className={notifyEnabled ? "ttsLed on" : "ttsLed off"} aria-hidden="true" />
            通知
          </button>
        </div>
      ) : null}
      {isOwner ? (
        <button
          type="button"
          className="buttonLink secondaryLink"
          onClick={() => setSettingsDialogOpen(true)}
        >
          設定
        </button>
      ) : null}
    </>
  );

  return (
    <>
      <div
        className={`roomHeader channelRoomHeader${usesChannelInfoDrawer ? " compactChannelRoomHeader" : ""}`}
      >
        <div className="roomHeaderMain">
          <div className="channelTitleRow">
            {embedded ? (
              <button
                type="button"
                className="channelBackButton"
                aria-label="チャンネル一覧に戻る"
                title="チャンネル一覧に戻る"
                onClick={() => onExit?.()}
              >
                ‹
              </button>
            ) : (
              <Link
                to="/"
                className="channelBackButton"
                aria-label="チャンネル一覧に戻る"
                title="チャンネル一覧に戻る"
              >
                ‹
              </Link>
            )}
            {usesChannelInfoDrawer ? (
              <button
                type="button"
                className="channelTitleButton"
                onClick={() => setInfoDialogOpen(true)}
                aria-label="チャンネル詳細を開く"
              >
                {channel?.title ?? slug}
                {accessBadge ? <span className="accessModeBadge">{accessBadge}</span> : null}
              </button>
            ) : (
              <h1>
                <button
                  type="button"
                  className="channelTitleTextButton"
                  onClick={() => setInfoDialogOpen(true)}
                  aria-label="チャンネル詳細を開く"
                >
                  {channel?.title ?? slug}
                </button>
                {accessBadge ? <span className="accessModeBadge">{accessBadge}</span> : null}
              </h1>
            )}
          </div>
          {!usesChannelInfoDrawer && channel?.description ? <p>{channel.description}</p> : null}
          {/* モバイル（ドロワー）ではヘッダー内・在室表示の上にボタンを置く。
              PC/Deckでは下のチャット領域（営業中残り時間表示の上）に枠付きで置く。 */}
          {usesChannelInfoDrawer ? (
            <div className="roomHeaderActions compactHeaderActions">{actionButtons}</div>
          ) : null}
          <ChannelPresence
            owner={owner ?? undefined}
            members={members}
            activeCount={activeCount}
            totalCount={totalCount}
          />
        </div>
      </div>
      <section
        ref={chatPageRef}
        className={`chatPage${isHandheldOS ? " handheldChatPage" : ""}`}
      >
        {!usesChannelInfoDrawer ? (
          <div className="channelActionsBar">{actionButtons}</div>
        ) : null}
        {operating ? (
          <div className="operatingStatus">
            <span className="operatingStatusLabel">営業中</span>
            <span className="operatingStatusTime">
              残り <span className="operatingStatusClock">{formatRemaining(secondsLeft)}</span>
            </span>
            {isOwner ? (
              <button
                type="button"
                className="operatingExtendButton"
                onClick={openExtendDialog}
                disabled={operatingBusy}
                aria-label="営業を延長する"
                title="営業を延長する"
              >
                ⋯
              </button>
            ) : null}
          </div>
        ) : null}
        {suspended ? (
          <p className="formNotice">このチャンネルは準備中です。</p>
        ) : null}
        {error ? <p className="formError">{error}</p> : null}
        {imageUploadError ? <p className="formError">{imageUploadError}</p> : null}
        <div
          className={`messageListSlot${useComposerButton && !ghostMode ? " hasComposeFab" : ""}`}
        >
          <ChatMessageList
            messages={messages}
            canModerateMessages={isOwner || isPrivileged(user)}
            currentUserId={user?.id}
            onDeleteMessage={deleteMessage}
            ttsEnabled={ttsEnabled}
            onReadFrom={(messageId) => void readFromMessage(messageId)}
            linkifyUrls={channel?.urlLinkifyEnabled ?? false}
            listRef={listRef}
            canReact={!ghostMode}
            onToggleReaction={toggleReaction}
          />
          {showScrollToBottom ? (
            <button
              type="button"
              className={`scrollToBottomButton${
                useComposerButton && !ghostMode ? " raised" : ""
              }`}
              onClick={scrollToBottom}
              aria-label="一番下までスクロール"
              title="一番下までスクロール"
            >
              <span className="scrollToBottomIcon" aria-hidden="true" />
            </button>
          ) : null}
          {useComposerButton && !ghostMode ? (
            <button
              type="button"
              className="composeFab"
              aria-label="メッセージを入力"
              title="書き込み"
              onClick={openMobileComposer}
            >
              <svg className="composeFabIcon" viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1 1 0 0 0 0-1.41l-2.34-2.34a1 1 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z"
                  fill="currentColor"
                />
              </svg>
            </button>
          ) : null}
        </div>
        {pendingImageUploads > 0 ? (
          <p className="imageUploadingNotice">画像を送信中...（{pendingImageUploads}件）</p>
        ) : null}
        {ghostMode ? (
          <p className="ghostNotice">👻 ゴーストモード中は閲覧のみで、書き込みはできません。</p>
        ) : null}
        {!useComposerButton && !ghostMode ? (
          <ChatInput
            disabled={status !== "open"}
            maxLength={messageMaxLength}
            onSend={send}
            imageUploadEnabled={channel?.imageUploadEnabled ?? false}
            onSendImage={sendImage}
          />
        ) : null}
        {useComposerButton && mobileComposerOpen ? (
          <div
            className="mobileComposerOverlay"
            role="presentation"
            style={{ "--mobile-keyboard-inset": `${mobileKeyboardInset}px` } as CSSProperties}
            onPointerDown={closeMobileComposer}
          >
            <div
              className="mobileComposerDialog"
              role="dialog"
              aria-modal="true"
              aria-label="メッセージ入力"
              onPointerDown={(event) => event.stopPropagation()}
            >
              <ChatInput
                disabled={status !== "open"}
                maxLength={messageMaxLength}
                body={mobileDraft}
                onBodyChange={setMobileDraft}
                onAfterSend={closeMobileComposer}
                onSend={send}
                imageUploadEnabled={channel?.imageUploadEnabled ?? false}
                onSendImage={sendImage}
                textareaRef={mobileTextareaRef}
              />
            </div>
          </div>
        ) : null}
        {ttsDialogOpen ? (
          <div
            className="ttsDialogOverlay"
            role="presentation"
            onClick={() => setTtsDialogOpen(false)}
          >
            <div
              className="ttsDialog"
              role="dialog"
              aria-modal="true"
              aria-label="読み上げ設定"
              onClick={(event) => event.stopPropagation()}
            >
              <h2 className="ttsDialogTitle">
                <span className={ttsEnabled ? "ttsLed on" : "ttsLed off"} aria-hidden="true" />
                読み上げ
              </h2>
              <label className="ttsDialogVolume">
                <span>音量</span>
                <input
                  type="range"
                  min="0"
                  max="1"
                  step="0.05"
                  value={ttsVolume}
                  onChange={(event) => setTtsVolume(Number(event.target.value))}
                />
              </label>
              <div className="ttsDialogActions">
                <button type="button" className="ttsDialogOn" onClick={turnOnTTSFromDialog}>
                  ON
                </button>
                <button type="button" className="ttsDialogOff" onClick={turnOffTTSFromDialog}>
                  OFF
                </button>
                <button type="button" className="ttsClear" onClick={clearAudioQueue}>
                  黙れ
                </button>
              </div>
            </div>
          </div>
        ) : null}
        {operatingDialogOpen ? (
          <div
            className="ttsDialogOverlay"
            role="presentation"
            onClick={() => setOperatingDialogOpen(false)}
          >
            <div
              className="ttsDialog operatingDialog"
              role="dialog"
              aria-modal="true"
              aria-label="営業時間の設定"
              onClick={(event) => event.stopPropagation()}
            >
              <h2 className="ttsDialogTitle">{operatingDialogTitle}</h2>
              {suspended && !operatingUnlimited ? (
                <>
                  <p className="operatingDialogHint">
                    選んだ時間だけ営業します。時間が経過すると自動で準備中になります。
                  </p>
                  <div className="operatingDurationGrid">
                    {OPERATING_DURATIONS.map((opt) => (
                      <button
                        key={opt.minutes}
                        type="button"
                        className="operatingDurationButton"
                        onClick={() => void startOperatingWith(opt.minutes)}
                        disabled={operatingBusy}
                      >
                        {opt.label}
                      </button>
                    ))}
                  </div>
                  <form
                    className="operatingEndTimeForm"
                    onSubmit={(event) => {
                      event.preventDefault();
                      void setOperatingEndTime(operatingEndTimeDraft, "start");
                    }}
                  >
                    <label className="operatingEndTimeField">
                      <span>終了時刻で設定</span>
                      <input
                        type="time"
                        value={operatingEndTimeDraft}
                        onChange={(event) => setOperatingEndTimeDraft(event.target.value)}
                        disabled={operatingBusy}
                        required
                      />
                    </label>
                    <button
                      type="submit"
                      className="operatingStartButton"
                      disabled={operatingBusy}
                    >
                      設定
                    </button>
                  </form>
                </>
              ) : null}
              {suspended && operatingUnlimited ? (
                <p className="operatingDialogHint">
                  営業を開始します。手動で準備中にするまで営業を続けます。
                </p>
              ) : null}
              {!suspended ? (
                <p className="operatingDialogHint">営業を終了して準備中に切り替えます。</p>
              ) : null}
              <div className="operatingDialogFooter">
                {!suspended ? (
                  <button
                    type="button"
                    className="operatingSuspendNowButton"
                    onClick={() => void suspendNow()}
                    disabled={operatingBusy}
                  >
                    今すぐ準備中にする
                  </button>
                ) : suspended && operatingUnlimited ? (
                  <button
                    type="button"
                    className="operatingStartButton"
                    onClick={() => void openUnlimitedChannel()}
                    disabled={operatingBusy}
                  >
                    営業開始
                  </button>
                ) : (
                  <span />
                )}
                <button
                  type="button"
                  className="operatingDialogClose"
                  onClick={() => setOperatingDialogOpen(false)}
                >
                  閉じる
                </button>
              </div>
            </div>
          </div>
        ) : null}
        {extendDialogOpen ? (
          <div
            className="ttsDialogOverlay"
            role="presentation"
            onClick={() => setExtendDialogOpen(false)}
          >
            <div
              className="ttsDialog operatingDialog"
              role="dialog"
              aria-modal="true"
              aria-label="営業の延長"
              onClick={(event) => event.stopPropagation()}
            >
              <h2 className="ttsDialogTitle">営業を延長</h2>
              <div className="operatingDurationGrid">
                {EXTEND_DURATIONS.map((opt) => (
                  <button
                    key={opt.minutes}
                    type="button"
                    className="operatingDurationButton"
                    onClick={() => void extendOperatingBy(opt.minutes)}
                    disabled={operatingBusy}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
              <form
                className="operatingEndTimeForm"
                onSubmit={(event) => {
                  event.preventDefault();
                  void setOperatingEndTime(extendEndTimeDraft, "extend");
                }}
              >
                <label className="operatingEndTimeField">
                  <span>終了時刻を設定</span>
                  <input
                    type="time"
                    value={extendEndTimeDraft}
                    onChange={(event) => setExtendEndTimeDraft(event.target.value)}
                    disabled={operatingBusy}
                    required
                  />
                </label>
                <button type="submit" className="operatingStartButton" disabled={operatingBusy}>
                  設定
                </button>
              </form>
              <div className="operatingDialogFooter">
                <span />
                <button
                  type="button"
                  className="operatingDialogClose"
                  onClick={() => setExtendDialogOpen(false)}
                >
                  閉じる
                </button>
              </div>
            </div>
          </div>
        ) : null}
      </section>
      {infoDialogOpen ? (
        <div
          className="channelInfoDialogOverlay"
          role="presentation"
          onClick={() => setInfoDialogOpen(false)}
        >
          <div
            className="channelInfoDialog"
            role="dialog"
            aria-modal="true"
            aria-label="チャンネル詳細"
            onClick={(event) => event.stopPropagation()}
          >
            <button
              type="button"
              className="channelInfoDialogClose"
              aria-label="閉じる"
              onClick={() => setInfoDialogOpen(false)}
            >
              ×
            </button>
            <h2 className="channelInfoDialogTitle">
              {channel?.title ?? slug}
              {accessBadge ? <span className="accessModeBadge">{accessBadge}</span> : null}
            </h2>
            <p className="channelInfoDialogDesc">
              {channel?.description ? channel.description : "チャンネルの説明はありません。"}
            </p>
            {owner ? (
              <div className="channelInfoDialogOwner">
                <button
                  type="button"
                  className="channelInfoDialogOwnerAvatar"
                  onClick={() => setInfoPopupUser(owner.user)}
                  aria-label={`${owner.user.displayName}のプロフィール`}
                >
                  <UserAvatar
                    displayName={owner.user.displayName}
                    avatarUrl={owner.user.avatarUrl}
                  />
                </button>
                <div className="channelInfoDialogOwnerMeta">
                  <span className="channelInfoDialogOwnerLabel">オーナー</span>
                  <strong>{owner.user.displayName}</strong>
                </div>
              </div>
            ) : null}
            <dl className="channelInfoList">
              <div className="channelInfoRow">
                <dt>投稿の寿命</dt>
                <dd>{labelPostTtlHours(channel)}</dd>
              </div>
              <div className="channelInfoRow">
                <dt>オーナー離席後の自動閉店</dt>
                <dd>{labelChannelGrace(channel)}</dd>
              </div>
              <div className="channelInfoRow">
                <dt>閉店から削除されるまでの時間</dt>
                <dd>{labelChannelRetention(channel)}</dd>
              </div>
              <div className="channelInfoRow">
                <dt>画像アップロード可否</dt>
                <dd>{labelFeatureEnabled(channel?.imageUploadEnabled)}</dd>
              </div>
              <div className="channelInfoRow">
                <dt>URLリンク化可否</dt>
                <dd>{labelFeatureEnabled(channel?.urlLinkifyEnabled)}</dd>
              </div>
              <div className="channelInfoRow">
                <dt>連続投稿の禁止</dt>
                <dd>{labelFeatureEnabled(channel?.noConsecutivePosts)}</dd>
              </div>
            </dl>
            <p className="channelInfoDialogPermalink">
              <a href={`/channels/${slug}`} target="_blank" rel="noreferrer noopener">
                パーマリンク
              </a>
            </p>
          </div>
        </div>
      ) : null}
      {infoPopupUser ? (
        <UserProfilePopup user={infoPopupUser} onClose={() => setInfoPopupUser(null)} />
      ) : null}
      {settingsDialogOpen ? (
        <div
          className="settingsModalOverlay"
          role="presentation"
          onClick={() => setSettingsDialogOpen(false)}
        >
          <div
            className="settingsModal channelSettingsModal"
            role="dialog"
            aria-modal="true"
            aria-label="チャンネル設定"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="settingsModalHeader">
              <h2>チャンネル設定</h2>
              <button
                type="button"
                className="settingsModalClose"
                aria-label="閉じる"
                onClick={() => setSettingsDialogOpen(false)}
              >
                ×
              </button>
            </div>
            <div className="settingsModalBody">
              <ChannelSettingsPanel
                slug={slug}
                onClose={() => setSettingsDialogOpen(false)}
                onDeleted={() => {
                  setSettingsDialogOpen(false);
                  if (embedded) {
                    onExit?.();
                  } else {
                    navigate("/");
                  }
                }}
              />
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}

function isPrivileged(user: User | null) {
  return user?.role === "owner" || user?.role === "admin";
}

// 表示中メッセージの中で最大のチャットメッセージID（数値）を返す。
function latestChatMessageId(list: ServerToClientMessage[]): string | null {
  let max: bigint | null = null;
  let maxStr: string | null = null;
  for (const m of list) {
    if (m.type !== "chat.message") {
      continue;
    }
    let value: bigint;
    try {
      value = BigInt(m.id);
    } catch {
      continue;
    }
    if (max === null || value > max) {
      max = value;
      maxStr = m.id;
    }
  }
  return maxStr;
}

function maxMessageId(a: string | null, b: string | null) {
  if (!a) {
    return b;
  }
  if (!b) {
    return a;
  }
  try {
    return BigInt(a) >= BigInt(b) ? a : b;
  } catch {
    return b;
  }
}

// 既存メッセージに新規取得分を重複なく取り込み、チャットメッセージをID順に整える。
function mergeMessages(
  current: ServerToClientMessage[],
  incoming: ServerToClientMessage[],
): ServerToClientMessage[] {
  const byID = new Map<string, ServerToClientMessage>();
  const nonChatMessages: ServerToClientMessage[] = [];
  for (const m of current) {
    if (m.type === "chat.message") {
      byID.set(m.id, m);
    } else {
      nonChatMessages.push(m);
    }
  }
  for (const m of incoming) {
    if (m.type === "chat.message" && !byID.has(m.id)) {
      byID.set(m.id, m);
    }
  }
  const chatMessages = [...byID.values()].sort(compareChatMessageID);
  return [...nonChatMessages, ...chatMessages].slice(-200);
}

function compareChatMessageID(a: ServerToClientMessage, b: ServerToClientMessage) {
  if (a.type !== "chat.message" || b.type !== "chat.message") {
    return 0;
  }
  try {
    const av = BigInt(a.id);
    const bv = BigInt(b.id);
    if (av === bv) {
      return 0;
    }
    return av < bv ? -1 : 1;
  } catch {
    return a.createdAt.localeCompare(b.createdAt);
  }
}

function isAudioPlaybackBlocked(err: unknown) {
  if (err instanceof DOMException) {
    return err.name === "NotAllowedError" || err.name === "AbortError";
  }
  return false;
}

// ルータ用の薄いラッパ。URLの :slug を渡し、通常のフルページ表示にする（モバイル/クラシック）。
export default function ChannelPage() {
  const { slug = "" } = useParams();
  return <ChannelView slug={slug} />;
}

function detectHandheldOS() {
  if (typeof navigator === "undefined") {
    return false;
  }
  const ua = navigator.userAgent;
  const isAndroid = /Android/i.test(ua);
  const isiOS = /iPhone|iPad|iPod/i.test(ua);
  const isiPadOS = navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1;
  return isAndroid || isiOS || isiPadOS;
}

function detectChannelInfoDrawerOS() {
  if (typeof navigator === "undefined") {
    return false;
  }
  const ua = navigator.userAgent;
  const isAndroid = /Android/i.test(ua);
  const isiPhone = /iPhone|iPod/i.test(ua);
  return isAndroid || isiPhone;
}
