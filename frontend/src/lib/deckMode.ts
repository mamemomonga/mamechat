import { useEffect, useState } from "react";

// Deck（複数カラム同時表示）を有効にする画面幅の下限。2カラム(600px)×2=1200px。
// これ未満では先頭カラムのみ・横スクロールなし。以上で2カラム目以降と横スクロールが現れる。
export const DECK_WIDE_MIN = 1200;

// スマートフォン（Android/iPhone）。ここではDeckを使わず現状レイアウトを維持する。
function isSmartphone(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  const ua = navigator.userAgent;
  return /Android/i.test(ua) || /iPhone|iPod/i.test(ua);
}

// ハンドヘルド（スマホ＋iPad）。iPadは横向きのときだけDeckを有効にする。
function isHandheld(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  const ua = navigator.userAgent;
  const isAndroid = /Android/i.test(ua);
  const isiOS = /iPhone|iPad|iPod/i.test(ua);
  const isiPadOS = navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1;
  return isAndroid || isiOS || isiPadOS;
}

// Deck有効判定: スマホ以外 かつ (PC または 横向き)。iPadは横向きのみ、PCは常に有効。
function computeDeckMode(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  if (isSmartphone()) {
    return false;
  }
  const isPC = !isHandheld();
  const landscape = window.innerWidth > window.innerHeight;
  return isPC || landscape;
}

// useDeckMode はDeck表示が有効か（PC/iPad横向き）を返し、リサイズ・回転で追従する。
export function useDeckMode(): boolean {
  const [deck, setDeck] = useState(computeDeckMode);
  useEffect(() => {
    const update = () => setDeck(computeDeckMode());
    window.addEventListener("resize", update);
    window.addEventListener("orientationchange", update);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("orientationchange", update);
    };
  }, []);
  return deck;
}

// useWide はウィンドウ幅が DECK_WIDE_MIN 以上か（2カラム目以降・横スクロール解禁）を返す。
export function useWide(): boolean {
  const [wide, setWide] = useState(
    () => typeof window !== "undefined" && window.innerWidth >= DECK_WIDE_MIN,
  );
  useEffect(() => {
    const update = () => setWide(window.innerWidth >= DECK_WIDE_MIN);
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);
  return wide;
}
