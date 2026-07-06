import { type CSSProperties, useEffect, useState } from "react";
import { createPortal } from "react-dom";

import UserAvatar from "./UserAvatar";

export type PopupUser = {
  displayName: string;
  handle?: string;
  avatarUrl?: string;
  provider?: string;
  profileUrl?: string;
  ttsVoicevoxSpeaker?: {
    uuid: string;
    name: string;
    url: string;
  } | null;
};

// メッセージのユーザー情報から外部プロフィールURLを求める。
// profileUrl があればそれを優先し、無ければ provider と handle から組み立てる。
export function profileUrlFor(user: PopupUser): string | null {
  if (user.profileUrl) {
    return user.profileUrl;
  }
  const handle = user.handle;
  if (!handle) {
    return null;
  }
  // Bluesky のプロバイダ文字列はバックエンドでは "atproto"。
  if (user.provider === "atproto" || user.provider === "bluesky") {
    return `https://bsky.app/profile/${handle}`;
  }
  // mastodon / misskey は handle が user@instance 形式。
  const at = handle.indexOf("@");
  if (at > 0 && (user.provider === "mastodon" || user.provider === "misskey")) {
    return `https://${handle.slice(at + 1)}/@${handle.slice(0, at)}`;
  }
  return null;
}

type Props = {
  user: PopupUser;
  onClose: () => void;
};

export default function UserProfilePopup({ user, onClose }: Props) {
  const [safeTop, setSafeTop] = useState(0);

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    function updateSafeTop() {
      const selectors = [".topBar", ".channelRoomHeader", ".channelInfoDrawerHandle"];
      const nextSafeTop = selectors.reduce((max, selector) => {
        const el = document.querySelector<HTMLElement>(selector);
        if (!el) {
          return max;
        }
        const rect = el.getBoundingClientRect();
        if (rect.width <= 0 || rect.height <= 0) {
          return max;
        }
        return Math.max(max, rect.bottom);
      }, 0);
      setSafeTop(Math.max(0, Math.ceil(nextSafeTop)));
    }
    updateSafeTop();
    const animationFrame = window.requestAnimationFrame(updateSafeTop);
    window.addEventListener("resize", updateSafeTop);
    window.visualViewport?.addEventListener("resize", updateSafeTop);
    window.visualViewport?.addEventListener("scroll", updateSafeTop);
    return () => {
      window.cancelAnimationFrame(animationFrame);
      window.removeEventListener("resize", updateSafeTop);
      window.visualViewport?.removeEventListener("resize", updateSafeTop);
      window.visualViewport?.removeEventListener("scroll", updateSafeTop);
    };
  }, []);

  const profileUrl = profileUrlFor(user);

  // Deckモードでは各カラム（overflow:hidden・独自スタッキング）内に描画されるため、
  // body直下へポータルしてカラム枠に隠れないようにする。
  return createPortal(
    <div
      className="userPopupOverlay"
      role="presentation"
      style={{ "--user-popup-safe-top": `${safeTop}px` } as CSSProperties}
      onClick={onClose}
    >
      <div
        className="userPopupCard"
        role="dialog"
        aria-modal="true"
        aria-label={`${user.displayName}のプロフィール`}
        onClick={(event) => event.stopPropagation()}
      >
        <button type="button" className="userPopupClose" aria-label="閉じる" onClick={onClose}>
          ×
        </button>
        <div className="userPopupAvatar">
          <UserAvatar displayName={user.displayName} avatarUrl={user.avatarUrl} />
        </div>
        <strong className="userPopupName">{user.displayName}</strong>
        {user.handle ? <span className="userPopupHandle">@{user.handle}</span> : null}
        {user.ttsVoicevoxSpeaker ? (
          <a
            className="userPopupCv"
            href={user.ttsVoicevoxSpeaker.url}
            target="_blank"
            rel="noreferrer noopener"
          >
            CV: VOICEVOX:{user.ttsVoicevoxSpeaker.name}
          </a>
        ) : null}
        {profileUrl ? (
          <a
            className="buttonLink"
            href={profileUrl}
            target="_blank"
            rel="noreferrer noopener"
          >
            プロフィールを開く
          </a>
        ) : null}
      </div>
    </div>,
    document.body,
  );
}
