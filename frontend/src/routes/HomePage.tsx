import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";

import UserAvatar from "../components/UserAvatar";
import { getChannel, getMe, getPublicConfig, listChannels, setChannelNotify } from "../lib/api";
import { connectLobbySocket } from "../lib/websocket";
import type { Channel, LobbyChannelPresence } from "../types/chat";

// 開店通知ダイアログの理由。preparing=準備中を見て/直接来た, closed=準備中に切り替わってキックされた。
type NotifyReason = "preparing" | "closed";
type NotifyDialogState = {
  slug: string;
  title: string;
  reason: NotifyReason;
  ownerUserId?: string;
};

// 一覧の在室/アクティブ状態などを反映するための再取得間隔（既定15秒）。
// 実際の値はサーバ設定 ACTIVE_POLL_SECONDS（/api/config）で調整できる。
const DEFAULT_POLL_MS = 15000;
const MIN_POLL_MS = 5000;
const MAX_POLL_MS = 300000;
// カードに表示するリスナーアバターの最大件数。
const MAX_LISTENER_AVATARS = 16;

function normalizePollMs(seconds: number | undefined) {
  if (!seconds || !Number.isFinite(seconds) || seconds <= 0) {
    return DEFAULT_POLL_MS;
  }
  return Math.min(Math.max(seconds * 1000, MIN_POLL_MS), MAX_POLL_MS);
}

