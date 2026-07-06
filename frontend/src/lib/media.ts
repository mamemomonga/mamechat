// アップロード素材の種別判定。GIF/APNG/MP4 は「アニメーション素材」として
// 変換せずそのままサーバへ送り、サーバ側で音声なしMP4へ変換する。
// それ以外の静止画は従来どおりクライアントでリサイズ・JPEG化する。

// アニメーション素材として許容する最大バイト数（サーバの UPLOAD_MEDIA_MAX_BYTES と揃える）。
export const MEDIA_MAX_BYTES = 20 * 1024 * 1024;

// isAnimatedMedia はファイルが GIF/APNG/MP4 のいずれかなら true を返す。
// PNG は静止画とAPNGの区別がMIMEからつかないため、先頭バイトを調べる。
export async function isAnimatedMedia(file: File): Promise<boolean> {
  const type = file.type.toLowerCase();
  const name = file.name.toLowerCase();
  if (type === "video/mp4" || type.startsWith("video/") || name.endsWith(".mp4")) {
    return true;
  }
  if (type === "image/gif" || name.endsWith(".gif")) {
    return true;
  }
  if (type === "image/apng" || name.endsWith(".apng")) {
    return true;
  }
  if (type === "image/png" || name.endsWith(".png")) {
    return await isApng(file);
  }
  return false;
}

// isApng は PNG の先頭領域に acTL チャンク（最初の IDAT より前）があるか調べる。
async function isApng(file: File): Promise<boolean> {
  // acTL は IHDR 直後・IDAT より前に現れるため先頭64KBを見れば十分。
  const slice = file.slice(0, 65536);
  const buf = new Uint8Array(await slice.arrayBuffer());
  const actl = indexOfAscii(buf, "acTL");
  if (actl < 0) {
    return false;
  }
  const idat = indexOfAscii(buf, "IDAT");
  return idat < 0 || actl < idat;
}

function indexOfAscii(haystack: Uint8Array, needle: string): number {
  const n = needle.length;
  for (let i = 0; i + n <= haystack.length; i++) {
    let match = true;
    for (let j = 0; j < n; j++) {
      if (haystack[i + j] !== needle.charCodeAt(j)) {
        match = false;
        break;
      }
    }
    if (match) {
      return i;
    }
  }
  return -1;
}
