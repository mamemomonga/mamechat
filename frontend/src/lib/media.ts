// アップロード素材の種別判定。GIF/APNG/MP4 は「アニメーション素材」として
// 変換せずそのままサーバへ送り、サーバ側で音声なしMP4へ変換する。
// それ以外の静止画は従来どおりクライアントでリサイズ・JPEG化する。

// アニメーション素材として許容する最大バイト数（サーバの UPLOAD_MEDIA_MAX_BYTES と揃える）。
// GIF/APNG と、ブラウザで変換済みのMP4出力に適用する（出力は720p・音声なし・短時間で小さい）。
export const MEDIA_MAX_BYTES = 20 * 1024 * 1024;

// 動画ファイル選択時に受け付ける最大バイト数（ブラウザ側で720p・音声なしへ変換する前の
// 元ファイル上限）。変換後は MEDIA_MAX_BYTES 以内に収まる。
//
// 入力は WORKERFS でマウントし wasm ヒープへ丸ごと読み込まないため、必要メモリは入力サイズ
// ではなく解像度と切り出し尺（最大30秒）で決まる。よって数GB級でも編集できる。上限は
// フリーズ・暴走を避けるための安全弁で、実用上ほぼ全ての端末動画（4K長尺含む）を許容する 8GiB。
export const VIDEO_MAX_BYTES = 8 * 1024 * 1024 * 1024;

// 上限の人間向け表記（エラーメッセージ用）。VIDEO_MAX_BYTES と揃える。
export const VIDEO_MAX_LABEL = "8GB";

// トリミングで切り出せる最大長さ（秒）。サーバの UPLOAD_VIDEO_MAX_SECONDS と揃える。
export const VIDEO_MAX_SECONDS = 30;

// isVideoFile はファイルが動画（トリミングUIの対象）かどうかを返す。
export function isVideoFile(file: File): boolean {
  const type = file.type.toLowerCase();
  const name = file.name.toLowerCase();
  return type.startsWith("video/") || name.endsWith(".mp4") || name.endsWith(".mov");
}

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
