import { Fragment, useRef, useState, type ReactNode, type Ref } from "react";

import { parseYouTube } from "../lib/youtube";
import { recordRecentEmoji } from "../lib/reactionRecents";
import type { ReactionUser, ServerToClientMessage } from "../types/chat";
import EmojiPalette from "./EmojiPalette";
import LinkPreviewCard from "./LinkPreviewCard";
import UserAvatar from "./UserAvatar";
import UserProfilePopup, { type PopupUser } from "./UserProfilePopup";
import YouTubeEmbed from "./YouTubeEmbed";

type PopupImage = {
  url: string;
  width?: number;
  height?: number;
  isVideo?: boolean;
};

type Props = {
  messages: ServerToClientMessage[];
  // canModerateMessages はチャンネルオーナー/管理者。どの投稿も削除できる。
  canModerateMessages?: boolean;
  // canDownloadMedia が true のとき、添付動画・画像のダウンロード項目を出す（オーナー限定）。
  canDownloadMedia?: boolean;
  // currentUserId は閲覧中のユーザー。自分の投稿は削除できる。
  currentUserId?: string;
  onDeleteMessage?: (messageId: string) => void;
  // ttsEnabled が false のときは「ここから読み上げる」をグレーアウトする。
  ttsEnabled?: boolean;
  // 現在読み上げ中の投稿ID。該当投稿のアバターを跳ねさせる。
  speakingMessageId?: string | null;
  onReadFrom?: (messageId: string) => void;
  linkifyUrls?: boolean;
  listRef?: Ref<HTMLDivElement>;
  // canReact が true のときリアクションの追加・トグルができる（ゴースト時は false）。
  canReact?: boolean;
  onToggleReaction?: (messageId: string, emoji: string) => void;
};

