import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { useNavigate } from "react-router";

import {
  newModuleId,
  useDeck,
  type DeckModule,
  type DeckModuleView,
} from "../context/DeckContext";
import { getChannel, getDeckLayout, saveDeckLayout, setChannelNotify } from "../lib/api";
import { useWide } from "../lib/deckMode";
import type { DeckColumn } from "../types/chat";
import type { WebSocketStatus } from "../lib/websocket";
import { ChannelView } from "./ChannelPage";
import { ChannelListView } from "./HomePage";

type DeckPageProps = {
  // 現在のURLの :slug（"/" のときは null）。初期カラムのシード／外部ナビの反映に使う。
  routeSlug: string | null;
};

// view から URL パスを求める。list→"/"、channel/suspended→"/channels/slug"。
function viewPath(view: DeckModuleView | undefined): string {
  if (view && (view.type === "channel" || view.type === "suspended")) {
    return `/channels/${view.slug}`;
  }
  return "/";
}

function channelSlugOf(view: DeckModuleView): string | null {
  return view.type === "channel" || view.type === "suspended" ? view.slug : null;
}

// カラム幅（px）の既定・下限・上限。ドラッグ調整時にクランプする。
const DEFAULT_COL_WIDTH = 600;
const MIN_COL_WIDTH = 320;
const MAX_COL_WIDTH = 1000;
function clampWidth(w: number): number {
  return Math.max(MIN_COL_WIDTH, Math.min(MAX_COL_WIDTH, Math.round(w)));
}

function uniqueModules(input: DeckModule[]): DeckModule[] {
  const out: DeckModule[] = [];
  const seenSlugs = new Set<string>();
  let hasList = false;
  for (const mod of input) {
    const slug = channelSlugOf(mod.view);
    if (!slug) {
      if (hasList) {
        continue;
      }
      hasList = true;
      out.push(mod);
      continue;
    }
    if (seenSlugs.has(slug)) {
      continue;
    }
    seenSlugs.add(slug);
    out.push(mod);
  }
  return out.length > 0 ? out : [{ id: newModuleId(), view: { type: "list" } }];
}

