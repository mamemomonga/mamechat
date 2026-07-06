import { useState } from "react";

import type { PresenceMember } from "../types/chat";
import UserAvatar from "./UserAvatar";
import UserProfilePopup, { type PopupUser } from "./UserProfilePopup";

type Props = {
  owner?: PresenceMember;
  members?: PresenceMember[];
  activeCount?: number;
  totalCount?: number;
};

export default function ChannelPresence({
  owner,
  members = [],
  activeCount = 0,
  totalCount = 0,
}: Props) {
  const [popupUser, setPopupUser] = useState<PopupUser | null>(null);

  function avatarButton(member: PresenceMember, key: string, extraClass = "") {
    return (
      <button
        type="button"
        className={`presenceMember ${member.active ? "active" : "inactive"}${extraClass}`}
        key={key}
        title={`${member.user.displayName}${member.active ? " / アクティブ" : " / 非アクティブ"}`}
        onClick={() => setPopupUser(member.user)}
      >
        <UserAvatar displayName={member.user.displayName} avatarUrl={member.user.avatarUrl} />
      </button>
    );
  }

  return (
    <div className="channelPresence" aria-label="チャンネルメンバー">
      {owner ? avatarButton(owner, "owner", " owner") : null}
      <span className="presenceDivider" aria-hidden="true" />
      <span className="activeCount" title="アクティブ数 / のべ人数">
        {activeCount}/{totalCount}
      </span>
      <div className="presenceAvatars">
        {members.map((member) => avatarButton(member, member.user.id))}
      </div>
      {popupUser ? <UserProfilePopup user={popupUser} onClose={() => setPopupUser(null)} /> : null}
    </div>
  );
}
