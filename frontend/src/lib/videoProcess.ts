// WebASM版ffmpeg（ffmpeg.wasm）で動画をブラウザ内でトリミング・変換する。
// - 音声は除去（-an）
// - 最大720p（短辺を720に収め、拡大はしない）
// - フレームレートは元動画のまま（-r を指定しない）
// COOP/COEP（cross-origin isolation）を必要としないシングルスレッド版コアを使う。
// コアは同一オリジンの /ffmpeg/ から読み込む（vite.config.ts が node_modules から複製）。

import { FFmpeg, FFFSType } from "@ffmpeg/ffmpeg";
import { fetchFile } from "@ffmpeg/util";

// VideoProcessError は失敗の要約（message）と、ffmpeg のログなどの詳細（detail）を持つ。
// UI で詳細を折りたたみ表示できるようにするための型。
export class VideoProcessError extends Error {
  detail: string;
  constructor(message: string, detail = "") {
    super(message);
    this.name = "VideoProcessError";
    this.detail = detail;
  }
}

// toDetail は任意の例外値を人が読める文字列へ変換する（Errorでない値も落とさない）。
function toDetail(err: unknown): string {
  if (err instanceof Error) {
    return err.stack ? `${err.message}\n${err.stack}` : err.message;
  }
  if (typeof err === "string") {
    return err;
  }
  try {
    return JSON.stringify(err);
  } catch {
    return String(err);
  }
}

let ffmpegPromise: Promise<FFmpeg> | null = null;