export default function DeckPage({ routeSlug }: DeckPageProps) {
  const navigate = useNavigate();
  const wide = useWide();
  const { modules, setModules, activeId, setActiveId, seeded } = useDeck();
  // 自分でURLを更新したときのループ防止フラグ。
  const selfNav = useRef(false);
  // 外部ナビ反映effectの初回実行（＝マウント時のrouteSlug）はスキップする。シードに委ねる。
  const routeInitDone = useRef(false);
  const modulesRef = useRef(modules);
  modulesRef.current = modules;
  const activeIdRef = useRef(activeId);
  activeIdRef.current = activeId;
  // チャンネルカラムの接続状態（カラムID→状態）。
  const [statuses, setStatuses] = useState<Record<string, WebSocketStatus>>({});
  // ready はシード完了後にURL同期を有効化するフラグ（初回描画でdeep linkを潰さないため）。
  const [ready, setReady] = useState(false);
  // カラム構成のセーブ/ロード完了をボタン点滅で知らせる。
  const [flashAction, setFlashAction] = useState<"load" | "save" | null>(null);
  const flashTimerRef = useRef<number | undefined>(undefined);
  const flashFrameRef = useRef<number | undefined>(undefined);

  // 初回のみ: URLのslugから先頭カラムを構成し、アクティブにする（deep link対応）。
  // 再マウント（deckモード切替）時はシード済みなので構成は保持し、URL同期だけ再有効化する。
  useEffect(() => {
    if (!seeded.current) {
      seeded.current = true;
      const firstId = modulesRef.current[0]?.id ?? null;
      if (routeSlug && firstId) {
        setModules((prev) =>
          prev.map((m, i) => (i === 0 ? { ...m, view: { type: "channel", slug: routeSlug } } : m)),
        );
      }
      setActiveId(firstId);
    } else if (routeSlug) {
      // シード済みでも、非deckルート（チャンネル作成/設定など）から特定チャンネルのURLへ
      // 戻ってきてそのチャンネルがどのカラムにも無ければ開く。チャンネル作成直後に新しい
      // チャンネルへ入るケースなど（外部ナビeffectは初回マウント時はスキップするため）。
      const list = modulesRef.current;
      const existing = list.find((m) => channelSlugOf(m.view) === routeSlug);
      if (existing) {
        setActiveId(existing.id);
      } else if (list.length > 0) {
        const targetId =
          activeIdRef.current && list.some((m) => m.id === activeIdRef.current)
            ? activeIdRef.current
            : list[0].id;
        setModules((prev) =>
          prev.map((m) =>
            m.id === targetId ? { ...m, view: { type: "channel", slug: routeSlug } } : m,
          ),
        );
        setActiveId(targetId);
      }
    }
    setReady(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    return () => {
      window.clearTimeout(flashTimerRef.current);
      if (flashFrameRef.current !== undefined) {
        cancelAnimationFrame(flashFrameRef.current);
      }
    };
  }, []);

  // 各カラムの接続状態を1つへ集約し、topBar（App）のステータスピルへ通知する。
  // カラムごとの個別ピルは廃止し、ログインユーザ左横に1つだけ表示する。
  useEffect(() => {
    const detail = aggregateStatus(modules, statuses);
    window.dispatchEvent(new CustomEvent("channel-status-changed", { detail }));
  }, [modules, statuses]);

  // Deckモードを離れるときはピルを消す。
  useEffect(() => {
    return () => {
      window.dispatchEvent(new CustomEvent("channel-status-changed", { detail: null }));
    };
  }, []);

  // アクティブカラムの内容をURLへ反映する（deck→URL）。
  useEffect(() => {
    if (!ready) {
      return;
    }
    const active = modules.find((m) => m.id === activeId);
    const target = active ? viewPath(active.view) : "/";
    const current = routeSlug ? `/channels/${routeSlug}` : "/";
    if (target !== current) {
      selfNav.current = true;
      navigate(target);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId, modules, ready]);

  // 外部ナビ（戻る/進む/topBar等）でURLが変わったらアクティブカラムへ反映する（URL→deck）。
  useEffect(() => {
    // マウント時の初回（＝シード対象）はスキップする。
    if (!routeInitDone.current) {
      routeInitDone.current = true;
      return;
    }
    if (selfNav.current) {
      selfNav.current = false;
      return;
    }
    if (!routeSlug) {
      // カラム以外（topBar等）でルートへ → アクティブ解除のみ（カラムは保持）。
      setActiveId(null);
      return;
    }
    const list = modulesRef.current;
    if (list.length === 0) {
      return;
    }
    const existing = list.find((m) => channelSlugOf(m.view) === routeSlug);
    if (existing) {
      setActiveId(existing.id);
      return;
    }
    const targetId =
      activeIdRef.current && list.some((m) => m.id === activeIdRef.current)
        ? activeIdRef.current
        : list[0].id;
    setModules((prev) =>
      prev.map((m) => (m.id === targetId ? { ...m, view: { type: "channel", slug: routeSlug } } : m)),
    );
    setActiveId(targetId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [routeSlug]);

  function updateView(moduleId: string, view: DeckModuleView) {
    setModules((prev) => prev.map((m) => (m.id === moduleId ? { ...m, view } : m)));
  }

  function openChannelInModule(moduleId: string, slug: string) {
    const existing = modulesRef.current.find((m) => channelSlugOf(m.view) === slug);
    if (existing) {
      if (existing.view.type === "suspended") {
        updateView(existing.id, { type: "channel", slug });
      }
      setActiveId(existing.id);
      return;
    }
    updateView(moduleId, { type: "channel", slug });
    setActiveId(moduleId);
  }

  function addListModule() {
    const existing = modulesRef.current.find((m) => m.view.type === "list");
    if (existing) {
      setActiveId(existing.id);
      return;
    }
    const id = newModuleId();
    setModules((prev) => [...prev, { id, view: { type: "list" } }]);
    setActiveId(id);
  }

  function closeModule(moduleId: string) {
    setModules((prev) => prev.filter((m) => m.id !== moduleId));
    setStatuses((prev) => {
      const next = { ...prev };
      delete next[moduleId];
      return next;
    });
    setActiveId((cur) => (cur === moduleId ? null : cur));
  }

  // カラム右端のハンドルをドラッグして幅を調整する。ドラッグ中はDOMを直接更新し、
  // 離したときに幅を状態へ確定する（重いカラムの再描画を避けるため）。
  function startResize(event: ReactPointerEvent, moduleId: string) {
    const moduleEl = (event.currentTarget as HTMLElement).closest(".deckModule") as HTMLElement | null;
    if (!moduleEl) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    const startX = event.clientX;
    const startWidth = moduleEl.getBoundingClientRect().width;
    function move(e: PointerEvent) {
      const w = clampWidth(startWidth + (e.clientX - startX));
      moduleEl!.style.width = `${w}px`;
      moduleEl!.style.maxWidth = `${w}px`;
    }
    function up() {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      const finalWidth = clampWidth(moduleEl!.getBoundingClientRect().width);
      setModules((prev) => prev.map((m) => (m.id === moduleId ? { ...m, width: finalWidth } : m)));
    }
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  }

  async function saveColumns() {
    const columns: DeckColumn[] = uniqueModules(modules).map((m) => {
      const base: DeckColumn =
        m.view.type === "list" ? { type: "list" } : { type: "channel", slug: m.view.slug };
      return m.width ? { ...base, width: m.width } : base;
    });
    try {
      await saveDeckLayout(columns);
      flashButton("save");
    } catch {
      // 完了していないため点滅させない。
    }
  }

  async function restoreColumns() {
    try {
      const res = await getDeckLayout();
      if (!res.columns || res.columns.length === 0) {
        return;
      }
      const restored = uniqueModules(
        res.columns.map((c) => ({
          id: newModuleId(),
          view: c.type === "channel" ? { type: "channel", slug: c.slug } : { type: "list" },
          width: c.width && c.width > 0 ? clampWidth(c.width) : undefined,
        })),
      );
      setModules(restored);
      setActiveId(restored[0]?.id ?? null);
      flashButton("load");
    } catch {
      // 完了していないため点滅させない。
    }
  }

  function flashButton(action: "load" | "save") {
    window.clearTimeout(flashTimerRef.current);
    if (flashFrameRef.current !== undefined) {
      cancelAnimationFrame(flashFrameRef.current);
    }
    setFlashAction(null);
    flashFrameRef.current = requestAnimationFrame(() => {
      setFlashAction(action);
      flashTimerRef.current = window.setTimeout(() => setFlashAction(null), 500);
    });
  }

  // <1400px は先頭カラムのみ表示・横スクロールなし。>=1400px で全カラム＋空きカラム＋横スクロール。
  const visibleModules = wide ? modules : modules.slice(0, 1);
  const realCount = modules.length;

  return (
    <div className="deckRoot">
      <div
        className={`deck${wide ? "" : " narrow"}`}
        onPointerDown={(event) => {
          // カラム外（隙間）をクリックしたらアクティブ解除→ルート。
          if (event.target === event.currentTarget) {
            setActiveId(null);
          }
        }}
      >
        {visibleModules.map((module) => (
          <div
            key={module.id}
            className={`deckModule${activeId === module.id ? " active" : ""}`}
            style={{
              width: `${module.width ?? DEFAULT_COL_WIDTH}px`,
              maxWidth: `${module.width ?? DEFAULT_COL_WIDTH}px`,
            }}
            onPointerDownCapture={() => setActiveId(module.id)}
          >
            {wide && realCount > 1 ? (
              <button
                type="button"
                className="deckModuleClose"
                aria-label="カラムを閉じる"
                title="カラムを閉じる"
                onClick={() => closeModule(module.id)}
              >
                ×
              </button>
            ) : null}
            <div className="deckModuleBody">
              {module.view.type === "list" ? (
                <ChannelListView onOpenChannel={(slug) => openChannelInModule(module.id, slug)} />
              ) : module.view.type === "channel" ? (
                <ChannelView
                  key={module.view.slug}
                  slug={module.view.slug}
                  embedded
                  onExit={() => updateView(module.id, { type: "list" })}
                  onStatus={(s) =>
                    setStatuses((prev) => ({ ...prev, [module.id]: s }))
                  }
                  onSuspended={(reason) =>
                    updateView(module.id, {
                      type: "suspended",
                      slug: (module.view as { slug: string }).slug,
                      reason,
                    })
                  }
                />
              ) : (
                <SuspendedCard
                  slug={module.view.slug}
                  reason={module.view.reason}
                  onOpenList={() => updateView(module.id, { type: "list" })}
                />
              )}
            </div>
            {wide ? (
              <div
                className="deckModuleResizer"
                onPointerDown={(event) => startResize(event, module.id)}
                title="ドラッグで幅を調整"
                aria-hidden="true"
              />
            ) : null}
          </div>
        ))}

        {wide ? (
          <div className="deckModuleEmpty">
            <button
              type="button"
              className="deckEmptyButton"
              aria-label="カラムを追加"
              title="チャンネル一覧のカラムを追加"
              onClick={addListModule}
            >
              <span className="deckEmptyPlus" aria-hidden="true">
                ＋
              </span>
            </button>
            <button
              type="button"
              className={`deckEmptyButton${flashAction === "load" ? " deckEmptyButtonFlash" : ""}`}
              aria-label="カラム構成をロード"
              title="カラム構成をロード"
              onClick={() => void restoreColumns()}
            >
              <svg viewBox="0 0 24 24" width="28" height="28" aria-hidden="true">
                <path
                  d="M5 11.5a1 1 0 0 1 1-1h3.5v2H7v5h10v-5h-2.5v-2H18a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-7Z"
                  fill="currentColor"
                />
                <path
                  d="M11 15V7.8L8.7 10.1 7.3 8.7 12 4l4.7 4.7-1.4 1.4L13 7.8V15h-2Z"
                  fill="currentColor"
                />
              </svg>
            </button>
            <button
              type="button"
              className={`deckEmptyButton${flashAction === "save" ? " deckEmptyButtonFlash" : ""}`}
              aria-label="カラム構成をセーブ"
              title="カラム構成をセーブ"
              onClick={() => void saveColumns()}
            >
              <svg viewBox="0 0 24 24" width="28" height="28" aria-hidden="true">
                <path
                  d="M5 11.5a1 1 0 0 1 1-1h3.5v2H7v5h10v-5h-2.5v-2H18a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-7Z"
                  fill="currentColor"
                />
                <path
                  d="M11 4h2v7.2l2.3-2.3 1.4 1.4L12 15l-4.7-4.7 1.4-1.4 2.3 2.3V4Z"
                  fill="currentColor"
                />
              </svg>
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}

// SuspendedCard は準備中カラムの表示。準備中の案内と「開店したら通知」トグルを出す。
function SuspendedCard({
  slug,
  reason,
  onOpenList,
}: {
  slug: string;
  reason: "preparing" | "closed";
  onOpenList: () => void;
}) {
  const [title, setTitle] = useState(slug);
  const [notifyOn, setNotifyOn] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let active = true;
    void getChannel(slug)
      .then((res) => {
        if (!active) {
          return;
        }
        setTitle(res.channel.title);
        setNotifyOn(res.channel.notifyEnabled ?? false);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [slug]);

  async function toggle() {
    if (busy) {
      return;
    }
    setBusy(true);
    try {
      const res = await setChannelNotify(slug, !notifyOn);
      setNotifyOn(res.enabled);
    } catch {
      // 失敗は無視。
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="deckSuspendedCard">
      <span className="channelStatusBadge resting">準備中</span>
      <h2 className="deckSuspendedTitle">{title}</h2>
      <p className="muted">
        {reason === "closed"
          ? "このチャンネルは準備中になりました。"
          : "このチャンネルは現在準備中です。"}
      </p>
      <div className="ttsControls" aria-label="開店通知">
        <button
          type="button"
          className={notifyOn ? "ttsToggle enabled" : "ttsToggle"}
          onClick={() => void toggle()}
          disabled={busy}
        >
          <span className={notifyOn ? "ttsLed on" : "ttsLed off"} aria-hidden="true" />
          開店したら通知
        </button>
      </div>
      <button type="button" className="buttonLink secondaryLink" onClick={onOpenList}>
        チャンネル一覧へ
      </button>
    </div>
  );
}

// 接続状態の深刻度。集約時は最も重い状態を代表として選ぶ（openは全カラムopen時のみ）。
const STATUS_SEVERITY: Record<WebSocketStatus, number> = {
  open: 0,
  closed: 1,
  connecting: 2,
  reconnecting: 3,
  error: 4,
};

// aggregateStatus は表示中チャンネルカラムの接続状態を1つへ集約する。
// チャンネルカラムが無ければ null（topBarのピルを出さない）。
function aggregateStatus(
  modules: DeckModule[],
  statuses: Record<string, WebSocketStatus>,
): WebSocketStatus | null {
  let worst: WebSocketStatus | null = null;
  for (const m of modules) {
    if (m.view.type !== "channel") {
      continue;
    }
    const s = statuses[m.id];
    if (!s) {
      continue;
    }
    if (worst === null || STATUS_SEVERITY[s] > STATUS_SEVERITY[worst]) {
      worst = s;
    }
  }
  return worst;
}
