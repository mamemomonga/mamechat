# mamechat

セルフホスト可能なチャットMVPです。現在の対象は、PostgreSQLへの永続化とValkey Pub/Sub配信を備えたWebSocketチャットです。ルームには安定したslugを持たせています。

## 技術スタック

- フロントエンド: React, Vite, TypeScript, React Router
- バックエンド: Go, net/http, github.com/coder/websocket, pgx, sqlc形式のクエリ, go-redis
- データベース: PostgreSQL
- リアルタイム配信: Redis互換プロトコル経由のValkey Pub/Sub
- サーバサイドTTS: VOICEVOX ENGINE, ffmpeg, M4A/AAC-LC

## クイックスタート

クローンとビルド

```sh
$ git clone git@github.com:mamemomonga/mamechat.git
$ cd mamechat
$ cp .env.example .env
$ vim .env
OWNER_PASSWORD=hogehoge
```

makeの利用方法表示

```sh
$ make
```

ビルドと起動

```bash
$ make prod-up prod-logs
```

* 管理者ログイン: http://localhost:8080/admin/login
* ローカル: http://localhost:8080

クリーンアップ

```sh
$ make clean-volumes
```

## Dockerでの起動

```sh
cp .env.example .env
make dev-up
```

`.env` に `OWNER_PASSWORD` を設定してから、http://localhost:5173 を開きます。

開発用スタックではVite dev serverを使います。PostgreSQLは `localhost:5432`、Valkeyは `localhost:6379`、バックエンドは `localhost:8080` に公開されます。VOICEVOX ENGINEはCompose内部ネットワークだけで使い、ホストへは公開しません。

TTS workerはデフォルトで2並列です。Composeでは `voicevox` と `voicevox-2` の2つのVOICEVOX ENGINEを起動し、`TTS_VOICEVOX_URLS` の順にworker loopへ割り当てます。並列数を変える場合は `.env` の `TTS_WORKER_CONCURRENCY` を変更してください。

ログインページは `/login` です。ローカルユーザーとAdminユーザーはhandleとパスワードでログインできます。Ownerユーザーは直接アクセス専用の `/admin/login` で、`OWNER_PASSWORD` を使ってログインします。

Bluesky / AT Protocolログインも `/login` から開始できます。OAuthクライアントメタデータは `/oauth/atproto/client-metadata.json` で公開します。実際にBluesky OAuthを使う場合は、`ATPROTO_PUBLIC_BASE_URL` を外部から到達できるHTTPS URLに設定してください。ローカルの `http://localhost:8080` は開発時の構成確認用です。

ユーザー用ページは `/settings` です。アカウント情報を参照し、自分のポストを読み上げるVOICEVOXキャラクターを変更できます。「使用しない」を選ぶと、そのユーザーの投稿はリスナー側で読み上げをONにしていてもTTS生成されません。旧 `/account` は `/settings` へリダイレクトします。

管理ページは `/admin` です。OwnerまたはAdmin権限を持つユーザーでログインしている場合だけ利用できます。

本番相当のスタックでは、フロントエンドを静的なHTML/JS/CSSへビルドし、nginxで配信します。

```sh
make prod-up
```

http://localhost:8080 を開きます。

本番用スタックでは、PostgreSQL、Valkey、バックエンドのポートはホストへ公開されません。ホストへ公開されるのはnginxだけです。nginxは内部Composeネットワーク経由で `/api`、`/healthz`、`/ws` をバックエンドへproxyします。

バックエンドとTTS workerは起動時に `backend/migrations/` 以下のSQLをファイル名順に適用します。

`OWNER_PASSWORD` にデフォルト値はありません。未設定の場合、バックエンドは起動しません。Ownerパスワードはバックエンドの環境変数からのみ読み込まれ、PostgreSQLには保存されません。Adminユーザーのパスワードは通常のローカルユーザーと同じく、bcryptハッシュとしてPostgreSQLに保存されます。

サービス名は `SERVICE_NAME` で変更できます。未指定時は `mamechat` です。画面上部のブランド表示・ページタイトル、および各SNSのOAuthログイン時にアプリ名として表示されます。

## アクセスログ（コンプライアンス）

cloudflaredでトンネルする構成では、nginxが見る接続元はcloudflared側の内部IPになるため、実クライアントIPはCloudflareが付与する `CF-Connecting-IP` ヘッダから取得します。nginxの `realip` モジュールで実IPを復元し、JSON形式（1行1レコード）で `/var/log/nginx/access.log` に記録します。ログはホストへバインドマウントし、ホスト側の `logrotate` で保全します。

