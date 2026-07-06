import UnavailableNotice from "../components/UnavailableNotice";

// /unavailable への直接アクセス用。停止ユーザーはApp側でこのページ内容へ誘導される。
export default function UnavailablePage() {
  return <UnavailableNotice />;
}
