import type { ClientToServerMessage, ServerToClientMessage } from "../types/chat";
import { handleServerVersion } from "./version";

const wsBaseUrl =
  import.meta.env.VITE_WS_BASE_URL ||
  `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;

export type WebSocketStatus = "connecting" | "open" | "reconnecting" | "closed" | "error";

const DEFAULT_BEACON_MS = 10000;
const MIN_BEACON_MS = 5000;
const MAX_BEACON_MS = 60000;

function normalizeBeaconMs(value: number | undefined) {
  if (!value || !Number.isFinite(value)) {
    return DEFAULT_BEACON_MS;
  }
  return Math.min(Math.max(value, MIN_BEACON_MS), MAX_BEACON_MS);
}

// connectLobbySocket はチャンネル一覧（ロビー）の在室情報をリアルタイム購読する。
// 投稿やビーコンは送らず、サーバから配信される lobby.presence を受け取るだけ。
export function connectLobbySocket(onMessage: (message: ServerToClientMessage) => void) {
  let socket: WebSocket | null = null;
  let pingTimer: number | undefined;
  let reconnectTimer: number | undefined;
  let closedByClient = false;
  let retryCount = 0;

  connect();

  function connect() {
    if (closedByClient) {
      return;
    }
    const nextSocket = new WebSocket(`${wsBaseUrl}/ws/lobby`);
    socket = nextSocket;

    nextSocket.addEventListener("open", () => {
      if (socket !== nextSocket || closedByClient) {
        nextSocket.close();
        return;
      }
      retryCount = 0;
      pingTimer = window.setInterval(() => {
        if (socket?.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "ping" }));
        }
      }, 25000);
    });
    nextSocket.addEventListener("message", (event) => {
      try {
        const message = JSON.parse(event.data) as ServerToClientMessage;
        if (message.type === "version") {
          handleServerVersion(message.version);
        }
        onMessage(message);
      } catch {
        // 解析できないメッセージは無視する（一覧の再取得には影響しない）。
      }
    });
    nextSocket.addEventListener("close", () => {
      if (socket !== nextSocket) {
        return;
      }
      clearLobbyTimers();
      if (!closedByClient) {
        scheduleReconnect();
      }
    });
    nextSocket.addEventListener("error", () => {
      if (socket === nextSocket && !closedByClient) {
        try {
          nextSocket.close();
        } catch {
          // closeに失敗しても再接続処理は同じ経路で進める。
        }
      }
    });
  }

  function scheduleReconnect() {
    if (closedByClient || reconnectTimer) {
      return;
    }
    if (retryCount >= 20) {
      return;
    }
    const delay = retryCount < 10 ? 5000 : 30000;
    retryCount += 1;
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = undefined;
      connect();
    }, delay);
  }

  function clearLobbyTimers() {
    if (pingTimer) {
      window.clearInterval(pingTimer);
      pingTimer = undefined;
    }
  }

  return {
    close: () => {
      closedByClient = true;
      clearLobbyTimers();
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = undefined;
      }
      socket?.close();
    },
  };
}

export function connectChannelSocket(
  channelSlug: string,
  onMessage: (message: ServerToClientMessage) => void,
  onStatus: (status: WebSocketStatus) => void,
  // アクティブ申告（ビーコン）の送信間隔（ms）。サーバ設定 BEACON_SECONDS 由来。
  beaconMs = DEFAULT_BEACON_MS,
) {
  let socket: WebSocket | null = null;
  let pingTimer: number | undefined;
  let beaconTimer: number | undefined;
  let reconnectTimer: number | undefined;
  let closedByClient = false;
  let terminalError = false;
  let retryCount = 0;
  const normalizedBeaconMs = normalizeBeaconMs(beaconMs);

  connect(false);

  function connect(reconnecting: boolean) {
    if (closedByClient) {
      return;
    }
    onStatus(reconnecting ? "reconnecting" : "connecting");
    const nextSocket = new WebSocket(`${wsBaseUrl}/ws/channels/${encodeURIComponent(channelSlug)}`);
    socket = nextSocket;

    nextSocket.addEventListener("open", () => {
      if (socket !== nextSocket || closedByClient) {
        nextSocket.close();
        return;
      }
      retryCount = 0;
      terminalError = false;
      onStatus("open");
      send({ type: "presence.beacon" });
      beaconTimer = window.setInterval(() => {
        send({ type: "presence.beacon" });
      }, normalizedBeaconMs);
      pingTimer = window.setInterval(() => {
        send({ type: "ping" });
      }, 25000);
    });
    nextSocket.addEventListener("message", (event) => {
      try {
        onMessage(JSON.parse(event.data) as ServerToClientMessage);
      } catch {
        onMessage({ type: "error", message: "サーバーメッセージを解析できませんでした" });
      }
    });
    nextSocket.addEventListener("close", () => {
      if (socket !== nextSocket) {
        return;
      }
      clearSocketTimers();
      if (closedByClient) {
        onStatus("closed");
        return;
      }
      scheduleReconnect();
    });
    nextSocket.addEventListener("error", () => {
      if (socket === nextSocket && !closedByClient) {
        try {
          nextSocket.close();
        } catch {
          // closeに失敗しても再接続処理は同じ経路で進める。
        }
        scheduleReconnect();
      }
    });
  }

  function scheduleReconnect() {
    if (closedByClient || reconnectTimer) {
      return;
    }
    if (retryCount >= 20) {
      if (terminalError) {
        return;
      }
      terminalError = true;
      onStatus("error");
      onMessage({ type: "error", message: "WebSocketエラー" });
      return;
    }
    clearSocketTimers();
    const delay = retryCount < 10 ? 5000 : 30000;
    retryCount += 1;
    onStatus("reconnecting");
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = undefined;
      connect(true);
    }, delay);
  }

  function clearSocketTimers() {
    if (pingTimer) {
      window.clearInterval(pingTimer);
      pingTimer = undefined;
    }
    if (beaconTimer) {
      window.clearInterval(beaconTimer);
      beaconTimer = undefined;
    }
  }

  function send(message: ClientToServerMessage) {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(message));
    }
  }

  return {
    send,
    close: () => {
      closedByClient = true;
      clearSocketTimers();
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = undefined;
      }
      socket?.close();
    },
  };
}