なりすまし防止のため、本番の公開ポートは `127.0.0.1` バインドに限定しています（`docker-compose.prod.yml`）。ホスト上のcloudflaredは `localhost:${PROD_HTTP_PORT:-8080}` 経由で到達でき、外部からは直接叩けません。`CF-Connecting-IP` を信頼できるのは、この「トンネル経由のみ到達可能」という前提が満たされている場合だけです。

ログの保存先はホストの `./logs/nginx`（`NGINX_LOG_DIR` で変更可）です。保持期間などは `deploy/logrotate/nginx-mamechat` を編集し、ホストの `/etc/logrotate.d/` に設置して運用します（既定は日次ローテート・365世代・圧縮保存）。IPアドレスは個人情報に該当し得るため、保持期間の明文化とアクセス制御をあわせて定めてください。

## Makefileタスク

よく使う操作は `Makefile` に集約しています。

```sh
make help
make dev-up
make dev-down
make dev-logs
make prod-up
make prod-down
make prod-logs
make backend-test
make frontend-build
make db-shell
make dev-db-dump
make dev-db-restore DB_RESTORE_FILE=backups/app.dump
make prod-db-dump
make prod-db-restore DB_RESTORE_FILE=backups/app.dump
make valkey-cli
```

## PostgreSQLのdump/restore

dump/restoreの対象はPostgreSQLのみです。Valkeyのデータは対象にしません。

開発用PostgreSQLのdump:

```sh
make dev-db-dump
```

dump先を指定する場合:

```sh
make dev-db-dump DB_DUMP_FILE=backups/app.dump
```

開発用PostgreSQLへのrestore:

```sh
make dev-db-restore DB_RESTORE_FILE=backups/app.dump
```

本番用スタックのPostgreSQLも同じ形式で操作できます。

```sh
make prod-db-dump
make prod-db-restore DB_RESTORE_FILE=backups/app.dump
```

restoreは `pg_restore --clean --if-exists --no-owner` で実行します。既存DBオブジェクトを削除してから復元するため、実行前に対象スタックとdumpファイルを確認してください。

## 開発

バックエンド:

```sh
cd backend
go mod download
go run ./cmd/server
```

フロントエンド:

```sh
cd frontend
npm install
npm run dev
```

Composeを使わずにローカルでバックエンドを起動する場合は、以下の環境変数を設定します。

```sh
export DATABASE_URL='postgres://app:app@localhost:5432/app?sslmode=disable'
export REDIS_URL='redis://localhost:6379/0'
export CORS_ALLOWED_ORIGIN='http://localhost:5173'
export OWNER_PASSWORD='change-me'
export ATPROTO_PUBLIC_BASE_URL='https://example.com'
export ATPROTO_LOGIN_REDIRECT_URL='https://example.com'
export TTS_VOICEVOX_URLS='http://localhost:50021'
export TTS_WORKER_CONCURRENCY=1
export TTS_STORAGE_DIR='/tmp/mamechat-tts'
```

ローカルでTTS workerも起動する場合は、別ターミナルでVOICEVOX ENGINEとffmpegを用意したうえで以下を実行します。

```sh
cd backend
go run ./cmd/tts-worker
```

## データベースとsqlc

初期マイグレーションは以下です。

```sh
backend/migrations/000001_init.sql
```

クエリ定義は以下に置いています。

```sh
backend/internal/store/queries/
```

ローカルにsqlcがなくてもビルドできるように、生成済み相当のGoコードを `backend/internal/generated/db/` にコミットしています。sqlcで再生成する場合は以下を実行します。

```sh
cd backend
sqlc generate
```

## API

- `GET /healthz`
- `GET /oauth/atproto/client-metadata.json`
- `POST /api/auth/atproto/start`
- `GET /api/auth/atproto/callback`
- `POST /api/auth/mastodon/start`
- `GET /api/auth/mastodon/callback`
- `POST /api/auth/misskey/start`
- `GET /api/auth/misskey/callback`
- `POST /api/owner/login`
- `GET /api/admin/stats`
- `GET /api/admin/sessions`
- `POST /api/admin/sessions/{sessionID}/revoke`
- `GET /api/admin/users`
- `PUT /api/admin/users/{userID}`
- `DELETE /api/admin/users/{userID}`
- `POST /api/admin/channels`
- `DELETE /api/admin/channels/{slug}`
- `PUT /api/admin/channels/{slug}/suspend`
- `PUT /api/admin/channels/{slug}/grace`
- `DELETE /api/admin/messages/{messageID}`
- `POST /api/logout`
- `GET /api/me`
- `PUT /api/account`
- `GET /api/settings/voicevox-speakers`
- `PUT /api/settings/voicevox-speaker`
- `GET /api/channels`
- `POST /api/channels`
- `GET /api/channels/{slug}`
- `DELETE /api/channels/{slug}`
- `GET /api/channels/{slug}/messages`
- `DELETE /api/channels/{slug}/messages`
- `GET /api/tts/{contentHash}.m4a`
- `GET /ws/channels/{slug}`

