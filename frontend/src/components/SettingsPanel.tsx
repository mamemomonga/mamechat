import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";

import {
  getMe,
  listVoicevoxSpeakers,
  previewVoicevoxSpeaker,
  sendTestPush,
  setGhostMode,
  updateVoicevoxSpeaker,
} from "../lib/api";
import {
  disablePush,
  enablePush,
  getPushConfig,
  isPushEnabled,
  isPushEnvironmentSupported,
} from "../lib/pushNotify";
import type { TTSVoicevoxSpeaker, User } from "../types/chat";

// 自動再生をユーザー操作の延長として解錠するための無音WAV。
const SILENT_AUDIO_URL =
  "data:audio/wav;base64,UklGRigAAABXQVZFZm10IBAAAAABAAEAESsAACJWAAACABAAZGF0YQQAAAAAAA==";

// SettingsPanel はユーザー設定の中身。ルートページ・モーダルの両方で使う。
export default function SettingsPanel() {
  const navigate = useNavigate();
  const [user, setUser] = useState<User | null>(null);
  const [speakers, setSpeakers] = useState<TTSVoicevoxSpeaker[]>([]);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [savingGhost, setSavingGhost] = useState(false);
  const [loading, setLoading] = useState(true);
  // Push通知: 対応環境か / 現在有効か / 操作中か。
  const [pushSupported, setPushSupported] = useState(false);
  const [pushOn, setPushOn] = useState(false);
  const [pushBusy, setPushBusy] = useState(false);
  const [pushTestNote, setPushTestNote] = useState("");
  const previewAudioRef = useRef<HTMLAudioElement | null>(null);

  useEffect(() => {
    void load();
  }, []);

  // Service Worker からの push 受信通知を拾い、テスト結果に反映する（表示側の問題切り分け用）。
  useEffect(() => {
    if (!("serviceWorker" in navigator)) {
      return;
    }
    function onMessage(event: MessageEvent) {
      const data = event.data as { type?: string; title?: string; message?: string } | undefined;
      if (data?.type === "push-received") {
        setPushTestNote(
          `✅ この端末でPushを受信しました（Service Workerは動作しています／"${data.title ?? ""}"）。` +
            "それでもOSに通知が出ない場合は、システムの通知設定や集中モード、フォアグラウンド抑制が原因です。",
        );
      } else if (data?.type === "push-error") {
        setPushTestNote(`⚠️ SWは受信しましたが通知表示でエラー: ${data.message ?? ""}`);
      }
    }
    navigator.serviceWorker.addEventListener("message", onMessage);
    return () => navigator.serviceWorker.removeEventListener("message", onMessage);
  }, []);

  async function load() {
    setLoading(true);
    try {
      const res = await getMe();
      if (!res.user) {
        navigate("/login");
        return;
      }
      const speakersRes = await listVoicevoxSpeakers();
      setUser(res.user);
      setSpeakers(speakersRes.speakers);
      // Push通知が使える環境（VAPID設定済み）かを判定し、現在の有効状態を反映する。
      if (isPushEnvironmentSupported() && (await getPushConfig()).enabled) {
        setPushSupported(true);
        setPushOn(await isPushEnabled());
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "設定の読み込みに失敗しました");
    } finally {
      setLoading(false);
    }
  }

  function getPreviewAudio() {
    if (!previewAudioRef.current) {
      const audio = new Audio();
      audio.preload = "auto";
      previewAudioRef.current = audio;
    }
    return previewAudioRef.current;
  }

  // ブラウザの自動再生制限を避けるため、ユーザー操作と同じタスク内で無音を再生して解錠する。
  function unlockPreviewAudio() {
    const audio = getPreviewAudio();
    audio.muted = true;
    audio.src = SILENT_AUDIO_URL;
    void audio
      .play()
      .then(() => {
        audio.pause();
        audio.muted = false;
      })
      .catch(() => {
        audio.muted = false;
      });
  }

  async function playSpeakerPreview(speakerUuid: string) {
    setPreviewing(true);
    try {
      const res = await previewVoicevoxSpeaker(speakerUuid);
      const audio = getPreviewAudio();
      audio.muted = false;
      audio.src = res.audioUrl;
      audio.load();
      await audio.play();
    } catch {
      // 試聴の失敗は設定操作の妨げにしない（読み上げ無効時など）。
    } finally {
      setPreviewing(false);
    }
  }

  async function changeSpeaker(speakerUuid: string) {
    if (speakerUuid !== "") {
      unlockPreviewAudio();
    }
    setSaving(true);
    setError("");
    try {
      const res = await updateVoicevoxSpeaker(speakerUuid);
      setUser((current) =>
        current ? { ...current, ttsVoicevoxSpeaker: res.speaker } : current,
      );
      if (res.speaker) {
        void playSpeakerPreview(res.speaker.uuid);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "キャラクター設定の保存に失敗しました");
    } finally {
      setSaving(false);
    }
  }

  function replaySelectedSpeaker() {
    if (!selectedSpeaker) {
      return;
    }
    unlockPreviewAudio();
    void playSpeakerPreview(selectedSpeaker.uuid);
  }

  async function changeGhostMode(enabled: boolean) {
    setSavingGhost(true);
    setError("");
    try {
      const res = await setGhostMode(enabled);
      setUser((current) => (current ? { ...current, ghostMode: res.ghostMode } : current));
      // ヘッダーのおばけアイコン表示を更新する。
      window.dispatchEvent(new Event("auth-changed"));
    } catch (err) {
      setError(err instanceof Error ? err.message : "ゴーストモードの設定に失敗しました");
    } finally {
      setSavingGhost(false);
    }
  }

  async function togglePush() {
    setPushBusy(true);
    setError("");
    try {
      if (pushOn) {
        await disablePush();
        setPushOn(false);
      } else {
        await enablePush();
        setPushOn(true);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "通知設定の変更に失敗しました");
    } finally {
      setPushBusy(false);
    }
  }

  async function testPush() {
    setPushBusy(true);
    setPushTestNote("");
    setError("");
    try {
      const res = await sendTestPush();
      setPushTestNote(
        `送信しました（成功 ${res.sent} / 失効 ${res.gone} / 失敗 ${res.failed}）。` +
          "アプリを閉じるか画面をロックして届くか確認してください。",
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "テスト通知の送信に失敗しました");
    } finally {
      setPushBusy(false);
    }
  }

  const selectedSpeaker = user?.ttsVoicevoxSpeaker;

  return (
    <>
      {loading ? <p>読み込み中...</p> : null}
      {error ? <p className="formError">{error}</p> : null}
      {user ? (
        <>
          <section className="settingsSection">
            <h2>アカウント情報</h2>
            <dl className="accountInfo">
              <dt>表示名</dt>
              <dd>{user.displayName}</dd>
              <dt>ハンドル (ID)</dt>
              <dd>{user.handle || "-"}</dd>
              <dt>プロフィール</dt>
              <dd>
                {user.profileUrl ? (
                  <a href={user.profileUrl} target="_blank" rel="noreferrer noopener">
                    プロフィールページを開く
                  </a>
                ) : (
                  "-"
                )}
              </dd>
            </dl>
          </section>
          <section className="settingsSection">
            <h2>自分のポストの読み上げ</h2>
            <div className="settingRows">
              <label className="settingField">
                キャラクター
                <select
                  value={selectedSpeaker?.uuid ?? ""}
                  onChange={(event) => void changeSpeaker(event.target.value)}
                  disabled={saving}
                >
                  <option value="">使用しない</option>
                  {speakers.map((speaker) => (
                    <option key={speaker.uuid} value={speaker.uuid}>
                      {speaker.name}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="buttonLink secondaryLink"
                  onClick={replaySelectedSpeaker}
                  disabled={!selectedSpeaker || saving || previewing}
                >
                  {previewing ? "再生中..." : "試聴"}
                </button>
              </label>
              {selectedSpeaker ? (
                <a
                  className="voicevoxCredit"
                  href={selectedSpeaker.url}
                  target="_blank"
                  rel="noreferrer noopener"
                >
                  VOICEVOX:{selectedSpeaker.name}
                </a>
              ) : null}
            </div>
          </section>
          {pushSupported ? (
            <section className="settingsSection">
              <h2>通知</h2>
              <div className="settingRows">
                <button
                  type="button"
                  className="buttonLink"
                  onClick={() => void togglePush()}
                  disabled={pushBusy}
                >
                  {pushBusy
                    ? "変更中..."
                    : pushOn
                      ? "Push通知を無効にする"
                      : "Push通知を有効にする"}
                </button>
                <p className="muted">
                  有効にすると、各チャンネルの「通知」ボタンをオンにしたチャンネルが営業中に
                  なったときにお知らせが届きます。
                </p>
                {pushOn ? (
                  <>
                    <button
                      type="button"
                      className="buttonLink secondaryLink"
                      onClick={() => void testPush()}
                      disabled={pushBusy}
                    >
                      テスト通知を送信
                    </button>
                    {pushTestNote ? <p className="muted">{pushTestNote}</p> : null}
                  </>
                ) : null}
              </div>
            </section>
          ) : null}
          {user.canManage ? (
            <section className="settingsSection">
              <h2>管理</h2>
              <div className="settingRows">
                <label className="settingToggle">
                  <input
                    type="checkbox"
                    checked={!!user.ghostMode}
                    disabled={savingGhost}
                    onChange={(event) => void changeGhostMode(event.target.checked)}
                  />
                  ゴーストモード
                </label>
                <p className="muted">
                  有効にすると、ホワイトリスト・ブラックリストに関係なくどのチャンネルにも入室できますが、
                  書き込みはできません。有効中はアバターの横に 👻 が表示されます。
                </p>
              </div>
            </section>
          ) : null}
        </>
      ) : null}
    </>
  );
}
