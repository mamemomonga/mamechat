// チャットに添付する画像をクライアント側で縮小・JPEG化するユーティリティ。
// 長辺を最大 maxEdge px に収め、品質 quality の JPEG として書き出す。

const MAX_EDGE = 1600;
const JPEG_QUALITY = 0.8;

export async function resizeImageToJpeg(
  file: File,
  maxEdge = MAX_EDGE,
  quality = JPEG_QUALITY,
): Promise<Blob> {
  const bitmap = await loadBitmap(file);
  try {
    const { width, height } = scaledSize(bitmap.width, bitmap.height, maxEdge);
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      throw new Error("canvasの初期化に失敗しました");
    }
    // 透過PNG等を変換した際に背景が黒くならないよう白で塗る。
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, width, height);
    ctx.drawImage(bitmap, 0, 0, width, height);
    return await canvasToJpeg(canvas, quality);
  } finally {
    if ("close" in bitmap && typeof bitmap.close === "function") {
      bitmap.close();
    }
  }
}

type DrawableImage =
  | ImageBitmap
  | (HTMLImageElement & { width: number; height: number; close?: () => void });

async function loadBitmap(file: File): Promise<DrawableImage> {
  if ("createImageBitmap" in window) {
    try {
      return await createImageBitmap(file);
    } catch {
      // 一部ブラウザ/形式では失敗するため、<img> 経由にフォールバックする。
    }
  }
  return await loadViaImageElement(file);
}

function loadViaImageElement(file: File): Promise<DrawableImage> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.onload = () => {
      URL.revokeObjectURL(url);
      resolve(img as DrawableImage);
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("画像の読み込みに失敗しました"));
    };
    img.src = url;
  });
}

function scaledSize(srcW: number, srcH: number, maxEdge: number) {
  const longest = Math.max(srcW, srcH);
  if (longest <= maxEdge) {
    return { width: srcW, height: srcH };
  }
  const ratio = maxEdge / longest;
  return {
    width: Math.round(srcW * ratio),
    height: Math.round(srcH * ratio),
  };
}

function canvasToJpeg(canvas: HTMLCanvasElement, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) {
          resolve(blob);
        } else {
          reject(new Error("画像の変換に失敗しました"));
        }
      },
      "image/jpeg",
      quality,
    );
  });
}
