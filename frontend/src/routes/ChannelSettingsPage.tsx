import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";

import {
  clearChannelMessages,
  deleteChannel,
  getChannel,
  getMe,
  getPublicConfig,
  resolveAccessEntry,
  setChannelSuspendGrace,
  updateChannelSettings,
} from "../lib/api";
import { graceFromValue, graceOptions, graceValue } from "../lib/suspendOptions";
import type { AccessEntry, Channel, ChannelAccessMode } from "../types/chat";

// 各プロバイダの表示名（リスト項目のバッジ用）。
const PROVIDER_LABELS: Record<string, string> = {
  atproto: "Bluesky",
  mastodon: "Mastodon",
  misskey: "Misskey",
};

// entryKey はエントリの同一性判定キー（安定ID優先→ハンドル→URL→生入力）。
function entryKey(e: AccessEntry): string {
  if (e.provider && e.subject) return `subject:${e.provider.toLowerCase()}:${e.subject.toLowerCase()}`;
  if (e.handle) return `handle:${e.handle.toLowerCase()}`;
  if (e.profileUrl) return `url:${e.profileUrl.toLowerCase()}`;
  return `raw:${(e.raw ?? "").trim().toLowerCase()}`;
}

// entryLabel はリストに表示する主ラベル（表示名→ハンドル→生入力）。
function entryLabel(e: AccessEntry): string {
  return e.displayName || e.handle || e.raw || e.subject || "(不明)";
}

const ACCESS_MODE_LABELS: Record<ChannelAccessMode, string> = {
  none: "設定しない",
  whitelist: "ホワイトリスト",
  blacklist: "ブラックリスト",
};

const ACCESS_MODE_HINTS: Record<ChannelAccessMode, string> = {
  none: "誰でも入室できます。",
  whitelist: "リストに登録したユーザーだけが入室でき、それ以外にはチャンネルが見えません。",
  blacklist: "リストに登録したユーザーは入室できず、チャンネルも見えなくなります。",
};

type SettingsTab = "features" | "access" | "delete";

const SETTINGS_TABS: { id: SettingsTab; label: string }[] = [
  { id: "features", label: "機能" },
  { id: "access", label: "入室許可" },
  { id: "delete", label: "削除" },
];

type ChannelSettingsPanelProps = {
  slug: string;
  // モーダル埋め込み用。権限なし等で閉じる／削除後に閉じる。未指定＝フルページ遷移。
  onClose?: () => void;
  onDeleted?: () => void;
};

