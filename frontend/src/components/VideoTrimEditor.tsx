import { useEffect, useRef, useState } from "react";
import { VIDEO_MAX_SECONDS } from "../lib/media";

type Props = {
  // 元動画ファイル（トリミング前）。
  file: File;
  // ×（キャンセル）押下。
  onCancel: () => void;
  // ○（決定）→ 選択した切り出し範囲[start,end]（秒）を渡す。実際の変換は親が
  //  バックグラウンドで行う（エディタはここで閉じる）。
  onConfirm: (startSec: number, endSec: number) => void;
};

// requestVideoFrameCallback は型定義に無い環境があるため最小限で宣言する。
type FrameMeta = { mediaTime: number; presentedFrames: number };
type VideoWithRVFC = HTMLVideoElement & {
  requestVideoFrameCallback?: (cb: (now: number, meta: FrameMeta) => void) => number;
};

// clamp は値を [min, max] に収める。
function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

// 秒:フレーム 形式（両方2桁）で表示する。
function formatCounter(seconds: number, fps: number): string {
  const whole = Math.floor(seconds);
  const frame = Math.floor((seconds - whole) * fps);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(whole)}:${pad(frame)}`;
}

export default function VideoTrimEditor({ file, onCancel, onConfirm }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const srcUrlRef = useRef<string>("");
  const fpsRef = useRef<number>(30);
  const rafRef = useRef<number>(0);
  // ループ判定は毎フレーム最新の start/end を参照する必要があるため ref に写す
  // （rAFのクロージャが古い値を掴まないように）。
  const startRef = useRef(0);
  const endRef = useRef(0);
  // 直前フレームで再生位置が選択区間内にあったか。範囲外へシークしたらループを止め、
  // 再び範囲内へ入ったらループを再開するために使う。
  const wasInsideRef = useRef(true);

  const [srcUrl, setSrcUrl] = useState<string>("");
  const [duration, setDuration] = useState(0);
  const [currentTime, setCurrentTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [start, setStart] = useState(0);
  const [end, setEnd] = useState(0);
  const [fpsLabel, setFpsLabel] = useState(30);
  const [dragging, setDragging] = useState<null | "start" | "end">(null);

  // start/end の最新値を ref へ同期する。
  useEffect(() => {
    startRef.current = start;
    endRef.current = end;
  }, [start, end]);

  // 選択範囲内かどうか。範囲外ならカウンターは 00:00。
  const inRange = currentTime >= start && currentTime <= end && end > start;
  const counter = inRange ? formatCounter(currentTime - start, fpsLabel) : "00:00";

  // オブジェクトURLの生成・解放。
  useEffect(() => {
    const url = URL.createObjectURL(file);
    srcUrlRef.current = url;
    setSrcUrl(url);
    return () => {
      URL.revokeObjectURL(url);
    };
  }, [file]);

  // 再生中は requestAnimationFrame で滑らかに現在位置を追い、選択区間をループする
  //（終了点に達したら開始点へ戻る）。
  useEffect(() => {
    if (!playing) {
      return;
    }
    const tick = () => {
      const video = videoRef.current;
      if (video) {
        const s = startRef.current;
        const e = endRef.current;
        const t = video.currentTime;
        // 区間内で再生していて終了点に達したら開始点へ戻る（ループ）。
        // ただし範囲外へシークした後（wasInside=false）はループせず素通しし、
        // 再び範囲内へ入ったらループを再開する。
        if (e > s && t >= e && wasInsideRef.current) {
          video.currentTime = s;
          setCurrentTime(s);
          wasInsideRef.current = true;
        } else {
          setCurrentTime(t);
          wasInsideRef.current = e > s && t >= s && t < e;
        }
      }
      rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [playing]);

  // 再生中は requestVideoFrameCallback で実フレームレートを推定する（未対応なら30のまま）。
  useEffect(() => {
    const video = videoRef.current as VideoWithRVFC | null;
    if (!video || !video.requestVideoFrameCallback || !playing) {
      return;
    }
    let prev: FrameMeta | null = null;
    let stopped = false;
    const cb = (_now: number, meta: FrameMeta) => {
      if (prev) {
        const dt = meta.mediaTime - prev.mediaTime;
        const df = meta.presentedFrames - prev.presentedFrames;
        if (dt > 0 && df > 0) {
          const inst = clamp(df / dt, 1, 240);
          // 直近値へ緩やかに寄せて安定させる。
          fpsRef.current = fpsRef.current * 0.7 + inst * 0.3;
          setFpsLabel(Math.round(fpsRef.current));
        }
      }
      prev = meta;
      if (!stopped) {
        video.requestVideoFrameCallback?.(cb);
      }
    };
    video.requestVideoFrameCallback?.(cb);
    return () => {
      stopped = true;
    };
  }, [playing]);

  function handleLoadedMetadata() {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    const dur = Number.isFinite(video.duration) ? video.duration : 0;
    setDuration(dur);
    // 既定の選択範囲は先頭から最大長さ（もしくは動画全体の短いほう）。
    setStart(0);
    setEnd(Math.min(dur, VIDEO_MAX_SECONDS));
    setCurrentTime(0);
    // 編集開始時は自動再生（ミュートなのでモバイルでも許可される）。選択区間をループ再生する。
    void video.play().catch(() => {
      // 自動再生がブロックされても、ユーザーが再生ボタンで開始できる。
    });
  }

  function play() {
    void videoRef.current?.play();
  }

  function pause() {
    videoRef.current?.pause();
  }

  // 指定位置へシークし、そこから再生を開始する。範囲内外に応じてループ状態も更新する。
  function seekTo(t: number) {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    const clamped = clamp(t, 0, duration);
    video.currentTime = clamped;
    setCurrentTime(clamped);
    wasInsideRef.current = end > start && clamped >= start && clamped < end;
    void video.play().catch(() => {
      // 自動再生がブロックされても手動で再生できる。
    });
  }

  function seekBy(delta: number) {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    seekTo(video.currentTime + delta);
  }

  // 開始マーカを現在位置に設定する。終了点は基本そのまま（自動設定しない）。
  // ただし区間が最大長を超える、または終了点が開始点以下になる場合は、
  // 終了点を開始点+最大長（動画終端で頭打ち）へ移動する。
  function setStartMarker() {
    const t = clamp(currentTime, 0, duration);
    setStart(t);
    if (end - t > VIDEO_MAX_SECONDS || end <= t) {
      setEnd(Math.min(t + VIDEO_MAX_SECONDS, duration));
    }
  }

  // 終了マーカを現在位置に設定する。開始点は基本そのまま（自動設定しない）。
  // ただし区間が最大長を超える、または開始点が終了点以上になる場合は、
  // 開始点を終了点-最大長（動画先頭で頭打ち）へ移動する。
  function setEndMarker() {
    const t = clamp(currentTime, 0, duration);
    setEnd(t);
    if (t - start > VIDEO_MAX_SECONDS || start >= t) {
      setStart(Math.max(t - VIDEO_MAX_SECONDS, 0));
    }
  }

  function seekToRatio(ratio: number) {
    if (duration <= 0) {
      return;
    }
    // タッチ/クリックでのシークもその位置から再生を開始する。
    seekTo(ratio * duration);
  }

  function handleTrackClick(event: React.MouseEvent<HTMLDivElement>) {
    if (dragging) {
      return;
    }
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect) {
      return;
    }
    seekToRatio((event.clientX - rect.left) / rect.width);
  }

  // マーカーハンドルのドラッグ。範囲は最大長以内・0..duration に収める。
  function handleHandlePointerDown(which: "start" | "end") {
    return (event: React.PointerEvent) => {
      event.preventDefault();
      event.stopPropagation();
      setDragging(which);
      (event.target as Element).setPointerCapture?.(event.pointerId);
    };
  }

  useEffect(() => {
    if (!dragging) {
      return;
    }
    const onMove = (event: PointerEvent) => {
      const rect = trackRef.current?.getBoundingClientRect();
      if (!rect || duration <= 0) {
        return;
      }
      const t = clamp(((event.clientX - rect.left) / rect.width) * duration, 0, duration);
      if (dragging === "start") {
        // start は end より前、かつ範囲が最大長を超えないよう end-最大長 以上。
        setStart(clamp(t, Math.max(0, end - VIDEO_MAX_SECONDS), Math.max(0, end - 0.1)));
      } else {
        setEnd(clamp(t, Math.min(duration, start + 0.1), Math.min(duration, start + VIDEO_MAX_SECONDS)));
      }
    };
    const onUp = () => setDragging(null);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  }, [dragging, duration, start, end]);

  // ○決定：選択範囲を親へ渡す。変換はバックグラウンドで親が行い、ここは即座に閉じる。
  function confirm() {
    if (end <= start) {
      return;
    }
    onConfirm(start, end);
  }

  const pct = (t: number) => (duration > 0 ? (t / duration) * 100 : 0);
  const rangeSeconds = Math.max(0, end - start);

  return (
    <div className="videoTrimOverlay" role="dialog" aria-label="動画のトリミング" aria-modal="true">
      <div className="videoTrimPanel">
        <div className="videoTrimStage">
          {srcUrl ? (
            <video
              ref={videoRef}
              className="videoTrimVideo"
              src={srcUrl}
              muted
              autoPlay
              playsInline
              onLoadedMetadata={handleLoadedMetadata}
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
              onTimeUpdate={() => {
                const v = videoRef.current;
                if (v && !playing) {
                  setCurrentTime(v.currentTime);
                }
              }}
            />
          ) : null}
          <div className="videoTrimCounter" aria-label="選択範囲内の再生位置（秒:フレーム）">
            {counter}
          </div>
        </div>

        <div className="videoTrimTimeline">
          <div className="videoTrimTrack" ref={trackRef} onClick={handleTrackClick}>
            <div
              className="videoTrimSelection"
              style={{ left: `${pct(start)}%`, width: `${pct(end - start)}%` }}
            />
            <div className="videoTrimPlayhead" style={{ left: `${pct(currentTime)}%` }} />
            <button
              type="button"
              className="videoTrimHandle videoTrimHandleStart"
              style={{ left: `${pct(start)}%` }}
              onPointerDown={handleHandlePointerDown("start")}
              aria-label="開始マーカー"
            />
            <button
              type="button"
              className="videoTrimHandle videoTrimHandleEnd"
              style={{ left: `${pct(end)}%` }}
              onPointerDown={handleHandlePointerDown("end")}
              aria-label="終了マーカー"
            />
          </div>
          <div className="videoTrimRangeInfo">
            選択 {rangeSeconds.toFixed(1)} 秒 / 最大 {VIDEO_MAX_SECONDS} 秒（音声なし・最大720p）
          </div>
        </div>

        <div className="videoTrimControls">
          <button type="button" className="videoTrimBtn" onClick={play} disabled={playing} aria-label="再生">
            <IconPlay />
          </button>
          <button type="button" className="videoTrimBtn" onClick={pause} disabled={!playing} aria-label="一時停止">
            <IconPause />
          </button>
          <button type="button" className="videoTrimBtn" onClick={() => seekBy(-10)} aria-label="10秒戻す">
            <IconBack10 />
          </button>
          <button type="button" className="videoTrimBtn" onClick={() => seekBy(10)} aria-label="10秒進める">
            <IconFwd10 />
          </button>
          <button type="button" className="videoTrimBtn" onClick={setStartMarker} aria-label="開始マーカーを設定">
            <IconMarkStart />
          </button>
          <button type="button" className="videoTrimBtn" onClick={setEndMarker} aria-label="終了マーカーを設定">
            <IconMarkEnd />
          </button>
          <button
            type="button"
            className="videoTrimBtn videoTrimConfirm"
            onClick={confirm}
            aria-label="採用"
            title="採用"
          >
            <IconConfirm />
          </button>
          <button
            type="button"
            className="videoTrimBtn videoTrimCancel"
            onClick={onCancel}
            aria-label="キャンセル"
            title="キャンセル"
          >
            <IconCancel />
          </button>
        </div>
      </div>
    </div>
  );
}

// --- アイコン（インラインSVG。太めのストロークでボタン内に収める） ---
const svgProps = {
  width: 22,
  height: 22,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
};

function IconPlay() {
  return (
    <svg {...svgProps}>
      <path d="M7 5l12 7-12 7z" fill="currentColor" stroke="none" />
    </svg>
  );
}
function IconPause() {
  return (
    <svg {...svgProps}>
      <rect x="6" y="5" width="4" height="14" fill="currentColor" stroke="none" />
      <rect x="14" y="5" width="4" height="14" fill="currentColor" stroke="none" />
    </svg>
  );
}
function IconBack10() {
  return (
    <svg {...svgProps}>
      <path d="M11 7L6 12l5 5" />
      <path d="M18 7l-5 5 5 5" />
    </svg>
  );
}
function IconFwd10() {
  return (
    <svg {...svgProps}>
      <path d="M13 7l5 5-5 5" />
      <path d="M6 7l5 5-5 5" />
    </svg>
  );
}
function IconMarkStart() {
  return (
    <svg {...svgProps}>
      <line x1="7" y1="4" x2="7" y2="20" />
      <path d="M20 7l-7 5 7 5z" fill="currentColor" stroke="none" />
    </svg>
  );
}
function IconMarkEnd() {
  return (
    <svg {...svgProps}>
      <line x1="17" y1="4" x2="17" y2="20" />
      <path d="M4 7l7 5-7 5z" fill="currentColor" stroke="none" />
    </svg>
  );
}
function IconConfirm() {
  return (
    <svg {...svgProps}>
      <circle cx="12" cy="12" r="8" />
    </svg>
  );
}
function IconCancel() {
  return (
    <svg {...svgProps}>
      <line x1="6" y1="6" x2="18" y2="18" />
      <line x1="18" y1="6" x2="6" y2="18" />
    </svg>
  );
}
