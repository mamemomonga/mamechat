import type { YouTubeVideo } from "../lib/youtube";

type Props = {
  video: YouTubeVideo;
};

// YouTube動画の埋め込みプレイヤー。privacy-enhanced な youtube-nocookie ドメインを使う。
export default function YouTubeEmbed({ video }: Props) {
  const params = new URLSearchParams({ rel: "0" });
  if (video.start) {
    params.set("start", String(video.start));
  }
  const src = `https://www.youtube-nocookie.com/embed/${video.id}?${params.toString()}`;
  return (
    <div className="youtubeEmbed">
      <iframe
        src={src}
        title="YouTube動画プレイヤー"
        loading="lazy"
        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
        referrerPolicy="strict-origin-when-cross-origin"
        allowFullScreen
      />
    </div>
  );
}
