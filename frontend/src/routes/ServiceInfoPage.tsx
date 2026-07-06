import { useEffect, useState } from "react";
import { useNavigate } from "react-router";

import { getServiceInfo } from "../lib/api";

type ServiceInfo = {
  serviceName: string;
  version: string;
  headerImageUrl: string;
  overviewHtml: string;
  appInfoHtml: string;
};

// サービス名クリックで開く、サービス・アプリケーション情報ページ。
// サービス名／ヘッダ画像／サーバ概要（Markdown）／アプリケーション情報（スクロール枠）を表示する。
export default function ServiceInfoPage() {
  const navigate = useNavigate();
  const [info, setInfo] = useState<ServiceInfo | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    getServiceInfo()
      .then((res) => {
        if (active) {
          setInfo(res);
        }
      })
      .catch((err) => {
        if (active) {
          setError(err instanceof Error ? err.message : "サービス情報を取得できませんでした");
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  // OKボタン（＝閉じる）: 履歴があれば戻り、無ければトップへ。
  function close() {
    if (window.history.length > 1) {
      navigate(-1);
    } else {
      navigate("/");
    }
  }

  return (
    <section className="serviceInfoPage">
      {loading ? (
        <p className="muted">読み込み中...</p>
      ) : error ? (
        <p className="formError">{error}</p>
      ) : info ? (
        <>
          <h1 className="serviceInfoName">{info.serviceName}</h1>

          {info.headerImageUrl ? (
            <img
              className="serviceInfoHeader"
              src={info.headerImageUrl}
              alt={`${info.serviceName} のヘッダ画像`}
            />
          ) : null}

          {info.overviewHtml ? (
            <section className="serviceInfoSection">
              <h2>サーバ概要</h2>
              <div
                className="markdownBody"
                // サーバ側で整形済みの信頼済みHTML（管理者が設定）。
                dangerouslySetInnerHTML={{ __html: info.overviewHtml }}
              />
            </section>
          ) : null}

          <section className="serviceInfoSection">
            <h2>アプリケーション情報</h2>
            <div className="serviceInfoScrollBox">
              <div
                className="markdownBody"
                // ソース埋め込みの信頼済みHTML。
                dangerouslySetInnerHTML={{ __html: info.appInfoHtml }}
              />
            </div>
          </section>
        </>
      ) : null}

      <div className="serviceInfoActions">
        <button type="button" className="serviceInfoOkButton" onClick={close}>
          OK
        </button>
      </div>
    </section>
  );
}
