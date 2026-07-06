import SettingsPanel from "../components/SettingsPanel";

// /settings への直接アクセス用のフルページ表示。アプリ内ではモーダルで開く（App.tsx）。
export default function SettingsPage() {
  return (
    <section className="narrowPanel">
      <h1>設定</h1>
      <SettingsPanel />
    </section>
  );
}