// getFFmpeg は ffmpeg.wasm を一度だけ読み込んで再利用する。読み込み失敗は詳細付きで投げる。
async function getFFmpeg(): Promise<FFmpeg> {
  if (!ffmpegPromise) {
    ffmpegPromise = (async () => {
      const ffmpeg = new FFmpeg();
      // 同一オリジンから配信するコアを直接URLで指定する（blob: を使わない）。
      // 本番CSPは script-src 'self' のため、blob: モジュールをワーカーで動的importすると
      // ブロックされる（Failed to fetch dynamically imported module: blob:...）。
      // 直接の same-origin URL なら 'self' で読み込める。
      const base = new URL(`${import.meta.env.BASE_URL || "/"}ffmpeg/`, window.location.href);
      try {
        await ffmpeg.load({
          coreURL: new URL("ffmpeg-core.js", base).href,
          wasmURL: new URL("ffmpeg-core.wasm", base).href,
        });
      } catch (err) {
        throw new VideoProcessError(
          "動画変換エンジン(ffmpeg)の読み込みに失敗しました。ネットワークまたは配信設定を確認してください。",
          toDetail(err),
        );
      }
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

// WORKERFS が使えない環境では MEMFS へ書き込む方式にフォールバックする。ただし wasm ヒープ
// （約2GB）に載せるため、この上限までのファイルに限る。
const FALLBACK_MAX_BYTES = 512 * 1024 * 1024;

// processVideoClip は動画の [startSec, endSec) を切り出し、音声なし・最大720p・
// 元フレームレートのMP4(File)を返す。エンコード・トリミングはすべてブラウザ内で行う。
// 失敗時は VideoProcessError（要約＋詳細）を投げる。
export async function processVideoClip(
  file: File,
  startSec: number,
  endSec: number,
  onProgress?: VideoProcessProgress,
): Promise<File> {
  const duration = Math.max(0, endSec - startSec);
  if (duration <= 0) {
    throw new VideoProcessError("切り出す範囲を指定してください");
  }
  const ffmpeg = await getFFmpeg();

  // WORKERFS はファイルを name でマウントする。元名の文字化けや記号を避けるため、
  // 拡張子だけ引き継いだ安全な名前へ包み直す（Blobは参照渡しでメモリ複製は起きない）。
  const inName = `input${inputExt(file)}`;
  const mountFile = file.name === inName ? file : new File([file], inName, { type: file.type });
  const outName = "out.mp4";

  // ffmpeg のログを蓄積し、失敗時に末尾を詳細として見せる。
  const logLines: string[] = [];
  const onLog = (event: { message: string }) => {
    logLines.push(event.message);
    if (logLines.length > 400) {
      logLines.shift();
    }
  };
  const logTail = () => logLines.slice(-40).join("\n");

  const handleProgress = (event: { progress: number }) => {
    // ffmpeg の progress は 0..1。範囲外の値が来ることがあるので丸める。
    const ratio = Math.min(1, Math.max(0, event.progress));
    onProgress?.(ratio);
  };
  ffmpeg.on("progress", handleProgress);
  ffmpeg.on("log", onLog);

  let mounted = false;
  let inPath = `${MOUNT_DIR}/${inName}`;
  try {
    // まず WORKERFS マウントを試す。失敗した環境では小さいファイルに限り MEMFS へ退避する。
    try {
      await ffmpeg.unmount(MOUNT_DIR).catch(() => {});
      await ffmpeg.deleteDir(MOUNT_DIR).catch(() => {});
      await ffmpeg.createDir(MOUNT_DIR);
      await ffmpeg.mount(FFFSType.WORKERFS, { files: [mountFile] }, MOUNT_DIR);
      mounted = true;
    } catch (mountErr) {
      if (file.size > FALLBACK_MAX_BYTES) {
        throw new VideoProcessError(
          "この環境では大きな動画を読み込めませんでした。もう少し小さい動画でお試しください。",
          toDetail(mountErr),
        );
      }
      await ffmpeg.writeFile(inName, await fetchFile(file));
      inPath = inName;
    }

    // 入力側シーク（-ss を -i の前）で大きなファイルでも高速に開始位置へ移動する。
    // WORKERFS はランダムアクセス可能なので、巨大ファイルでも必要範囲だけを読む。
    // -an で音声除去。scale は短辺を720に収め拡大はしない（元が小さければそのまま）。
    // H.264(yuv420p) は幅・高さが偶数でなければエンコードに失敗するため、720に丸めた側も
    // trunc(.../2)*2 で必ず偶数化する（自動側は -2 で偶数化）。元寸が奇数の動画で
    // 「対応していない形式」になるのを防ぐ。-r を付けず元fpsを保持。
    const code = await ffmpeg.exec([
      "-ss",
      startSec.toFixed(3),
      "-i",
      inPath,
      "-t",
      duration.toFixed(3),
      "-an",
      "-vf",
      "scale='if(gt(iw,ih),-2,trunc(min(720,iw)/2)*2)':'if(gt(iw,ih),trunc(min(720,ih)/2)*2,-2)':flags=lanczos",
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
    if (code !== 0) {
      throw new VideoProcessError(
        `動画の変換に失敗しました（ffmpeg 終了コード ${code}）。この動画形式に対応していない可能性があります。`,
        logTail(),
      );
    }

    let data;
    try {
      data = await ffmpeg.readFile(outName);
    } catch (readErr) {
      throw new VideoProcessError("変換結果を取り出せませんでした。", logTail() || toDetail(readErr));
    }
    if (typeof data === "string") {
      throw new VideoProcessError("変換結果の形式が不正です。", logTail());
    }
    // 一部の型定義環境で BlobPart の受け付けが厳しいため、独立した ArrayBuffer に写す。
    const bytes = new Uint8Array(data as Uint8Array);
    if (bytes.byteLength === 0) {
      throw new VideoProcessError("変換結果が空でした。", logTail());
    }
    const blob = new Blob([bytes], { type: "video/mp4" });
    onProgress?.(1);
    return new File([blob], "clip.mp4", { type: "video/mp4" });
  } catch (err) {
    if (err instanceof VideoProcessError) {
      throw err;
    }
    // 予期しない失敗（ワーカーのクラッシュ等）。ffmpegログがあれば詳細に添える。
    throw new VideoProcessError("動画の処理に失敗しました。", logTail() || toDetail(err));
  } finally {
    ffmpeg.off("progress", handleProgress);
    ffmpeg.off("log", onLog);
    // 使ったFS（マウント/MEMFS）と出力を掃除する（失敗しても致命的ではないので無視）。
    if (mounted) {
      await ffmpeg.unmount(MOUNT_DIR).catch(() => {});
      await ffmpeg.deleteDir(MOUNT_DIR).catch(() => {});
    } else {
      await ffmpeg.deleteFile(inName).catch(() => {});
    }
    await ffmpeg.deleteFile(outName).catch(() => {});
  }
}
