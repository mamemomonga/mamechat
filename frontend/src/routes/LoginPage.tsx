import { FormEvent, useState } from "react";

import { startAtprotoLogin, startMastodonLogin, startMisskeyLogin } from "../lib/api";

type Provider = "atproto" | "mastodon" | "misskey";

export default function LoginPage() {
  const [provider, setProvider] = useState<Provider | null>(null);
  const [atprotoIdentifier, setAtprotoIdentifier] = useState("");
  const [mastodonInstance, setMastodonInstance] = useState("");
  const [misskeyInstance, setMisskeyInstance] = useState("");
  const [error, setError] = useState("");
  const [atprotoLoading, setAtprotoLoading] = useState(false);
  const [mastodonLoading, setMastodonLoading] = useState(false);
  const [misskeyLoading, setMisskeyLoading] = useState(false);

  function selectProvider(next: Provider) {
    setError("");
    setProvider(next);
  }

  function backToSelect() {
    setError("");
    setProvider(null);
  }

  async function submitAtproto(event: FormEvent) {
    event.preventDefault();
    setAtprotoLoading(true);
    setError("");
    try {
      const res = await startAtprotoLogin({ identifier: atprotoIdentifier });
      window.location.href = res.authorizationUrl;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Blueskyログインを開始できませんでした");
      setAtprotoLoading(false);
    }
  }

  async function submitMastodon(event: FormEvent) {
    event.preventDefault();
    setMastodonLoading(true);
    setError("");
    try {
      const res = await startMastodonLogin({ instanceUrl: mastodonInstance });
      window.location.href = res.authorizationUrl;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Mastodonログインを開始できませんでした");
      setMastodonLoading(false);
    }
  }

  async function submitMisskey(event: FormEvent) {
    event.preventDefault();
    setMisskeyLoading(true);
    setError("");
    try {
      const res = await startMisskeyLogin({ instanceUrl: misskeyInstance });
      window.location.href = res.authorizationUrl;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Misskeyログインを開始できませんでした");
      setMisskeyLoading(false);
    }
  }

  const oauthError = new URLSearchParams(window.location.search).get("error");

  return (
    <section className="narrowPanel">
      <h1>ログイン</h1>
      {oauthError ? (
        <p className="formError">外部ログインに失敗しました。もう一度試してください。</p>
      ) : null}
      {error ? <p className="formError">{error}</p> : null}

      {provider === null ? (
        <div className="loginSelect">
          <p>どのSNSアカウントでログインしますか？</p>
          <div className="loginSelectButtons">
            <button type="button" onClick={() => selectProvider("atproto")}>
              Bluesky
            </button>
            <button type="button" onClick={() => selectProvider("mastodon")}>
              Mastodon
            </button>
            <button type="button" onClick={() => selectProvider("misskey")}>
              Misskey
            </button>
          </div>
        </div>
      ) : null}

      {provider === "atproto" ? (
        <form className="stackForm" onSubmit={submitAtproto}>
          <button type="button" className="loginBackButton" onClick={backToSelect}>
            ‹ 選択に戻る
          </button>
          <h2>Blueskyでログイン</h2>
          <label>
            ハンドルまたはDID
            <input
              value={atprotoIdentifier}
              onChange={(event) => setAtprotoIdentifier(event.target.value)}
              autoComplete="username"
              placeholder="alice.bsky.social"
              required
            />
            <p className="commentText">.bsky.social は省略できます。</p>
          </label>
          <button type="submit" disabled={atprotoLoading}>
            {atprotoLoading ? "認証ページへ移動中..." : "Blueskyでログイン"}
          </button>
        </form>
      ) : null}

      {provider === "mastodon" ? (
        <form className="stackForm" onSubmit={submitMastodon}>
          <button type="button" className="loginBackButton" onClick={backToSelect}>
            ‹ 選択に戻る
          </button>
          <h2>Mastodonでログイン</h2>
          <label>
            インスタンスURL
            <input
              value={mastodonInstance}
              onChange={(event) => setMastodonInstance(event.target.value)}
              placeholder="mastodon.social"
              required
            />
          </label>
          <button type="submit" disabled={mastodonLoading}>
            {mastodonLoading ? "認証ページへ移動中..." : "Mastodonでログイン"}
          </button>
        </form>
      ) : null}

      {provider === "misskey" ? (
        <form className="stackForm" onSubmit={submitMisskey}>
          <button type="button" className="loginBackButton" onClick={backToSelect}>
            ‹ 選択に戻る
          </button>
          <h2>Misskeyでログイン</h2>
          <label>
            インスタンスURL
            <input
              value={misskeyInstance}
              onChange={(event) => setMisskeyInstance(event.target.value)}
              placeholder="misskey.io"
              required
            />
          </label>
          <button type="submit" disabled={misskeyLoading}>
            {misskeyLoading ? "認証ページへ移動中..." : "Misskeyでログイン"}
          </button>
        </form>
      ) : null}
    </section>
  );
}
