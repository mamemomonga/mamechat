import { FormEvent, useState } from "react";
import { useNavigate } from "react-router";

import { ownerLogin } from "../lib/adminApi";

export default function AdminLoginPage() {
  const navigate = useNavigate();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    try {
      await ownerLogin({ password });
      window.dispatchEvent(new Event("auth-changed"));
      navigate("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "オーナーログインに失敗しました");
    } finally {
      setLoading(false);
    }
  }

  return (
    <section className="narrowPanel">
      <h1>オーナーログイン</h1>
      <form className="stackForm" onSubmit={submit}>
        <label>
          パスワード
          <input
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            type="password"
            autoComplete="owner-password"
            required
          />
        </label>
        {error ? <p className="formError">{error}</p> : null}
        <button type="submit" disabled={loading}>
          {loading ? "ログイン中..." : "ログイン"}
        </button>
      </form>
    </section>
  );
}
