import { createContext, useContext, useRef, useState, type ReactNode } from "react";

// Deckの1カラムが表示する内容。
// list=チャンネル一覧、channel=チャンネル、suspended=準備中カード（チャンネルが準備中/キック）。
export type DeckModuleView =
  | { type: "list" }
  | { type: "channel"; slug: string }
  | { type: "suspended"; slug: string; reason: "preparing" | "closed" };

// width はカラム幅(px)。未指定なら既定幅。ドラッグで調整し、保存レイアウトにも含める。
export type DeckModule = { id: string; view: DeckModuleView; width?: number };

let idCounter = 1;
export function newModuleId(): string {
  return `m${idCounter++}`;
}

type DeckContextValue = {
  modules: DeckModule[];
  setModules: React.Dispatch<React.SetStateAction<DeckModule[]>>;
  activeId: string | null;
  setActiveId: React.Dispatch<React.SetStateAction<string | null>>;
  // seeded はルート（URL）から初期カラムを一度だけ生成したか。DeckPageの再マウントをまたいで保持する。
  seeded: React.MutableRefObject<boolean>;
};

const DeckContext = createContext<DeckContextValue | null>(null);

// DeckProvider はDeckのカラム構成をルート遷移（URL更新）をまたいで保持する。App直下に置く。
export function DeckProvider({ children }: { children: ReactNode }) {
  const [modules, setModules] = useState<DeckModule[]>(() => [
    { id: newModuleId(), view: { type: "list" } },
  ]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const seeded = useRef(false);
  return (
    <DeckContext.Provider value={{ modules, setModules, activeId, setActiveId, seeded }}>
      {children}
    </DeckContext.Provider>
  );
}

export function useDeck(): DeckContextValue {
  const ctx = useContext(DeckContext);
  if (!ctx) {
    throw new Error("useDeck must be used within DeckProvider");
  }
  return ctx;
}
