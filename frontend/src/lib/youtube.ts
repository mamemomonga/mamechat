// YouTube URLから動画IDと開始秒数を取り出すユーティリティ。
// youtube.com/watch?v=、youtu.be/、youtube.com/embed/、shorts/ に対応する。

export type YouTubeVideo = {
  id: string;
  // 再生開始位置（秒）。?t=90 や ?t=1m30s 形式に対応。指定が無ければ undefined。
  start?: number;
};

// 動画IDは11文字の英数・ハイフン・アンダースコア。
const VIDEO_ID = /^[A-Za-z0-9_-]{11}$/;

function parseStart(value: string | null): number | undefined {
  if (!value) {
    return undefined;
  }
  // 秒数だけ（例: 90）。
  if (/^\d+$/.test(value)) {
    const n = Number(value);
    return n > 0 ? n : undefined;
  }
  // 1h2m3s / 2m30s / 45s 形式。
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(value);
  if (!m || (!m[1] && !m[2] && !m[3])) {
    return undefined;
  }
  const total = Number(m[1] ?? 0) * 3600 + Number(m[2] ?? 0) * 60 + Number(m[3] ?? 0);
  return total > 0 ? total : undefined;
}

// parseYouTube はURLがYouTube動画なら {id, start} を、そうでなければ null を返す。
export function parseYouTube(rawUrl: string): YouTubeVideo | null {
  let url: URL;
  try {
    url = new URL(rawUrl);
  } catch {
    return null;
  }
  const host = url.hostname.replace(/^www\./, "").toLowerCase();
  const start = parseStart(url.searchParams.get("t") || url.searchParams.get("start"));

  // youtu.be/<id>
  if (host === "youtu.be") {
    const id = url.pathname.slice(1).split("/")[0];
    return VIDEO_ID.test(id) ? { id, start } : null;
  }

  if (host === "youtube.com" || host === "m.youtube.com" || host === "music.youtube.com") {
    // youtube.com/watch?v=<id>
    if (url.pathname === "/watch") {
      const id = url.searchParams.get("v") ?? "";
      return VIDEO_ID.test(id) ? { id, start } : null;
    }
    // youtube.com/embed/<id> · youtube.com/shorts/<id> · youtube.com/live/<id> · youtube.com/v/<id>
    const m = /^\/(?:embed|shorts|live|v)\/([A-Za-z0-9_-]{11})/.exec(url.pathname);
    if (m) {
      return { id: m[1], start };
    }
  }
  return null;
}
