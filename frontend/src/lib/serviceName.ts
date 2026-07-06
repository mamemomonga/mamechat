import { useEffect, useState } from "react";

import { getPublicConfig } from "./api";

// 既定のサービス名。サーバの SERVICE_NAME 未設定時もこの名前で表示する。
export const DEFAULT_SERVICE_NAME = "mamechat";

// 取得済みのサービス名はモジュール内にキャッシュして再取得を避ける。
let cachedServiceName: string | null = null;

// useServiceName は /api/config からサービス名を取得して返す。
// 取得前は既定値（またはキャッシュ）を返し、取得後に document.title も更新する。
export function useServiceName(suffix?: string): string {
  const [name, setName] = useState(cachedServiceName ?? DEFAULT_SERVICE_NAME);

  useEffect(() => {
    let active = true;
    if (cachedServiceName) {
      setName(cachedServiceName);
    } else {
      void getPublicConfig()
        .then((config) => {
          const resolved = config.serviceName?.trim() || DEFAULT_SERVICE_NAME;
          cachedServiceName = resolved;
          if (active) {
            setName(resolved);
          }
        })
        .catch(() => {
          // 取得失敗時は既定値のまま。
        });
    }
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    document.title = suffix ? `${name} ${suffix}` : name;
  }, [name, suffix]);

  return name;
}