export default function ChatMessageList({
  messages,
  canModerateMessages = false,
  canDownloadMedia = false,
  currentUserId,
  onDeleteMessage,
  ttsEnabled = false,
  speakingMessageId = null,
  onReadFrom,
  linkifyUrls = false,
  listRef,
  canReact = false,
  onToggleReaction,
}: Props) {
  const [popupUser, setPopupUser] = useState<PopupUser | null>(null);
  const [popupImage, setPopupImage] = useState<PopupImage | null>(null);
  // 時刻タップで開くアクションメニューの対象メッセージID。
  const [menuMessageId, setMenuMessageId] = useState<string | null>(null);
  // メニューを開いた投稿が削除可能か（メニュー表示時に確定する）。
  const [menuCanDelete, setMenuCanDelete] = useState(false);
  // メニューを開いた投稿の添付メディア（画像/動画）。無ければ null。
  const [menuMedia, setMenuMedia] = useState<{ url: string; isVideo: boolean } | null>(null);
  // ダウンロード中はメニュー項目を無効化して二重押しを防ぐ。
  const [downloading, setDownloading] = useState(false);
  // 絵文字パレットを開いている投稿IDと、開く向き（上/下）。
  const [paletteMessageId, setPaletteMessageId] = useState<string | null>(null);
  // パレットを寄せる基準ボタン（クリックされた「リアクション追加」ボタン）。
  const [paletteAnchor, setPaletteAnchor] = useState<HTMLElement | null>(null);
  // リアクション長押しの「誰が」ポップアップ。
  const [whoPopup, setWhoPopup] = useState<{ emoji: string; users: ReactionUser[] } | null>(null);
  const longPressTimer = useRef<number | undefined>(undefined);
  const longPressedRef = useRef(false);

  function startLongPress(emoji: string, users: ReactionUser[]) {
    longPressedRef.current = false;
    window.clearTimeout(longPressTimer.current);
    longPressTimer.current = window.setTimeout(() => {
      longPressedRef.current = true;
      setWhoPopup({ emoji, users });
    }, 450);
  }
  function cancelLongPress() {
    window.clearTimeout(longPressTimer.current);
  }
  function chipClick(messageId: string, emoji: string) {
    // 長押しで「誰が」を出した直後のクリックはトグルさせない。
    if (longPressedRef.current) {
      longPressedRef.current = false;
      return;
    }
    if (canReact) {
      onToggleReaction?.(messageId, emoji);
    }
  }
  function pickEmoji(messageId: string, emoji: string) {
    setPaletteMessageId(null);
    onToggleReaction?.(messageId, emoji);
    recordRecentEmoji(emoji);
  }

  // downloadMedia は添付メディアを取得してブラウザに保存させる。ダウンロード属性付き
  // アンカーだけでは（同一オリジンでもContent-Dispositionによっては）別タブで開くだけの
  // 環境があるため、いったん blob へ取得してから object URL で保存を確実にする。
  async function downloadMedia(url: string, isVideo: boolean) {
    setDownloading(true);
    try {
      const res = await fetch(url);
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      const blob = await res.blob();
      const objectUrl = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = objectUrl;
      a.download = downloadName(url, isVideo, blob.type);
      document.body.appendChild(a);
      a.click();
      a.remove();
      // クリック後すぐに revoke するとダウンロードが始まらない環境があるため少し遅らせる。
      setTimeout(() => URL.revokeObjectURL(objectUrl), 10_000);
    } catch {
      // 取得に失敗した場合は、最終手段として別タブで開く（右クリック保存できる）。
      window.open(url, "_blank", "noopener,noreferrer");
    } finally {
      setDownloading(false);
    }
  }

  return (
    <div className="messageList" aria-live="polite" ref={listRef}>
      {messages.length === 0 ? <p className="emptyState">まだメッセージはありません。</p> : null}
      {messages.map((message, index) => {
        if (message.type === "chat.message") {
          // 自分の投稿、またはオーナー/管理者ならどの投稿でも削除できる。
          const canDelete =
            canModerateMessages || (!!currentUserId && message.user.id === currentUserId);
          // 自分の発言は左右反転（アバター右端・テキスト右寄せ）で表示する。
          const isOwn = !!currentUserId && message.user.id === currentUserId;
          // リンク化が有効なチャンネルでは、本文の最初のURLのOGPプレビューを表示する。
          // 画像のみの投稿は body が undefined のため、その場合は何もしない。
          const previewUrl = linkifyUrls && message.body ? firstUrl(message.body) : null;
          // YouTubeリンクは埋め込みプレイヤーを表示し、OGPカードは出さない。
          const youtube = previewUrl ? parseYouTube(previewUrl) : null;
          // 読み上げ中の投稿はアバターを跳ねさせる。
          const isSpeaking = message.id === speakingMessageId;
          return (
            <article className={isOwn ? "messageRow own" : "messageRow"} key={message.id}>
              <button
                type="button"
                className={isSpeaking ? "avatarButton speaking" : "avatarButton"}
                aria-label={`${message.user.displayName}のプロフィール`}
                onClick={() => setPopupUser(message.user)}
              >
                <UserAvatar
                  displayName={message.user.displayName}
                  avatarUrl={message.user.avatarUrl}
                />
              </button>
              <div className="messageBody">
                <div className="messageMeta">
                  <time dateTime={message.createdAt}>{formatDateTime(message.createdAt)}</time>
                  <button
                    type="button"
                    className="messageMenuButton"
                    onClick={() => {
                      setMenuCanDelete(canDelete);
                      setMenuMedia(
                        message.imageUrl
                          ? { url: message.imageUrl, isVideo: message.mediaKind === "video" }
                          : null,
                      );
                      setMenuMessageId(message.id);
                    }}
                    aria-label="この投稿のメニューを開く"
                    title="この投稿のメニュー（読み上げ・削除）"
                  >
                    ⋯
                  </button>
                </div>
                <div className="messageContent">
                {message.imageUrl ? (
                  <button
                    type="button"
                    className="messageImageButton"
                    onClick={() =>
                      setPopupImage({
                        url: message.imageUrl!,
                        width: message.imageWidth,
                        height: message.imageHeight,
                        isVideo: message.mediaKind === "video",
                      })
                    }
                    aria-label={message.mediaKind === "video" ? "添付動画を拡大" : "添付画像を拡大"}
                  >
                    {message.mediaKind === "video" ? (
                      // 音声なしのループ動画として自動再生する（GIF/APNG/MP4をMP4化したもの）。
                      <video
                        className="messageImage"
                        src={message.imageUrl}
                        width={message.imageWidth || undefined}
                        height={message.imageHeight || undefined}
                        muted
                        loop
                        autoPlay
                        playsInline
                      />
                    ) : (
                      <img
                        className="messageImage"
                        src={message.imageUrl}
                        width={message.imageWidth || undefined}
                        height={message.imageHeight || undefined}
                        alt="添付画像"
                        loading="lazy"
                      />
                    )}
                  </button>
                ) : null}
                {message.body ? <p>{renderBody(message.body, linkifyUrls)}</p> : null}
                {youtube ? (
                  <YouTubeEmbed video={youtube} />
                ) : previewUrl ? (
                  <LinkPreviewCard url={previewUrl} />
                ) : null}
                </div>
                {(message.reactions?.length || canReact) ? (
                  <div className="reactionBar">
                    {(message.reactions ?? []).map((group) => {
                      const reactedByMe =
                        !!currentUserId && group.users.some((u) => u.userId === currentUserId);
                      return (
                        <button
                          type="button"
                          key={group.emoji}
                          className={`reactionChip${reactedByMe ? " reacted" : ""}`}
                          onPointerDown={() => startLongPress(group.emoji, group.users)}
                          onPointerUp={cancelLongPress}
                          onPointerLeave={cancelLongPress}
                          onClick={() => chipClick(message.id, group.emoji)}
                          title="クリックでリアクション、長押しで誰がしたかを表示"
                        >
                          <span className="reactionEmoji">{group.emoji}</span>
                          <span className="reactionCount">{group.count}</span>
                        </button>
                      );
                    })}
                    {canReact ? (
                      <div className="reactionAddWrap">
                        <button
                          type="button"
                          className="reactionAddButton"
                          onClick={(event) => {
                            if (paletteMessageId === message.id) {
                              setPaletteMessageId(null);
                              return;
                            }
                            // クリックしたボタンを基準に、画面内へ収まる位置へパレットを出す。
                            setPaletteAnchor(event.currentTarget);
                            setPaletteMessageId(message.id);
                          }}
                          aria-label="リアクションを追加"
                          title="リアクションを追加"
                        >
                          😃
                        </button>
                        {paletteMessageId === message.id ? (
                          <EmojiPalette
                            anchorEl={paletteAnchor}
                            onSelect={(emoji) => pickEmoji(message.id, emoji)}
                            onClose={() => setPaletteMessageId(null)}
                          />
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>
            </article>
          );
        }
        if (message.type === "system.notice") {
          return (
            <div className="notice" key={`${message.createdAt}-${index}`}>
              {message.body}
            </div>
          );
        }
        if (message.type === "error") {
          return (
            <div className="notice errorNotice" key={`error-${index}`}>
              {message.message}
            </div>
          );
        }
        return null;
      })}
      {whoPopup ? (
        <div
          className="reactionWhoOverlay"
          role="presentation"
          onClick={() => setWhoPopup(null)}
        >
          <div
            className="reactionWhoPopup"
            role="dialog"
            aria-label="リアクションした人"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="reactionWhoHeader">
              <span className="reactionWhoEmoji">{whoPopup.emoji}</span>
              <span className="muted">{whoPopup.users.length}</span>
            </div>
            <ul className="reactionWhoList">
              {whoPopup.users.map((u) => (
                <li key={u.userId}>{u.handle ? `@${u.handle}` : u.displayName}</li>
              ))}
            </ul>
          </div>
        </div>
      ) : null}
      {popupUser ? <UserProfilePopup user={popupUser} onClose={() => setPopupUser(null)} /> : null}
      {popupImage ? (
        <button
          type="button"
          className="imageViewerOverlay"
          onClick={() => setPopupImage(null)}
          aria-label="拡大表示を閉じる"
        >
          {popupImage.isVideo ? (
            <video
              className="imageViewerImage"
              src={popupImage.url}
              width={popupImage.width || undefined}
              height={popupImage.height || undefined}
              muted
              loop
              autoPlay
              playsInline
              controls
            />
          ) : (
            <img
              className="imageViewerImage"
              src={popupImage.url}
              width={popupImage.width || undefined}
              height={popupImage.height || undefined}
              alt="拡大画像"
            />
          )}
        </button>
      ) : null}
      {menuMessageId ? (
        <div
          className="messageMenuOverlay"
          role="presentation"
          onClick={() => setMenuMessageId(null)}
        >
          <div
            className="messageMenu"
            role="dialog"
            aria-modal="true"
            aria-label="投稿メニュー"
            onClick={(event) => event.stopPropagation()}
          >
            <button
              type="button"
              className="messageMenuItem"
              disabled={!ttsEnabled}
              onClick={() => {
                const id = menuMessageId;
                setMenuMessageId(null);
                if (id) {
                  onReadFrom?.(id);
                }
              }}
            >
              ここから読み上げる
            </button>
            {menuMedia && canDownloadMedia ? (
              <button
                type="button"
                className="messageMenuItem"
                disabled={downloading}
                onClick={() => {
                  const media = menuMedia;
                  setMenuMessageId(null);
                  if (media) {
                    void downloadMedia(media.url, media.isVideo);
                  }
                }}
              >
                {menuMedia.isVideo ? "動画をダウンロード" : "画像をダウンロード"}
              </button>
            ) : null}
            {menuCanDelete ? (
              <button
                type="button"
                className="messageMenuItem danger"
                onClick={() => {
                  const id = menuMessageId;
                  setMenuMessageId(null);
                  if (id) {
                    onDeleteMessage?.(id);
                  }
                }}
              >
                削除
              </button>
            ) : null}
            <button
              type="button"
              className="messageMenuItem"
              onClick={() => setMenuMessageId(null)}
            >
              閉じる
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

// チャンネルでURLリンク化が有効なとき、本文中のURLを<a>に変換して描画する。
// React がテキストをエスケープするため、XSSの心配はない。
const URL_PATTERN = /(https?:\/\/[^\s]+)/g;

// firstUrl は本文の最初のURL（末尾の句読点を除いたもの）を返す。
function firstUrl(body: string | undefined | null): string | null {
  if (!body) {
    return null;
  }
  const match = body.match(/https?:\/\/[^\s]+/);
  if (!match) {
    return null;
  }
  // 末尾につきがちな句読点・括弧を除く。
  return match[0].replace(/[)\]}>,.、。！？!?]+$/u, "");
}

// downloadName は保存時のファイル名を決める。URL末尾のファイル名を活かしつつ、
// 拡張子が無い場合は種別（動画/画像）とMIMEから補う。
function downloadName(url: string, isVideo: boolean, mimeType: string): string {
  let base = "media";
  try {
    const path = new URL(url, window.location.href).pathname;
    const last = path.split("/").filter(Boolean).pop();
    if (last) {
      base = decodeURIComponent(last);
    }
  } catch {
    // URL解析に失敗しても既定名で続行する。
  }
  if (/\.[a-z0-9]{2,4}$/i.test(base)) {
    return base;
  }
  const ext = extFromMime(mimeType) || (isVideo ? "mp4" : "jpg");
  return `${base}.${ext}`;
}

function extFromMime(mimeType: string): string | null {
  const map: Record<string, string> = {
    "video/mp4": "mp4",
    "video/webm": "webm",
    "image/jpeg": "jpg",
    "image/png": "png",
    "image/gif": "gif",
    "image/webp": "webp",
  };
  return map[mimeType.toLowerCase()] ?? null;
}

function renderBody(body: string, linkify: boolean): ReactNode {
  if (!linkify) {
    return body;
  }
  const segments = body.split(URL_PATTERN);
  return segments.map((segment, index) => {
    if (/^https?:\/\//.test(segment)) {
      return (
        <a key={index} href={segment} target="_blank" rel="noreferrer noopener">
          {segment}
        </a>
      );
    }
    return <Fragment key={index}>{segment}</Fragment>;
  });
}

// 「6/25 13:42」形式（月/日 24時間表記、ローカル時刻）。
function formatDateTime(value: string) {
  const d = new Date(value);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${hh}:${mm}`;
}
