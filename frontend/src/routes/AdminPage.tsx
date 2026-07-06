import { type ChangeEvent, FormEvent, useEffect, useState } from "react";
import { useNavigate } from "react-router";

import UserAvatar from "../components/UserAvatar";
import { getMe, listChannels } from "../lib/api";
import {
  createAdminChannel,
  deleteAdminChannel,
  deleteAdminUser,
  deleteServiceHeader,
  deleteTTSAutoDictionaryEntry,
  getAdminStats,
  getServiceSettings,
  importTTSAutoDictionaryEntries,
  listAdminSessions,
  listTTSAutoDictionaryEntries,
  listAdminUsers,
  revokeAdminSession,
  revokeAllAdminSessions,
  setChannelOperatingUnlimited,
  setChannelSuspendGrace,
  setChannelSuspendRetention,
  updateAdminUser,
  updateServiceOverview,
  updateWhitelistEnabled,
  uploadServiceHeader,
  upsertTTSAutoDictionaryEntry,
} from "../lib/adminApi";
import {
  graceFromValue,
  graceOptions,
  graceValue,
  retentionFromValue,
  retentionOptions,
  retentionValue,
} from "../lib/suspendOptions";
import type { AdminSession, AdminStats, AdminUser, Channel, TTSAutoDictionaryEntry } from "../types/chat";

type UserDraft = {
  displayName: string;
  handle: string;
  avatarUrl: string;
  status: string;
  role: string;
};

type AdminTab = "sessions" | "users" | "channels" | "dictionary" | "server";

const ADMIN_TABS: { id: AdminTab; label: string }[] = [
  { id: "sessions", label: "ブラウザセッション" },
  { id: "users", label: "ユーザー" },
  { id: "channels", label: "チャンネル" },
  { id: "dictionary", label: "辞書" },
  { id: "server", label: "サーバ設定" },
];

// サーバ概要の最大文字数（バックエンドの serverOverviewMaxRunes と合わせる）。
const SERVER_OVERVIEW_MAX = 4000;
// ヘッダ画像の推奨サイズ（横長）。アップロード画面に表示する。
const HEADER_RECOMMENDED = "推奨サイズ: 横1200×縦300px程度の横長画像（JPEG / PNG / GIF、5MBまで）";

