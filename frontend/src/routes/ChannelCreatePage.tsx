import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";

import { createChannel, getMe } from "../lib/api";
import { randomSlug } from "../lib/slugWords";

export default function ChannelCreatePage() {
  const navigate = useNavigate();
  // 初期値として <形容詞>-<名詞>-<名詞> のslug候補を入れておく（考える手間を省く）。
  const [slug, setSlug] = useState(randomSlug);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void (async () => {
      const me = await getMe();
      if (!me.user) {
        navigate("/login");
      }
    })();
  }, [navigate]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      const res = await createChannel({ slug, title, description });
      navigate(`/channels/${res.channel.slug}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "チャンネルの作成に失敗しました");
      setSaving(false);
    }
  }

  return (
    <section className="narrowPanel">
      <div className="sectionHeader">
        <h1>チャンネル作成</h1>
        <Link className="buttonLink secondaryLink" to="/">
          一覧へ戻る
        </Link>
      </div>
      <form className="stackForm" onSubmit={submit}>
        <label>
          URL用文字列<small>(8文字以上50文字まで、英小文字、数字、ハイフン)</small>
          <div className="slugField">
            <input
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
              placeholder="music-channel"
              required
            />
            <button
              type="button"
              className="slugRandomButton"
              onClick={() => setSlug(randomSlug())}
              title="別の候補を生成"
              aria-label="別の候補を生成"
            >
              🎲
            </button>
          </div>
        </label>
        <label>
          タイトル<small>(50文字まで)</small>
          <input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder="音楽チャンネル"
            maxLength={50}
            required
          />
        </label>
        <label>
          説明<small>(200文字まで)</small>
          <textarea
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            rows={3}
            maxLength={200}
          />
        </label>
        {error ? <p className="formError">{error}</p> : null}
        <button type="submit" disabled={saving}>
          {saving ? "作成中..." : "作成"}
        </button>
      </form>
    </section>
  );
}
