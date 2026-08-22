// WebASM版ffmpeg（ffmpeg.wasm）で動画をブラウザ内でトリミング・変換する。
// - 音声は除去（-an）
// - 最大720p（短辺を720に収め、拡大はしない）
// - フレームレートは元動画のまま（-r を指定しない）
// COOP/COEP（cross-origin isolation）を必要としないシングルスレッド版コアを使う。
// コアは同一オリジンの /ffmpeg/ から読み込む（vite.config.ts が node_modules から複製）。

import { FFmpeg, FFFSType } from "@ffmpeg/ffmpeg";
import { toBlobURL } from "@ffmpeg/util";

let ffmpegPromise: Promise<FFmpeg> | null = null;

// getFFmpeg は ffmpeg.wasm を一度だけ読み込んで再利用する。
async function getFFmpeg(): Promise<FFmpeg> {
  if (!ffmpegPromise) {
    ffmpegPromise = (async () => {
      const ffmpeg = new FFmpeg();
      const base = `${import.meta.env.BASE_URL ?? "/"}ffmpeg`.replace(/\/{2,}/g, "/");
      await ffmpeg.load({
        coreURL: await toBlobURL(`${base}/ffmpeg-core.js`, "text/javascript"),
        wasmURL: await toBlobURL(`${base}/ffmpeg-core.wasm`, "application/wasm"),
      });
      return ffmpeg;
    })();
    // 読み込みに失敗したら次回リトライできるようキャッシュを捨てる。
    ffmpegPromise.catch(() => {
      ffmpegPromise = null;
    });
  }
  return ffmpegPromise;
}

// inputExt は元ファイル名から拡張子（先頭のドット付き）を推定する。ffmpegのMEMFS上の
// 入力名に使う。判別できなければ .mp4 とする。
function inputExt(file: File): string {
  const m = /\.([a-z0-9]{2,4})$/i.exec(file.name);
  return m ? `.${m[1].toLowerCase()}` : ".mp4";
}

export type VideoProcessProgress = (ratio: number) => void;

// 入力ファイルは WORKERFS でマウントして参照する。ffmpeg の MEMFS（wasmヒープ）へ
// 丸ごとコピーしないため、数GBの動画でもヒープを食い潰さずに扱える。切り出し範囲だけを
// デコード・エンコードするので、必要メモリは解像度と出力尺（最大30秒）で決まり、
// 入力サイズにほぼ依存しない。
const MOUNT_DIR = "/mount";

// processVideoClip は動画の [startSec, endSec) を切り出し、音声なし・最大720p・
// 元フレームレートのMP4(File)を返す。エンコード・トリミングはすべてブラウザ内で行う。
export async function processVideoClip(
  file: File,
  startSec: number,
  endSec: number,
  onProgress?: VideoProcessProgress,
): Promise<File> {
  const duration = Math.max(0, endSec - startSec);
  if (duration <= 0) {
    throw new Error("切り出す範囲を指定してください");
  }
  const ffmpeg = await getFFmpeg();

  // WORKERFS はファイルを name でマウントする。元名の文字化けや記号を避けるため、
  // 拡張子だけ引き継いだ安全な名前へ包み直す（Blobは参照渡しでメモリ複製は起きない）。
  const inName = `input${inputExt(file)}`;
  const mountFile = file.name === inName ? file : new File([file], inName, { type: file.type });
  const inPath = `${MOUNT_DIR}/${inName}`;
  const outName = "out.mp4";

  const handleProgress = (event: { progress: number }) => {
    // ffmpeg の progress は 0..1。範囲外の値が来ることがあるので丸める。
    const ratio = Math.min(1, Math.max(0, event.progress));
    onProgress?.(ratio);
  };
  ffmpeg.on("progress", handleProgress);

  // 前回の残骸があれば掃除してからマウントする（インスタンスは再利用される）。
  await ffmpeg.unmount(MOUNT_DIR).catch(() => {});
  await ffmpeg.deleteDir(MOUNT_DIR).catch(() => {});
  await ffmpeg.createDir(MOUNT_DIR);
  await ffmpeg.mount(FFFSType.WORKERFS, { files: [mountFile] }, MOUNT_DIR);

  try {
    // 入力側シーク（-ss を -i の前）で大きなファイルでも高速に開始位置へ移動する。
    // WORKERFS はランダムアクセス可能なので、巨大ファイルでも必要範囲だけを読む。
    // -an で音声除去。scale は短辺を720に収め拡大はしない（元が小さければそのまま）。
    // 幅・高さは H.264(yuv420p) のため -2 で偶数にそろえAR維持。-r を付けず元fpsを保持。
    await ffmpeg.exec([
      "-ss",
      startSec.toFixed(3),
      "-i",
      inPath,
      "-t",
      duration.toFixed(3),
      "-an",
      "-vf",
      "scale='if(gt(iw,ih),-2,min(720,iw))':'if(gt(iw,ih),min(720,ih),-2)':flags=lanczos",
      "-c:v",
      "libx264",
      "-preset",
      "veryfast",
      "-crf",
      "23",
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      outName,
    ]);
    const data = await ffmpeg.readFile(outName);
    // Uint8Array で返る想定。string の場合は変換失敗とみなす。
    if (typeof data === "string") {
      throw new Error("動画の変換に失敗しました");
    }
    // 一部の型定義環境で BlobPart の受け付けが厳しいため、独立した ArrayBuffer に写す。
    const bytes = new Uint8Array(data as Uint8Array);
    if (bytes.byteLength === 0) {
      throw new Error("動画の変換に失敗しました");
    }
    const blob = new Blob([bytes], { type: "video/mp4" });
    onProgress?.(1);
    return new File([blob], "clip.mp4", { type: "video/mp4" });
  } finally {
    ffmpeg.off("progress", handleProgress);
    // マウントと出力を掃除する（失敗しても致命的ではないので無視）。
    await ffmpeg.unmount(MOUNT_DIR).catch(() => {});
    await ffmpeg.deleteDir(MOUNT_DIR).catch(() => {});
    await ffmpeg.deleteFile(outName).catch(() => {});
  }
}