// 残り営業時間を H:MM:SS / M:SS 形式に整形する。
function formatRemaining(totalSeconds: number): string {
  const s = Math.max(0, totalSeconds);
  const hours = Math.floor(s / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  const mm = String(minutes).padStart(2, "0");
  const ss = String(seconds).padStart(2, "0");
  return hours > 0 ? `${hours}:${mm}:${ss}` : `${minutes}:${ss}`;
}

// 一覧カードの時間表示エリアの内容を返す。
// 準備中: 空 / 時間制限なしで営業中: ∞ / 営業中で終了予定あり: 残りカウントダウン。
// overrideDeadline はロビー配信の締切（オーナー離席の自動閉店を含む）。あれば優先する。
function remainingText(channel: Channel, now: number, overrideDeadline?: string): string {
  if (channel.suspended) {
    return "";
  }
  if (channel.operatingUnlimited) {
    return "∞";
  }
  const iso = overrideDeadline ?? channel.operatingDeadline;
  if (!iso) {
    return "";
  }
  const secs = Math.ceil((new Date(iso).getTime() - now) / 1000);
  return secs > 0 ? formatRemaining(secs) : "";
}

type ChannelListViewProps = {
  // Deck埋め込み時にチャンネルを開くコールバック。未指定なら通常のページ遷移になる。
  onOpenChannel?: (slug: string) => void;
};

export function ChannelListView({ onOpenChannel }: ChannelListViewProps) {
  const navigate = useNavigate();
  const location = useLocation();
  // チャンネルを開く。Deck埋め込み時はカラム内で開き、通常時はページ遷移する。
  const openChannel = (slug: string) => {
    if (onOpenChannel) {
      onOpenChannel(slug);
    } else {
      navigate(`/channels/${slug}`);
    }
  };
  const [channels, setChannels] = useState<Channel[]>([]);
  const [userId, setUserId] = useState<string | null>(null);
  const [userRole, setUserRole] = useState<string | null>(null);
  const [presence, setPresence] = useState<Record<string, LobbyChannelPresence>>({});
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  // 残り営業時間のカウントダウン表示用に、1秒ごとに更新する現在時刻。
  const [now, setNow] = useState(() => Date.now());
  // 準備中チャンネルの「開店したら通知」ダイアログの対象と状態。
  const [notifyDialog, setNotifyDialog] = useState<NotifyDialogState | null>(null);
  const [notifyOn, setNotifyOn] = useState(false);
  const [notifyBusy, setNotifyBusy] = useState(false);
  const [notifyLoading, setNotifyLoading] = useState(false);
  // 遷移状態（notifyPrompt）を一度だけ処理するためのフラグ。
  const notifyPromptHandled = useRef(false);
  const mountedRef = useRef(true);
  // 再取得間隔（ms）。/api/config の activePollSeconds が届いたら差し替える。
  const [pollMs, setPollMs] = useState(DEFAULT_POLL_MS);
  // サーバ全体のホワイトリスト機能フラグ。無効時はホワイトリスト有効バッヂを出さない。
  const [whitelistEnabled, setWhitelistEnabled] = useState(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const load = useCallback(
    async (initial: boolean) => {
      if (initial) {
        setLoading(true);
      }
      try {
        if (initial) {
          const meRes = await getMe();
          if (!meRes.user) {
            window.dispatchEvent(new Event("auth-changed"));
            navigate("/login");
            return;
          }
          setUserId(meRes.user.id);
          setUserRole(meRes.user.role);
        }
        const channelsRes = await listChannels();
        if (!mountedRef.current) {
          return;
        }
        setChannels(channelsRes.channels);
        setError("");
      } catch (err) {
        if (!mountedRef.current) {
          return;
        }
        // ポーリング失敗は表示中のデータを維持し、初回読み込みのみエラー表示する。
        if (initial) {
          setError(err instanceof Error ? err.message : "読み込みに失敗しました");
        }
      } finally {
        if (initial && mountedRef.current) {
          setLoading(false);
        }
      }
    },
    [navigate],
  );

  const refreshIfVisible = useCallback(() => {
    if (document.visibilityState === "visible") {
      void load(false);
    }
  }, [load]);

  // 初回ロードと、再取得間隔（サーバ設定）の取得。
  useEffect(() => {
    void load(true);
    void getPublicConfig()
      .then((config) => {
        setPollMs(normalizePollMs(config.activePollSeconds));
        setWhitelistEnabled(!!config.whitelistEnabled);
      })
      .catch(() => {
        // 取得失敗時は既定間隔のまま。
      });
  }, [load]);

  // 定期再取得＋可視化/フォーカス復帰時の再取得。間隔は pollMs に追従する。
  useEffect(() => {
    const interval = window.setInterval(refreshIfVisible, pollMs);
    document.addEventListener("visibilitychange", refreshIfVisible);
    window.addEventListener("focus", refreshIfVisible);
    return () => {
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", refreshIfVisible);
      window.removeEventListener("focus", refreshIfVisible);
    };
  }, [refreshIfVisible, pollMs]);

  // チャンネル一覧の在室情報（オーナー在席・リスナー）をWebSocketでリアルタイム購読する。
  useEffect(() => {
    const conn = connectLobbySocket((message) => {
      if (message.type !== "lobby.presence") {
        return;
      }
      const next: Record<string, LobbyChannelPresence> = {};
      for (const ch of message.channels) {
        next[ch.channelSlug] = ch;
      }
      setPresence(next);
    });
    return () => conn.close();
  }, []);

  // 残り時間表示のため1秒ごとに現在時刻を更新する。
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  // チャンネルページから遷移してきた場合（準備中に直接アクセス／キック）、開店通知ダイアログを開く。
  useEffect(() => {
    const state = location.state as { notifyPrompt?: { slug: string; reason: NotifyReason } } | null;
    const prompt = state?.notifyPrompt;
    if (!prompt || notifyPromptHandled.current) {
      return;
    }
    notifyPromptHandled.current = true;
    // リロードや戻る操作で再表示されないよう履歴のstateをクリアする。
    window.history.replaceState(null, "");
    void openNotifyDialog({ slug: prompt.slug, reason: prompt.reason });
  }, [location.state]);

  // 「開店したら通知」ダイアログを開き、現在の登録状態・タイトルを取得する。
  async function openNotifyDialog(init: {
    slug: string;
    title?: string;
    reason: NotifyReason;
    ownerUserId?: string;
  }) {
    setNotifyDialog({
      slug: init.slug,
      title: init.title ?? init.slug,
      reason: init.reason,
      ownerUserId: init.ownerUserId,
    });
    setNotifyOn(false);
    setNotifyLoading(true);
    try {
      const res = await getChannel(init.slug);
      setNotifyDialog({
        slug: init.slug,
        title: res.channel.title,
        reason: init.reason,
        ownerUserId: res.channel.ownerUserId,
      });
      setNotifyOn(res.channel.notifyEnabled ?? false);
    } catch {
      // 取得失敗時は既定（オフ）のまま。
    } finally {
      setNotifyLoading(false);
    }
  }

  async function toggleNotify() {
    if (!notifyDialog || notifyBusy) {
      return;
    }
    setNotifyBusy(true);
    try {
      const res = await setChannelNotify(notifyDialog.slug, !notifyOn);
      setNotifyOn(res.enabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : "通知設定の変更に失敗しました");
    } finally {
      setNotifyBusy(false);
    }
  }

  return (
    <section className="contentColumn">
      <div className="sectionHeader">
        <div>
          <h1>チャンネル</h1>
          <p>{loading ? "読み込み中..." : `${channels.length}件のチャンネル`}</p>
        </div>
        <Link className="buttonLink" to="/channels/new">
          チャンネル作成
        </Link>
      </div>

      {error ? <p className="formError">{error}</p> : null}
      <div className="channelList">
        {channels.map((channel) => {
          const isOwner = !!userId && channel.ownerUserId === userId;
          const className = `channelCard${channel.suspended ? " suspended" : ""}`;
          // 入室許可バッヂは管理者・オーナー（このチャンネルの所有者）にのみ表示する。
          const canSeeAccessBadge = isOwner || userRole === "admin" || userRole === "owner";
          const accessBadge =
            canSeeAccessBadge && channel.accessMode === "whitelist" && whitelistEnabled
              ? "ホワイトリスト有効"
              : canSeeAccessBadge && channel.accessMode === "blacklist"
                ? "ブラックリスト有効"
                : null;
          const live = presence[channel.slug];
          const overrideDeadline = live?.suspendDeadline;
          const remaining = remainingText(channel, now, overrideDeadline);
          const owner = live?.owner;
          const ownerPresent = !!owner?.active;
          // オーナー離席で営業を一時停止し、自動閉店カウントダウン中か。
          // 営業終了予定(operatingDeadline)がNULL化され、ロビー配信の締切だけがある状態。
          const autoClose =
            !channel.suspended &&
            !channel.operatingUnlimited &&
            !channel.operatingDeadline &&
            !!overrideDeadline &&
            !!remaining;
          const listeners = (live?.members ?? []).slice(0, MAX_LISTENER_AVATARS);

          const body = (
            <>
              <div className="channelCardSide">
                <div
                  className={`channelOwnerAvatar${ownerPresent ? " present" : " absent"}`}
                  title={
                    owner
                      ? `オーナー: ${owner.user.displayName}${ownerPresent ? "（在室）" : "（不在）"}`
                      : "オーナー"
                  }
                >
                  <UserAvatar
                    displayName={owner?.user.displayName ?? channel.title}
                    avatarUrl={owner?.user.avatarUrl}
                  />
                </div>
                <span
                  className={`channelStatusBadge${channel.suspended ? " resting" : " open"}`}
                >
                  {channel.suspended ? "準備中" : "営業中"}
                </span>
                {remaining ? (
                  <span
                    className={`channelRemaining${autoClose ? " autoClose" : ""}`}
                    title={autoClose ? "オーナー離席中: 自動で準備中になるまで" : undefined}
                  >
                    {remaining}
                  </span>
                ) : null}
              </div>
              <div className="channelCardMain">
                <div className="channelCardHeader">
                  <strong className="channelCardTitle">
                    {channel.title}
                    {accessBadge ? <span className="accessModeBadge">{accessBadge}</span> : null}
                  </strong>
                </div>
                {channel.description ? (
                  <p className="channelCardDesc">{channel.description}</p>
                ) : null}
                <div className="channelCardListeners" aria-label="リスナー">
                  {listeners.map((member) => (
                    <span
                      key={member.user.id}
                      className={`channelListener ${member.active ? "active" : "inactive"}`}
                      title={`${member.user.displayName}${member.active ? " / アクティブ" : " / 非アクティブ"}`}
                    >
                      <UserAvatar
                        displayName={member.user.displayName}
                        avatarUrl={member.user.avatarUrl}
                      />
                    </span>
                  ))}
                </div>
              </div>
            </>
          );
          // 準備中チャンネルはクリックで「開店したら通知」ダイアログを開く。
          // 営業中チャンネルは従来どおりチャンネルへ遷移する。
          return channel.suspended ? (
            <div
              className={`${className} channelCardClickable`}
              key={channel.id}
              role="button"
              tabIndex={0}
              onClick={() =>
                void openNotifyDialog({
                  slug: channel.slug,
                  title: channel.title,
                  reason: "preparing",
                  ownerUserId: channel.ownerUserId,
                })
              }
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  void openNotifyDialog({
                    slug: channel.slug,
                    title: channel.title,
                    reason: "preparing",
                    ownerUserId: channel.ownerUserId,
                  });
                }
              }}
            >
              {body}
            </div>
          ) : (
            <a
              className={className}
              key={channel.id}
              href={`/channels/${channel.slug}`}
              onClick={(event) => {
                // 修飾クリック（新規タブ等）はブラウザ既定に任せる。
                if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) {
                  return;
                }
                event.preventDefault();
                openChannel(channel.slug);
              }}
            >
              {body}
            </a>
          );
        })}
      </div>

      {notifyDialog ? (
        <div className="ttsDialogOverlay" role="presentation" onClick={() => setNotifyDialog(null)}>
          <div
            className="ttsDialog"
            role="dialog"
            aria-modal="true"
            aria-label="開店したら通知"
            onClick={(event) => event.stopPropagation()}
          >
            <h2 className="ttsDialogTitle">
              {notifyDialog.reason === "closed"
                ? "このチャンネルは準備中になりました"
                : "このチャンネルは現在準備中です"}
            </h2>
            <p className="notifyDialogLead">
              {notifyDialog.reason === "closed"
                ? "次回を見逃さないように通知をオン！"
                : "開店を見逃さないように通知をオン！"}
              <span className="notifyDialogChannel">（{notifyDialog.title}）</span>
            </p>
            <div className="ttsControls" aria-label="開店通知">
              <button
                type="button"
                className={notifyOn ? "ttsToggle enabled" : "ttsToggle"}
                onClick={() => void toggleNotify()}
                disabled={notifyBusy || notifyLoading}
              >
                <span className={notifyOn ? "ttsLed on" : "ttsLed off"} aria-hidden="true" />
                開店したら通知
              </button>
            </div>
            <p className="muted">
              通知を受け取るには、通知が利用可能な環境でPush通知を有効にしてください。設定から有効にできます。
            </p>
            <div className="operatingDialogFooter">
              {!!userId && notifyDialog.ownerUserId === userId ? (
                <a
                  className="buttonLink secondaryLink"
                  href={`/channels/${notifyDialog.slug}`}
                  onClick={(event) => {
                    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) {
                      return;
                    }
                    event.preventDefault();
                    const slug = notifyDialog.slug;
                    setNotifyDialog(null);
                    openChannel(slug);
                  }}
                >
                  チャンネルを開く
                </a>
              ) : (
                <span />
              )}
              <button
                type="button"
                className="operatingDialogClose"
                onClick={() => setNotifyDialog(null)}
              >
                閉じる
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  );
}

// ルータ用の薄いラッパ。通常のページ遷移でチャンネルを開く（モバイル/クラシック表示）。
export default function HomePage() {
  return <ChannelListView />;
}
