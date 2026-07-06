import { useEffect, useState } from "react";

import { fetchLinkPreview } from "../lib/api";
import type { LinkPreview } from "../types/chat";

// 取得済みプレビューはモジュール内にキャッシュし、同じURLの再取得・再描画を避ける。
const cache = new Map<string, LinkPreview | null>();
const inflight = new Map<string, Promise<LinkPreview | null>>();

function loadPreview(url: string): Promise<LinkPreview | null> {
  if (cache.has(url)) {
    return Promise.resolve(cache.get(url) ?? null);
  }
  const existing = inflight.get(url);
  if (existing) {
    return existing;
  }
  const promise = fetchLinkPreview(url)
    .then((preview) => {
      // タイトルも画像も無ければプレビュー対象外として扱う。
      const usable = preview && (preview.title || preview.image) ? preview : null;
      cache.set(url, usable);
      return usable;
    })
    .catch(() => {
      cache.set(url, null);
      return null;
    })
    .finally(() => {
      inflight.delete(url);
    });
  inflight.set(url, promise);
  return promise;
}

type Props = {
  url: string;
};

export default function LinkPreviewCard({ url }: Props) {
  const [preview, setPreview] = useState<LinkPreview | null>(() => cache.get(url) ?? null);
  const [loaded, setLoaded] = useState(cache.has(url));

  useEffect(() => {
    let active = true;
    if (cache.has(url)) {
      setPreview(cache.get(url) ?? null);
      setLoaded(true);
      return;
    }
    setLoaded(false);
    void loadPreview(url).then((result) => {
      if (active) {
        setPreview(result);
        setLoaded(true);
      }
    });
    return () => {
      active = false;
    };
  }, [url]);

  if (!loaded || !preview || (!preview.title && !preview.image)) {
    return null;
  }

  let host = "";
  try {
    host = new URL(preview.url || url).host;
  } catch {
    host = "";
  }

  return (
    <a className="linkPreview" href={preview.url || url} target="_blank" rel="noreferrer noopener">
      {preview.image ? (
        <div className="linkPreviewImage">
          <img src={preview.image} alt="" loading="lazy" referrerPolicy="no-referrer" />
        </div>
      ) : null}
      <div className="linkPreviewBody">
        <span className="linkPreviewSite">{preview.siteName || host}</span>
        {preview.title ? <strong className="linkPreviewTitle">{preview.title}</strong> : null}
        {preview.description ? (
          <span className="linkPreviewDesc">{preview.description}</span>
        ) : null}
      </div>
    </a>
  );
}