Ownerログインでは、送信されたパスワードを `OWNER_PASSWORD` と照合します。成功した場合は単一のOwnerユーザーを作成または再利用し、Cookieセッションを発行します。生のセッショントークンはブラウザCookieにのみ保存され、PostgreSQLにはSHA-256ハッシュだけを保存します。OwnerパスワードはPostgreSQLに保存しません。

ローカルユーザーはhandleとパスワードでログインします。ローカルユーザーのパスワードはbcryptハッシュとしてPostgreSQLに保存し、平文では保存しません。

Bluesky / AT ProtocolログインではOAuthを本人確認のみに使います。Callbackで返るtoken responseの `sub` をDIDとして扱い、`auth_identities.provider='atproto'`、`auth_identities.subject='did:...'` に保存します。アクセストークンやリフレッシュトークンは永続化しません。プロフィールは `https://public.api.bsky.app` のpublic APIから取得し、handle、表示名、アバターURLを `users` と `auth_identities` に反映します。バックエンドは6時間ごとに、最終同期から24時間以上経過したatproto identityを再同期します。自動投稿は行いません。

管理ページでは、全体統計、アクティブなブラウザセッションの参照と失効、ユーザー情報の変更、Admin権限の付与と剥奪、ユーザー削除、チャンネルの作成と削除ができます。Admin APIはすべて現在のCookieセッションを検証し、`role=owner` または `role=admin` のユーザーだけに許可します。Ownerは1サービスに1ユーザーだけで、Ownerから権限を剥奪することはできません。

## リアルタイム配信

すべてのチャットメッセージは、同じバックエンドプロセスに接続しているクライアントから送信された場合も含めて、以下の経路を通ります。

```text
Client -> WebSocket -> Go app -> PostgreSQL -> Valkey Pub/Sub -> subscribed Go apps -> local channel clients
```

これにより、バックエンドを複数プロセス化しても同じ配信経路で動作します。

ValkeyはPub/Sub、プレゼンスの揮発キー、TTSジョブキュー、TTS生成ロックに使っています。Compose構成ではValkeyにvolumeを割り当てていません。Valkeyのデータが消えても、ユーザー、セッション、チャンネル、チャット履歴、TTS生成済みファイルのメタデータの正本はPostgreSQLまたはファイルストレージにあるため、サービスは復旧できます。ただし、Valkey停止中や再起動中に流れたPub/Subメッセージや未処理TTSキューは再配信されません。投稿本文の最大長は `MESSAGE_MAX_LENGTH` で変更できます。未指定時は400文字です。

## 画像・動画アップロード

チャットに添付する静止画はJPEGとして検証したうえで、`UPLOAD_STORAGE_DIR`（既定 `/storage/uploads`、Composeでは `uploads-data` volume）に `チャンネルslug/YYYYMM/ランダム.jpg` の形式で保存します。DBの `chat_messages.image_path` には相対パスのみを記録し、`/api/uploads/{path}` で配信します（画像か動画かは拡張子で判別）。

GIFアニメ・APNGアニメ・音声なしMP4のアップロードにも対応します。これらはサーバ側の `ffmpeg` で**音声なしのH.264 MP4**（`チャンネルslug/YYYYMM/ランダム.mp4`）に変換し、再生側の `<video loop autoplay muted playsinline>` でループ表示します。判定はアップロードされたバイト列のマジックバイトで行い（クライアントの申告は信用しない）、MP4入力は `ffprobe` で「映像あり・音声なし・`UPLOAD_VIDEO_MAX_SECONDS`（既定30秒）以下」を検証してから受理します。変換時に長辺を800pxに収め、H.264制約のため寸法を偶数化します。動画素材の上限は `UPLOAD_MEDIA_MAX_BYTES`（既定20MB）です。静止PNG等は従来どおりクライアントでリサイズ・JPEG化して送ります。

アップロード直後・未投稿のファイルはValkeyに `UPLOAD_STAGING_TTL_MINUTES`（既定60分）だけステージングし、投稿時に確定します。メッセージ削除・一括削除・チャンネル削除では紐づくファイルも即削除します。投稿されなかった孤立ファイルや、サスペンド期限切れで自動削除されたチャンネルのファイルは、1時間ごとの掃除でどのメッセージからも参照されず一定時間経過したものを削除します。