export function ChannelSettingsPanel({ slug, onClose, onDeleted }: ChannelSettingsPanelProps) {
  const navigate = useNavigate();
  const [channel, setChannel] = useState<Channel | null>(null);
  const [loading, setLoading] = useState(true);
  const [clearing, setClearing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [savingFeature, setSavingFeature] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  // タイトル・説明の編集ドラフト（保存ボタンで確定する）。
  const [titleDraft, setTitleDraft] = useState("");
  const [descDraft, setDescDraft] = useState("");
  const [savingProfile, setSavingProfile] = useState(false);

  // 入室許可制御の編集ドラフト（保存ボタンで確定する）。
  const [accessMode, setAccessMode] = useState<ChannelAccessMode>("none");
  // サーバ全体のホワイトリスト機能フラグ（デフォルトは無効）。無効時はホワイトリストの選択肢を出さない。
  const [whitelistEnabled, setWhitelistEnabled] = useState(false);
  const [accessEntries, setAccessEntries] = useState<AccessEntry[]>([]);
  const [newEntry, setNewEntry] = useState("");
  const [savingAccess, setSavingAccess] = useState(false);
  const [resolving, setResolving] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);

  const [tab, setTab] = useState<SettingsTab>("features");

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug]);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const me = await getMe();
      if (!me.user) {
        navigate("/login");
        return;
      }
      // サーバ全体のホワイトリスト機能フラグ。取得失敗時はデフォルト（無効）のまま。
      getPublicConfig()
        .then((config) => setWhitelistEnabled(!!config.whitelistEnabled))
        .catch(() => {});
      const res = await getChannel(slug);
      // チャンネルオーナーのみ設定可能
      if (res.channel.ownerUserId !== me.user.id) {
        if (onClose) {
          onClose();
        } else {
          navigate(`/channels/${slug}`);
        }
        return;
      }
      applyChannel(res.channel);
    } catch (err) {
      setError(err instanceof Error ? err.message : "チャンネルの読み込みに失敗しました");
    } finally {
      setLoading(false);
    }
  }

  function applyChannel(ch: Channel) {
    setChannel(ch);
    setAccessMode(ch.accessMode);
    setAccessEntries(ch.accessList ?? []);
    setTitleDraft(ch.title);
    setDescDraft(ch.description ?? "");
  }

  async function saveProfile() {
    const title = titleDraft.trim();
    if (!title) {
      setError("タイトルを入力してください。");
      return;
    }
    setSavingProfile(true);
    setError("");
    setNotice("");
    try {
      const res = await updateChannelSettings(slug, { title, description: descDraft.trim() });
      applyChannel(res.channel);
      setNotice("チャンネル情報を更新しました。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "設定の保存に失敗しました");
    } finally {
      setSavingProfile(false);
    }
  }

  async function changeFeature(settings: {
    urlLinkifyEnabled?: boolean;
    imageUploadEnabled?: boolean;
    noConsecutivePosts?: boolean;
    requireCommentForAttachment?: boolean;
    postTtlHours?: number;
  }) {
    setSavingFeature(true);
    setError("");
    setNotice("");
    try {
      const res = await updateChannelSettings(slug, settings);
      applyChannel(res.channel);
    } catch (err) {
      setError(err instanceof Error ? err.message : "設定の保存に失敗しました");
    } finally {
      setSavingFeature(false);
    }
  }

  // オーナー離席後の自動閉店（準備中への移行）までの猶予を変更する。
  async function changeGrace(value: string) {
    setSavingFeature(true);
    setError("");
    setNotice("");
    try {
      await setChannelSuspendGrace(slug, graceFromValue(value));
      const res = await getChannel(slug);
      applyChannel(res.channel);
      setNotice("オーナー離席後の自動閉店までの猶予を更新しました。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "設定の保存に失敗しました");
    } finally {
      setSavingFeature(false);
    }
  }

  // addEntry はサーバに問い合わせて入力を安定IDに解決し、リストへ追加する。
  async function addEntry() {
    const raw = newEntry.trim();
    if (!raw || resolving) {
      return;
    }
    setResolving(true);
    setError("");
    setNotice("");
    try {
      const res = await resolveAccessEntry(slug, raw);
      const resolved = res.entry;
      const key = entryKey(resolved);
      const exists = accessEntries.some((e) => entryKey(e) === key);
      if (exists) {
        setNotice("そのユーザーはすでに登録されています。");
      } else {
        setAccessEntries((current) => [...current, resolved]);
      }
      setNewEntry("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "ユーザーの解決に失敗しました");
    } finally {
      setResolving(false);
    }
  }

  function removeEntry(index: number) {
    setAccessEntries((current) => current.filter((_, i) => i !== index));
  }

  async function saveAccess() {
    setSavingAccess(true);
    setError("");
    setNotice("");
    try {
      const res = await updateChannelSettings(slug, {
        accessMode,
        accessList: accessEntries,
      });
      applyChannel(res.channel);
      setNotice("入室許可の設定を保存しました。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "入室許可の設定の保存に失敗しました");
    } finally {
      setSavingAccess(false);
    }
  }

  function exportAccessJson() {
    const payload = JSON.stringify({ mode: accessMode, entries: accessEntries }, null, 2);
    const blob = new Blob([payload], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `channel-access-${slug}.json`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  async function importAccessJson(file: File) {
    setError("");
    setNotice("");
    try {
      const text = await file.text();
      const data = JSON.parse(text) as unknown;
      const parsed = parseAccessImport(data);
      if (!parsed) {
        setError("JSONの形式が正しくありません（{ mode, entries } を含むJSONを指定してください）。");
        return;
      }
      setAccessMode(parsed.mode);
      setAccessEntries(parsed.entries);
      setNotice("JSONを読み込みました。内容を確認して「保存」してください。");
    } catch {
      setError("JSONの読み込みに失敗しました。");
    }
  }

  async function clearMessages() {
    if (!window.confirm("このチャンネルのチャットメッセージをすべて削除しますか？")) {
      return;
    }
    setClearing(true);
    setError("");
    setNotice("");
    try {
      await clearChannelMessages(slug);
      setNotice("チャットメッセージを一括削除しました。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "メッセージの削除に失敗しました");
    } finally {
      setClearing(false);
    }
  }

  async function removeChannel() {
    if (
      !window.confirm(
        `チャンネル「${channel?.title ?? slug}」を削除しますか？チャットメッセージや来訪者情報も含めて完全に削除され、元に戻せません。`,
      )
    ) {
      return;
    }
    setDeleting(true);
    setError("");
    setNotice("");
    try {
      await deleteChannel(slug);
      if (onDeleted) {
        onDeleted();
      } else {
        navigate("/");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "チャンネルの削除に失敗しました");
      setDeleting(false);
    }
  }

  const accessDirty =
    !!channel &&
    (accessMode !== channel.accessMode ||
      !sameEntries(accessEntries, channel.accessList ?? []));

  return (
    <>
      {loading ? <p>読み込み中...</p> : null}
      {channel ? (
        <>
          <p className="muted">
            {channel.title}（/{channel.slug}）
          </p>
          <div className="settingsLayout">
            <nav className="settingsNav" aria-label="設定の切り替え">
              {SETTINGS_TABS.map((item) => (
                <button
                  type="button"
                  key={item.id}
                  className={`settingsNavLink${tab === item.id ? " active" : ""}`}
                  aria-current={tab === item.id ? "page" : undefined}
                  onClick={() => setTab(item.id)}
                >
                  {item.label}
                </button>
              ))}
            </nav>
            <div className="settingsContent">
              {error ? <p className="formError">{error}</p> : null}
              {notice ? <p className="formNotice">{notice}</p> : null}

              {tab === "features" ? (
                <>
                <section className="settingsSection">
                  <h2>チャンネル情報</h2>
                  <div className="settingRows">
                    <label className="settingField settingFieldStacked">
                      タイトル
                      <input
                        type="text"
                        value={titleDraft}
                        maxLength={50}
                        disabled={savingProfile}
                        onChange={(event) => setTitleDraft(event.target.value)}
                      />
                    </label>
                    <label className="settingField settingFieldStacked">
                      説明
                      <textarea
                        value={descDraft}
                        maxLength={200}
                        rows={3}
                        disabled={savingProfile}
                        onChange={(event) => setDescDraft(event.target.value)}
                      />
                    </label>
                    <div>
                      <button
                        type="button"
                        className="buttonLink"
                        disabled={savingProfile || titleDraft.trim() === ""}
                        onClick={() => void saveProfile()}
                      >
                        {savingProfile ? "保存中..." : "保存"}
                      </button>
                    </div>
                  </div>
                </section>
                <section className="settingsSection">
                  <h2>機能</h2>
                  <div className="settingRows">
              <label className="settingToggle">
                <input
                  type="checkbox"
                  checked={channel.urlLinkifyEnabled}
                  disabled={savingFeature}
                  onChange={(event) =>
                    void changeFeature({ urlLinkifyEnabled: event.target.checked })
                  }
                />
                URLのリンク化
              </label>
              <p className="muted">投稿されたURLを自動でリンクとして表示します。</p>
              <label className="settingToggle">
                <input
                  type="checkbox"
                  checked={channel.imageUploadEnabled}
                  disabled={savingFeature}
                  onChange={(event) =>
                    void changeFeature({ imageUploadEnabled: event.target.checked })
                  }
                />
                画像アップロード
              </label>
              <p className="muted">チャット参加者が画像を投稿できるようにします。</p>
              <label className="settingToggle">
                <input
                  type="checkbox"
                  checked={channel.noConsecutivePosts}
                  disabled={savingFeature}
                  onChange={(event) =>
                    void changeFeature({ noConsecutivePosts: event.target.checked })
                  }
                />
                連続投稿の禁止
              </label>
              <p className="muted">
                書き込んだあと、自分以外の誰かが書き込むまで続けて書き込めないようにします。
              </p>
              {(() => {
                // 画像・URLのどちらも無効なら、この設定は意味を持たないのでグレーアウトする。
                const attachmentOff =
                  !channel.imageUploadEnabled && !channel.urlLinkifyEnabled;
                return (
                  <>
                    <label
                      className={`settingToggle${attachmentOff ? " settingToggleDisabled" : ""}`}
                    >
                      <input
                        type="checkbox"
                        checked={channel.requireCommentForAttachment}
                        disabled={savingFeature || attachmentOff}
                        onChange={(event) =>
                          void changeFeature({
                            requireCommentForAttachment: event.target.checked,
                          })
                        }
                      />
                      コメントなし添付の禁止
                    </label>
                    <p className={`muted${attachmentOff ? " settingHintDisabled" : ""}`}>
                      コメント（本文）なしで画像・URLだけを投稿できないようにします。
                    </p>
                  </>
                );
              })()}
              <label className="settingSelect">
                投稿の寿命
                <select
                  value={String(channel.postTtlHours ?? 24)}
                  disabled={savingFeature}
                  onChange={(event) =>
                    void changeFeature({ postTtlHours: Number(event.target.value) })
                  }
                >
                  <option value="6">6時間</option>
                  <option value="24">24時間</option>
                  <option value="72">3日</option>
                </select>
              </label>
              <p className="muted">
                この時間を過ぎた投稿は、画像も含めて自動的に削除されます。
              </p>
              <label className="settingSelect">
                オーナー離席後の自動閉店
                <select
                  value={graceValue(channel)}
                  disabled={savingFeature}
                  onChange={(event) => void changeGrace(event.target.value)}
                >
                  {graceOptions(channel).map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </label>
              <p className="muted">
                オーナーが離席すると、営業時間のカウントダウンを一時停止し、この時間で自動的に準備中へ移行します。
                「無期限」にすると離席しても自動閉店せず、通常の営業カウントダウンがそのまま進みます。
                （チャンネル寿命の無期限設定とは別です。）
              </p>
                  </div>
                </section>
                </>
              ) : null}

              {tab === "access" ? (
                <section className="settingsSection">
            <h2>入室許可</h2>
            <p className="muted">
              ホワイトリスト／ブラックリストで入室できるユーザーを制限します。入室対象外のユーザーは
              入室できず、チャンネル一覧にも表示されません。オーナーと管理者は常に入室できます。
            </p>
            <div className="accessModeChoices">
              {(["none", "whitelist", "blacklist"] as ChannelAccessMode[])
                // ホワイトリスト機能が無効なら選択肢から外す。ただし既にこのチャンネルが
                // ホワイトリストの場合は、無効化中である旨を示しつつ表示して別モードへ変更できるようにする。
                .filter((mode) => mode !== "whitelist" || whitelistEnabled || accessMode === "whitelist")
                .map((mode) => {
                  const whitelistDisabled = mode === "whitelist" && !whitelistEnabled;
                  return (
                    <label key={mode} className="accessModeChoice">
                      <input
                        type="radio"
                        name="accessMode"
                        value={mode}
                        checked={accessMode === mode}
                        disabled={whitelistDisabled}
                        onChange={() => setAccessMode(mode)}
                      />
                      <span className="accessModeChoiceLabel">
                        {ACCESS_MODE_LABELS[mode]}
                        {whitelistDisabled ? "（サーバ管理者により無効化中）" : ""}
                      </span>
                      <span className="accessModeChoiceHint">
                        {whitelistDisabled
                          ? "サーバ管理者によりホワイトリスト機能が無効化されているため、現在この制限は適用されていません。他のモードに変更できます。"
                          : ACCESS_MODE_HINTS[mode]}
                      </span>
                    </label>
                  );
                })}
            </div>

            {accessMode !== "none" ? (
              <div className="accessListEditor">
                <details className="accessHelp">
                  <summary>記述方法</summary>
                  <ul className="accessHelpList">
                    <li>
                      <b>Bluesky（ATProto）</b>: ハンドルを <code>@alice.bsky.social</code> の形式で入力。
                    </li>
                    <li>
                      <b>Mastodon / Misskey（Fediverse）</b>: ハンドルを <code>@user@instance.example</code> の形式で入力。
                    </li>
                    <li>
                      <b>プロフィールURL</b>: いずれのネットワークも、プロフィールページのURL（
                      <code>https://bsky.app/profile/alice.bsky.social</code> や
                      <code>https://instance.example/@user</code> など）をそのまま貼り付けても登録できます。
                    </li>
                    <li className="muted">
                      追加時にサーバが各SNSへ問い合わせ、安定した一意ID（Blueskyは DID
                      など）に変換して登録します。ハンドルを変更しても判定が維持されます。
                    </li>
                  </ul>
                </details>

                <div className="accessAddRow">
                  <input
                    type="text"
                    className="accessAddInput"
                    placeholder="@alice.bsky.social / @user@instance.example / https://..."
                    value={newEntry}
                    disabled={resolving}
                    onChange={(event) => setNewEntry(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") {
                        event.preventDefault();
                        void addEntry();
                      }
                    }}
                  />
                  <button
                    type="button"
                    className="buttonLink"
                    onClick={() => void addEntry()}
                    disabled={resolving || !newEntry.trim()}
                  >
                    {resolving ? "確認中..." : "追加"}
                  </button>
                </div>

                <p className="muted accessListCount">登録数: {accessEntries.length}件</p>
                {accessEntries.length === 0 ? (
                  <p className="muted">
                    {accessMode === "whitelist"
                      ? "まだ誰も登録されていません。この状態で保存するとオーナー以外は入室できません。"
                      : "まだ誰も登録されていません。"}
                  </p>
                ) : (
                  <ul className="accessList">
                    {accessEntries.map((entry, index) => (
                      <li className="accessListItem" key={`${entryKey(entry)}-${index}`}>
                        <div className="accessEntryInfo">
                          {entry.profileUrl ? (
                            <a
                              className="accessEntryName"
                              href={entry.profileUrl}
                              target="_blank"
                              rel="noreferrer noopener"
                            >
                              {entryLabel(entry)}
                            </a>
                          ) : (
                            <span className="accessEntryName">{entryLabel(entry)}</span>
                          )}
                          <span className="accessEntryMeta">
                            {entry.provider ? (
                              <span className="accessEntryBadge">
                                {PROVIDER_LABELS[entry.provider] ?? entry.provider}
                              </span>
                            ) : null}
                            {entry.handle ? (
                              <span className="accessEntryHandle">{entry.handle}</span>
                            ) : null}
                            {!entry.subject ? (
                              <span className="accessEntryUnresolved">未解決</span>
                            ) : null}
                          </span>
                        </div>
                        <button
                          type="button"
                          className="accessRemoveButton"
                          aria-label="削除"
                          onClick={() => removeEntry(index)}
                        >
                          ×
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            ) : null}

            <div className="accessActions">
              <button
                type="button"
                className="buttonLink"
                onClick={() => void saveAccess()}
                disabled={savingAccess || !accessDirty}
              >
                {savingAccess ? "保存中..." : accessDirty ? "保存" : "保存済み"}
              </button>
              <button type="button" className="buttonLink secondaryLink" onClick={exportAccessJson}>
                JSONエクスポート
              </button>
              <button
                type="button"
                className="buttonLink secondaryLink"
                onClick={() => importInputRef.current?.click()}
              >
                JSONインポート
              </button>
              <input
                ref={importInputRef}
                type="file"
                accept="application/json,.json"
                hidden
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (file) {
                    void importAccessJson(file);
                  }
                  event.target.value = "";
                }}
              />
            </div>
                </section>
              ) : null}

              {tab === "delete" ? (
                <>
                  <section className="settingsSection">
                    <h2>チャットメッセージの一括削除</h2>
                    <p className="muted">
                      このチャンネルのすべてのチャットメッセージを削除します。元に戻せません。
                    </p>
                    <button className="dangerButton" onClick={clearMessages} disabled={clearing}>
                      {clearing ? "削除中..." : "すべてのメッセージを削除"}
                    </button>
                  </section>
                  <section className="settingsSection">
                    <h2>チャンネルの削除</h2>
                    <p className="muted">
                      このチャンネルを削除します。チャットメッセージや来訪者情報も含めて完全に削除され、元に戻せません。
                    </p>
                    <button className="dangerButton" onClick={removeChannel} disabled={deleting}>
                      {deleting ? "削除中..." : "このチャンネルを削除"}
                    </button>
                  </section>
                </>
              ) : null}
            </div>
          </div>
        </>
      ) : null}
    </>
  );
}

// ルータ用の薄いラッパ。フルページのチャンネル設定（モバイル/直接アクセス）。
export default function ChannelSettingsPage() {
  const { slug = "" } = useParams();
  return (
    <section className="settingsPanel">
      <div className="sectionHeader">
        <h1>チャンネル設定</h1>
        <Link className="buttonLink secondaryLink" to={`/channels/${slug}`}>
          チャンネルへ戻る
        </Link>
      </div>
      <ChannelSettingsPanel slug={slug} />
    </section>
  );
}

// sameEntries は2つのエントリ配列が（順序込み・同一性キーで）同一かを判定する。
function sameEntries(a: AccessEntry[], b: AccessEntry[]): boolean {
  if (a.length !== b.length) {
    return false;
  }
  return a.every((v, i) => entryKey(v) === entryKey(b[i]));
}

// parseAccessImport はインポートJSONを検証し、mode/entries を取り出す。
// entries は構造化エントリ（オブジェクト）と旧形式の文字列の両方を受け付ける。
function parseAccessImport(
  data: unknown,
): { mode: ChannelAccessMode; entries: AccessEntry[] } | null {
  if (typeof data !== "object" || data === null) {
    return null;
  }
  const record = data as Record<string, unknown>;
  const rawMode = record.mode;
  const mode: ChannelAccessMode =
    rawMode === "whitelist" || rawMode === "blacklist" || rawMode === "none"
      ? rawMode
      : "none";
  if (!Array.isArray(record.entries)) {
    return null;
  }
  const entries: AccessEntry[] = [];
  const seen = new Set<string>();
  for (const item of record.entries) {
    let entry: AccessEntry | null = null;
    if (typeof item === "string") {
      const trimmed = item.trim();
      if (trimmed) {
        entry = { raw: trimmed };
      }
    } else if (typeof item === "object" && item !== null) {
      entry = item as AccessEntry;
    }
    if (!entry) {
      continue;
    }
    const key = entryKey(entry);
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    entries.push(entry);
  }
  return { mode, entries };
}