export default function AdminPage() {
  const navigate = useNavigate();
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [sessions, setSessions] = useState<AdminSession[]>([]);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [dictionaryEntries, setDictionaryEntries] = useState<TTSAutoDictionaryEntry[]>([]);
  const [drafts, setDrafts] = useState<Record<string, UserDraft>>({});
  const [newChannel, setNewChannel] = useState({ slug: "", title: "", description: "" });
  const [newDictionaryEntry, setNewDictionaryEntry] = useState({ term: "", reading: "" });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  // 表示中のタブ（セッション/ユーザー/チャンネルをページ分けして見やすくする）。
  const [adminTab, setAdminTab] = useState<AdminTab>("sessions");
  // 各タブの文字列フィルタ。
  const [sessionFilter, setSessionFilter] = useState("");
  const [userFilter, setUserFilter] = useState("");
  const [channelFilter, setChannelFilter] = useState("");
  const [dictionaryFilter, setDictionaryFilter] = useState("");
  // サーバ設定（サービスヘッダ画像・サーバ概要）。
  const [serverOverview, setServerOverview] = useState("");
  const [serverHeaderUrl, setServerHeaderUrl] = useState("");
  const [whitelistEnabled, setWhitelistEnabled] = useState(false);

  useEffect(() => {
    void load();
  }, []);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const me = await getMe();
      if (!me.user) {
        navigate("/login");
        return;
      }
      if (!isPrivileged(me.user)) {
        // 権限がなければ管理アプリから離脱してメインサイトへ
        window.location.assign("/");
        return;
      }
      const [statsRes, sessionsRes, usersRes, channelsRes, dictionaryRes, serviceRes] =
        await Promise.all([
          getAdminStats(),
          listAdminSessions(),
          listAdminUsers(),
          listChannels(),
          listTTSAutoDictionaryEntries(),
          getServiceSettings(),
        ]);
      setStats(statsRes.stats);
      setSessions(sessionsRes.sessions);
      setUsers(usersRes.users);
      setChannels(channelsRes.channels);
      setDictionaryEntries(dictionaryRes.entries);
      setServerOverview(serviceRes.overview);
      setServerHeaderUrl(serviceRes.headerImageUrl);
      setWhitelistEnabled(serviceRes.whitelistEnabled);
      setDrafts(Object.fromEntries(usersRes.users.map((user) => [user.id, draftFromUser(user)])));
    } catch (err) {
      setError(err instanceof Error ? err.message : "管理ページの読み込みに失敗しました");
    } finally {
      setLoading(false);
    }
  }

  async function refresh() {
    setNotice("");
    await load();
  }

  async function saveUser(user: AdminUser) {
    const draft = drafts[user.id];
    if (!draft) {
      return;
    }
    await runAction(async () => {
      await updateAdminUser(user.id, draft);
      setNotice("ユーザーを更新しました。");
      await load();
    });
  }

  async function removeUser(user: AdminUser) {
    if (user.role === "owner") {
      setError("オーナーユーザーは削除できません。");
      return;
    }
    if (!window.confirm(`${user.displayName} を削除しますか？`)) {
      return;
    }
    await runAction(async () => {
      await deleteAdminUser(user.id);
      setNotice("ユーザーを削除しました。");
      await load();
    });
  }

  async function resetAllSessions() {
    if (
      !window.confirm(
        "すべてのブラウザセッションをリセットします。\n全ユーザー（あなた自身を含む）がログアウトされ、再ログインが必要になります。実行しますか？",
      )
    ) {
      return;
    }
    await runAction(async () => {
      await revokeAllAdminSessions();
      // 自分のセッションも失効したため、管理ログインへ戻す。
      navigate("/login");
    });
  }

  async function revokeSession(session: AdminSession) {
    if (!window.confirm(`${session.userDisplayName} のセッションを失効しますか？`)) {
      return;
    }
    await runAction(async () => {
      await revokeAdminSession(session.id);
      setNotice("セッションを失効しました。");
      await load();
    });
  }

  async function submitChannel(event: FormEvent) {
    event.preventDefault();
    await runAction(async () => {
      await createAdminChannel(newChannel);
      setNewChannel({ slug: "", title: "", description: "" });
      setNotice("チャンネルを作成しました。");
      await load();
    });
  }

  async function changeRetention(channel: Channel, value: string) {
    await runAction(async () => {
      await setChannelSuspendRetention(channel.slug, retentionFromValue(value));
      setNotice("閉店後の削除時間を更新しました。");
      await load();
    });
  }

  async function changeGrace(channel: Channel, value: string) {
    await runAction(async () => {
      await setChannelSuspendGrace(channel.slug, graceFromValue(value));
      setNotice("オーナー退出後の自動閉店までの猶予を更新しました。");
      await load();
    });
  }

  async function changeOperatingUnlimited(channel: Channel, checked: boolean) {
    await runAction(async () => {
      await setChannelOperatingUnlimited(channel.slug, checked);
      setNotice(checked ? "時間制限なしを有効にしました。" : "時間制限なしを無効にしました。");
      await load();
    });
  }

  async function removeChannel(channel: Channel) {
    if (!window.confirm(`/${channel.slug} を削除しますか？`)) {
      return;
    }
    await runAction(async () => {
      await deleteAdminChannel(channel.slug);
      setNotice("チャンネルを削除しました。");
      await load();
    });
  }

  async function submitDictionaryEntry(event: FormEvent) {
    event.preventDefault();
    await runAction(async () => {
      await upsertTTSAutoDictionaryEntry(newDictionaryEntry);
      setNewDictionaryEntry({ term: "", reading: "" });
      setNotice("辞書を登録しました。");
      await load();
    });
  }

  async function removeDictionaryEntry(entry: TTSAutoDictionaryEntry) {
    if (!window.confirm(`「${entry.term}」の辞書を削除しますか？`)) {
      return;
    }
    await runAction(async () => {
      await deleteTTSAutoDictionaryEntry(entry.termKey);
      setNotice("辞書を削除しました。");
      await load();
    });
  }

  function exportDictionaryEntries() {
    const payload = {
      version: 1,
      exportedAt: new Date().toISOString(),
      entries: dictionaryEntries.map((entry) => ({
        term: entry.term,
        reading: entry.reading,
      })),
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `tts-dictionary-${formatDateForFilename(new Date())}.json`;
    a.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  async function importDictionaryEntries(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }
    await runAction(async () => {
      const text = await file.text();
      const entries = parseDictionaryImport(text);
      if (entries.length === 0) {
        throw new Error("インポートできる辞書がありません。");
      }
      const res = await importTTSAutoDictionaryEntries(entries);
      setNotice(`${res.imported}件の辞書をインポートしました。`);
      await load();
    });
  }

  async function saveServerOverview() {
    await runAction(async () => {
      await updateServiceOverview(serverOverview);
      setNotice("サーバ概要を保存しました。");
    });
  }

  async function toggleWhitelistEnabled(enabled: boolean) {
    await runAction(async () => {
      const res = await updateWhitelistEnabled(enabled);
      setWhitelistEnabled(res.whitelistEnabled);
      setNotice(
        res.whitelistEnabled
          ? "ホワイトリスト機能を有効にしました。"
          : "ホワイトリスト機能を無効にしました。",
      );
    });
  }

  async function uploadHeader(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }
    await runAction(async () => {
      const res = await uploadServiceHeader(file);
      setServerHeaderUrl(res.headerImageUrl);
      setNotice("ヘッダ画像を更新しました。");
    });
  }

  async function removeHeader() {
    if (!window.confirm("サービスヘッダ画像を削除しますか？")) {
      return;
    }
    await runAction(async () => {
      await deleteServiceHeader();
      setServerHeaderUrl("");
      setNotice("ヘッダ画像を削除しました。");
    });
  }

  async function runAction(action: () => Promise<void>) {
    setError("");
    setNotice("");
    try {
      await action();
    } catch (err) {
      setError(err instanceof Error ? err.message : "操作に失敗しました");
    }
  }

  // 各リストを文字列フィルタで絞り込む（部分一致・大文字小文字無視）。
  const matches = (query: string, fields: (string | undefined | null)[]) => {
    const q = query.trim().toLowerCase();
    if (!q) {
      return true;
    }
    return fields.some((v) => !!v && v.toLowerCase().includes(q));
  };
  const filteredSessions = sessions.filter((session) =>
    matches(sessionFilter, [
      session.userDisplayName,
      session.userHandle,
      session.ipPrefix,
      session.userAgent,
    ]),
  );
  const filteredUsers = users.filter((user) =>
    matches(userFilter, [
      user.displayName,
      user.handle,
      user.provider,
      user.role,
      user.status,
    ]),
  );
  const filteredChannels = channels.filter((channel) =>
    matches(channelFilter, [channel.title, channel.slug, channel.description]),
  );
  const filteredDictionaryEntries = dictionaryEntries.filter((entry) =>
    matches(dictionaryFilter, [
      entry.term,
      entry.reading,
      entry.registeredByHandle,
      entry.registeredByUserId,
    ]),
  );

  return (
    <section className="adminPage">
      <div className="sectionHeader">
        <div>
          <h1>管理</h1>
          <p>{loading ? "読み込み中..." : "システム管理"}</p>
        </div>
        <button onClick={refresh} disabled={loading}>
          更新
        </button>
      </div>

      {error ? <p className="formError">{error}</p> : null}
      {notice ? <p className="formNotice">{notice}</p> : null}

      <div className="statsGrid">
        <Stat label="ユーザー" value={stats?.usersCount ?? 0} />
        <Stat label="チャンネル" value={stats?.channelsCount ?? 0} />
        <Stat label="メッセージ" value={stats?.chatMessagesCount ?? 0} />
        <Stat label="セッション" value={stats?.activeSessionsCount ?? 0} />
      </div>

      <nav className="adminTabs" aria-label="管理セクションの切り替え">
        {ADMIN_TABS.map((item) => (
          <button
            type="button"
            key={item.id}
            className={`adminTabLink${adminTab === item.id ? " active" : ""}`}
            aria-current={adminTab === item.id ? "page" : undefined}
            onClick={() => setAdminTab(item.id)}
          >
            {item.label}
          </button>
        ))}
      </nav>

      {adminTab === "sessions" ? (
      <section className="adminSection">
        <div className="adminSectionHeader">
          <h2>ブラウザセッション</h2>
          <input
            type="search"
            className="adminFilterInput"
            value={sessionFilter}
            onChange={(event) => setSessionFilter(event.target.value)}
            placeholder="表示名・ハンドル・IP・User-Agentで絞り込み"
            aria-label="セッションのフィルタ"
          />
          <button type="button" className="dangerButton" onClick={resetAllSessions}>
            すべてリセット
          </button>
        </div>
        <p className="muted">
          {sessionFilter.trim()
            ? `${filteredSessions.length} / ${sessions.length} 件`
            : `${sessions.length} 件`}
        </p>
        <div className="tableWrap">
          <table>
            <thead>
              <tr>
                <th>ユーザー</th>
                <th>最終確認</th>
                <th>期限</th>
                <th>IP</th>
                <th>User-Agent</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filteredSessions.map((session) => (
                <tr key={session.id}>
                  <td>
                    <strong>{session.userDisplayName}</strong>
                    {session.userHandle ? <span className="muted"> @{session.userHandle}</span> : null}
                  </td>
                  <td>{formatDate(session.lastSeenAt || session.createdAt)}</td>
                  <td>{formatDate(session.expiresAt)}</td>
                  <td>{session.ipPrefix || "-"}</td>
                  <td className="userAgentCell">{session.userAgent || "-"}</td>
                  <td>
                    <button className="dangerButton" onClick={() => revokeSession(session)}>
                      失効
                    </button>
                  </td>
                </tr>
              ))}
              {filteredSessions.length === 0 ? (
                <tr>
                  <td colSpan={6}>
                    {sessions.length === 0
                      ? "有効なセッションはありません。"
                      : "条件に一致するセッションはありません。"}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
      ) : null}

      {adminTab === "users" ? (
      <section className="adminSection">
        <div className="adminSectionHeader">
          <h2>ユーザー</h2>
          <input
            type="search"
            className="adminFilterInput"
            value={userFilter}
            onChange={(event) => setUserFilter(event.target.value)}
            placeholder="表示名・ハンドル・プロバイダーで絞り込み"
            aria-label="ユーザーのフィルタ"
          />
        </div>
        <p className="muted">
          {userFilter.trim() ? `${filteredUsers.length} / ${users.length} 件` : `${users.length} 件`}
        </p>
        <div className="tableWrap">
          <table>
            <thead>
              <tr>
                <th>ユーザー</th>
                <th>状態</th>
                <th>権限</th>
                <th>プロバイダー</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filteredUsers.map((user) => {
                const draft = drafts[user.id] ?? draftFromUser(user);
                return (
                  <tr key={user.id}>
                    <td>
                      <div className="adminUserCell">
                        <span className="adminUserAvatar">
                          <UserAvatar
                            displayName={user.displayName}
                            avatarUrl={user.avatarUrl}
                          />
                        </span>
                        <div className="adminUserMeta">
                          <strong>{user.displayName}</strong>
                          {user.handle ? (
                            <span className="muted">@{user.handle}</span>
                          ) : null}
                        </div>
                      </div>
                    </td>
                    <td>
                      <select
                        value={draft.status}
                        onChange={(event) => updateDraft(user.id, "status", event.target.value)}
                        disabled={user.role === "owner"}
                      >
                        <option value="active">有効</option>
                        <option value="suspended">停止</option>
                      </select>
                    </td>
                    <td>
                      <select
                        value={draft.role}
                        onChange={(event) => updateDraft(user.id, "role", event.target.value)}
                        disabled={user.role === "owner"}
                      >
                        <option value="user">ユーザー</option>
                        <option value="admin">管理者</option>
                        <option value="owner" disabled={user.role !== "owner"}>
                          オーナー
                        </option>
                      </select>
                    </td>
                    <td>
                      <span>{user.provider || "-"}</span>
                    </td>
                    <td className="actionCell">
                      <button onClick={() => saveUser(user)}>保存</button>
                      <button
                        className="dangerButton"
                        onClick={() => removeUser(user)}
                        disabled={user.role === "owner"}
                      >
                        削除
                      </button>
                    </td>
                  </tr>
                );
              })}
              {filteredUsers.length === 0 ? (
                <tr>
                  <td colSpan={5}>条件に一致するユーザーはいません。</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
      ) : null}

      {adminTab === "channels" ? (
      <section className="adminSection">
        <div className="adminSectionHeader">
          <h2>チャンネル</h2>
          <input
            type="search"
            className="adminFilterInput"
            value={channelFilter}
            onChange={(event) => setChannelFilter(event.target.value)}
            placeholder="タイトル・slug・説明で絞り込み"
            aria-label="チャンネルのフィルタ"
          />
        </div>
        <p className="muted">
          {channelFilter.trim()
            ? `${filteredChannels.length} / ${channels.length} 件`
            : `${channels.length} 件`}
        </p>
        <form className="inlineForm" onSubmit={submitChannel}>
          <input
            value={newChannel.slug}
            onChange={(event) => setNewChannel({ ...newChannel, slug: event.target.value })}
            placeholder="channel-slug"
            required
          />
          <input
            value={newChannel.title}
            onChange={(event) => setNewChannel({ ...newChannel, title: event.target.value })}
            placeholder="チャンネル名"
            required
          />
          <input
            value={newChannel.description}
            onChange={(event) => setNewChannel({ ...newChannel, description: event.target.value })}
            placeholder="説明"
          />
          <button type="submit">チャンネル作成</button>
        </form>
        <div className="roomAdminList">
          {filteredChannels.map((channel) => (
            <div className="roomAdminItem" key={channel.id}>
              <div>
                <strong>
                  {channel.title}
                  {channel.suspended ? <span className="suspendBadge">準備中</span> : null}
                </strong>
                <span>/{channel.slug}</span>
                {channel.description ? <p>{channel.description}</p> : null}
                <label className="retentionControl">
                  オーナー退出後の自動閉店
                  <select
                    value={graceValue(channel)}
                    onChange={(event) => changeGrace(channel, event.target.value)}
                  >
                    {graceOptions(channel).map((opt) => (
                      <option value={opt.value} key={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="retentionControl">
                  閉店後の削除
                  <select
                    value={retentionValue(channel)}
                    onChange={(event) => changeRetention(channel, event.target.value)}
                  >
                    {retentionOptions(channel).map((opt) => (
                      <option value={opt.value} key={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="retentionControl">
                  <input
                    type="checkbox"
                    checked={channel.operatingUnlimited ?? false}
                    onChange={(event) =>
                      void changeOperatingUnlimited(channel, event.target.checked)
                    }
                  />
                  時間制限なし
                </label>
              </div>
              <div className="actionCell">
                <a className="buttonLink secondaryLink" href={`/channels/${channel.slug}`}>
                  開く
                </a>
                <button className="dangerButton" onClick={() => removeChannel(channel)}>
                  削除
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>
      ) : null}

      {adminTab === "dictionary" ? (
      <section className="adminSection">
        <div className="adminSectionHeader">
          <h2>辞書登録管理</h2>
          <input
            type="search"
            className="adminFilterInput"
            value={dictionaryFilter}
            onChange={(event) => setDictionaryFilter(event.target.value)}
            placeholder="単語・読み・登録者で絞り込み"
            aria-label="辞書のフィルタ"
          />
        </div>
        <p className="muted">
          {dictionaryFilter.trim()
            ? `${filteredDictionaryEntries.length} / ${dictionaryEntries.length} 件`
            : `${dictionaryEntries.length} 件`}
        </p>
        <form className="inlineForm dictionaryInlineForm" onSubmit={submitDictionaryEntry}>
          <input
            value={newDictionaryEntry.term}
            onChange={(event) =>
              setNewDictionaryEntry({ ...newDictionaryEntry, term: event.target.value })
            }
            placeholder="登録語（例: Claude）"
            maxLength={64}
            required
          />
          <input
            value={newDictionaryEntry.reading}
            onChange={(event) =>
              setNewDictionaryEntry({ ...newDictionaryEntry, reading: event.target.value })
            }
            placeholder="読み（例: クロード）"
            maxLength={64}
            required
          />
          <button type="submit">追加</button>
          <button type="button" className="secondaryLink" onClick={exportDictionaryEntries}>
            JSONエクスポート
          </button>
          <label className="buttonLink secondaryLink importButtonLabel">
            JSONインポート
            <input
              type="file"
              accept="application/json,.json"
              onChange={(event) => void importDictionaryEntries(event)}
            />
          </label>
        </form>
        <div className="tableWrap">
          <table>
            <thead>
              <tr>
                <th>登録語</th>
                <th>読み</th>
                <th>登録者</th>
                <th>登録日時</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filteredDictionaryEntries.map((entry) => (
                <tr key={entry.termKey}>
                  <td>
                    <strong>{entry.term}</strong>
                  </td>
                  <td>{entry.reading}</td>
                  <td>{entry.registeredByHandle ? `@${entry.registeredByHandle}` : "-"}</td>
                  <td>{formatDate(entry.registeredAt)}</td>
                  <td className="actionCell">
                    <button className="dangerButton" onClick={() => removeDictionaryEntry(entry)}>
                      削除
                    </button>
                  </td>
                </tr>
              ))}
              {filteredDictionaryEntries.length === 0 ? (
                <tr>
                  <td colSpan={5}>
                    {dictionaryEntries.length === 0
                      ? "登録済みの辞書はありません。"
                      : "条件に一致する辞書はありません。"}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
      ) : null}

      {adminTab === "server" ? (
      <section className="adminSection">
        <div className="adminSectionHeader">
          <h2>サーバ設定</h2>
        </div>

        <div className="serverSettingGroup">
          <h3>ホワイトリスト機能</h3>
          <p className="muted">
            チャンネルの「入室許可」設定でホワイトリスト（登録ユーザーだけが入室可）を使えるようにします。
            無効にすると、各チャンネルでホワイトリストを選べなくなり、既にホワイトリストを設定済みの
            チャンネルも制限が適用されません（設定値は保持され、再度有効にすると復活します）。ブラックリストには影響しません。
          </p>
          <label className="whitelistToggle">
            <input
              type="checkbox"
              checked={whitelistEnabled}
              onChange={(event) => void toggleWhitelistEnabled(event.target.checked)}
            />
            <span>ホワイトリスト機能を有効にする</span>
          </label>
          <p className="muted">現在: {whitelistEnabled ? "有効" : "無効（デフォルト）"}</p>
        </div>

        <div className="serverSettingGroup">
          <h3>サービスヘッダ画像</h3>
          <p className="muted">{HEADER_RECOMMENDED}</p>
          {serverHeaderUrl ? (
            <img className="serverHeaderPreview" src={serverHeaderUrl} alt="サービスヘッダ画像" />
          ) : (
            <p className="muted">現在は画像が設定されていません（デフォルト: 画像なし）。</p>
          )}
          <div className="actionCell">
            <label className="buttonLink secondaryLink importButtonLabel">
              {serverHeaderUrl ? "画像を差し替え" : "画像をアップロード"}
              <input
                type="file"
                accept="image/jpeg,image/png,image/gif"
                onChange={(event) => void uploadHeader(event)}
              />
            </label>
            {serverHeaderUrl ? (
              <button type="button" className="dangerButton" onClick={removeHeader}>
                画像を削除
              </button>
            ) : null}
          </div>
        </div>

        <div className="serverSettingGroup">
          <h3>サーバ概要</h3>
          <p className="muted">
            サービス情報ページに表示されます。Markdownで記述できます（{SERVER_OVERVIEW_MAX}文字まで）。
          </p>
          <textarea
            className="serverOverviewInput"
            value={serverOverview}
            onChange={(event) => setServerOverview(event.target.value.slice(0, SERVER_OVERVIEW_MAX))}
            rows={12}
            placeholder="# ようこそ&#10;&#10;このサーバーについての説明をMarkdownで記述します。"
          />
          <div className="serverOverviewFooter">
            <span className="muted">
              {serverOverview.length} / {SERVER_OVERVIEW_MAX} 文字
            </span>
            <button type="button" onClick={saveServerOverview}>
              サーバ概要を保存
            </button>
          </div>
        </div>
      </section>
      ) : null}
    </section>
  );

  function updateDraft(userId: string, key: keyof UserDraft, value: string) {
    setDrafts((current) => ({
      ...current,
      [userId]: {
        ...(current[userId] ?? draftFromUser(users.find((user) => user.id === userId)!)),
        [key]: value,
      },
    }));
  }
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="statBox">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function draftFromUser(user: AdminUser): UserDraft {
  return {
    displayName: user.displayName,
    handle: user.handle ?? "",
    avatarUrl: user.avatarUrl ?? "",
    status: user.status || "active",
    role: user.role || "user",
  };
}

function isPrivileged(user: { role?: string }) {
  return user.role === "owner" || user.role === "admin";
}

function formatDate(value: string) {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "short",
  }).format(new Date(value));
}

function formatDateForFilename(value: Date) {
  const yyyy = value.getFullYear();
  const mm = String(value.getMonth() + 1).padStart(2, "0");
  const dd = String(value.getDate()).padStart(2, "0");
  const hh = String(value.getHours()).padStart(2, "0");
  const min = String(value.getMinutes()).padStart(2, "0");
  return `${yyyy}${mm}${dd}-${hh}${min}`;
}

function parseDictionaryImport(text: string): Pick<TTSAutoDictionaryEntry, "term" | "reading">[] {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    throw new Error("JSONを読み取れませんでした。");
  }
  const entries = Array.isArray(raw)
    ? raw
    : raw && typeof raw === "object" && Array.isArray((raw as { entries?: unknown }).entries)
      ? (raw as { entries: unknown[] }).entries
      : null;
  if (!entries) {
    throw new Error("辞書JSONは entries 配列、または配列そのものを指定してください。");
  }
  return entries.map((entry, index) => {
    if (!entry || typeof entry !== "object") {
      throw new Error(`${index + 1}件目の形式が不正です。`);
    }
    const { term, reading } = entry as { term?: unknown; reading?: unknown };
    if (typeof term !== "string" || typeof reading !== "string") {
      throw new Error(`${index + 1}件目に term と reading が必要です。`);
    }
    return { term, reading };
  });
}