## PWA（ホーム画面に追加）

`frontend/public/` の Web App Manifest（`manifest.json`、`display: standalone`、192/512/maskableアイコン）と最小限のサービスワーカー（`sw.js`、キャッシュなし）により、Android Chrome / iOS Safari でホーム画面に追加するとアドレスバーのないスタンドアロン表示で起動します（Androidのスタンドアロン化にはHTTPS配信が必要です）。`manifest.json` のアプリ名は静的（既定 `mamechat`）で、`SERVICE_NAME` とは独立しています。

## リンクプレビュー（OGP）

URLリンク化が有効なチャンネルでは、投稿本文の最初のURLについてバックエンドがOGP（Open Graph Protocol）メタ情報を取得し、フロントがプレビューカード（タイトル・説明・画像・サイト名）を表示します（`GET /api/og?url=`）。取得結果はValkeyにキャッシュします（取得成功24時間 / 失敗・OGP無し1時間）。SSRF対策として、接続先IPがループバック・プライベート・リンクローカル・CGNAT等の場合は接続を拒否し、取得はHTMLのみ・本文512KBまで・タイムアウト6秒・リダイレクト5回までに制限します。

## TTS

チャット保存後、バックエンドは読み上げ対象テキストを正規化・分割し、content hash単位でTTSジョブをValkeyキューへ投入します。TTS workerはVOICEVOX ENGINEからWAVを生成し、ffmpegで `m4a / AAC-LC / mono / 48kbps` に変換して `tts-data` volumeへ保存します。生成完了後は既存WebSocket経由で `tts_part_ready` を配信し、ブラウザが `/api/tts/{contentHash}.m4a` を取得して再生します。読み上げは投稿本文全体を対象にし、50文字を超える投稿ではポスト全体の文字数を基準に速度を上げ、`MESSAGE_MAX_LENGTH` 到達時に最速になります。

システム上のデフォルトは「使用しない」です。ユーザー設定行がまだ存在しないユーザーには、初回セッション取得時に従来どおりランダムなVOICEVOXキャラクターを割り当てます。チャット画面の「読み上げ」ボタンをONにすると、受信した音声URLを順番に再生します。

## 依存する外部ソフトウェアとクレジット

mamechatは、TTS（読み上げ）機能のために既定で以下の外部ソフトウェアに依存します。これらはmamechat本体とは別のプロセス／コンテナとして動作し、mamechatはHTTP APIまたはコマンドライン経由で呼び出します（mamechatのバイナリには静的リンクしません）。

- **[VOICEVOX ENGINE](https://github.com/VOICEVOX/voicevox_engine)** — 音声合成エンジン。Composeでは独立したコンテナ（`voicevox/voicevox_engine` イメージ）として起動し、mamechatはHTTP APIで音声を生成します。
- **[ffmpeg](https://ffmpeg.org/)** — 音声・動画の変換に使用します。TTSではVOICEVOXが生成したWAVをAAC-LC（M4A）へ、画像・動画アップロードでは各種入力をH.264 MP4へ変換します。backendコンテナ内でCLIとして別プロセス起動します。

TTSを利用しない場合（`TTS_ENABLED=false`）はVOICEVOX ENGINEを起動する必要はありません。ただし画像・動画のアップロード変換ではffmpeg（`ffmpeg` / `ffprobe`）を利用します。

### VOICEVOXキャラクターの利用規約について

VOICEVOX ENGINEで生成した音声を公開・配布する場合は、各キャラクター（音源）ごとの利用規約に従い、必要なクレジット表記（例: `VOICEVOX:ずんだもん`）を行ってください。これはmamechatのソフトウェアライセンス（MIT）とは別に、生成された音声そのものに課される条件です。詳細は [VOICEVOX 公式サイトの利用規約](https://voicevox.hiroshiba.jp/) を確認してください。運営者・利用者の責任で遵守してください。

## ライセンス

mamechat本体は [MIT License](./LICENSE) で公開しています（製作者: mamemomonga）。

VOICEVOX ENGINE（LGPL 等）およびffmpeg（LGPL / GPL）は、それぞれ独立したプロセス／コンテナとしてAPI・CLI経由で呼び出す構成のため、mamechat本体のソースコードのライセンス（MIT）には影響しません。これらのソフトウェア自体、および配布物（例: ffmpegを同梱するDockerイメージ）を再配布する場合は、各ソフトウェアのライセンス条件に従ってください。

## クレジット

- サーバアイコンのロゴデザイン: © Mitch Ikeuchi ([https://mitchikeuchi.com](https://mitchikeuchi.com))

