type Props = {
  displayName: string;
  avatarUrl?: string;
};

export default function UserAvatar({ displayName, avatarUrl }: Props) {
  const initial = (displayName.trim()[0] ?? "?").toUpperCase();
  if (avatarUrl) {
    return <img className="avatar" src={avatarUrl} alt="" referrerPolicy="no-referrer" />;
  }
  return <div className="avatar avatarFallback">{initial}</div>;
}
