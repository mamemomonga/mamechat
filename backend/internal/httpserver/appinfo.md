<!--
このファイルはサービス情報ページの「アプリケーション情報」として表示される内容です。
ソースコードに埋め込まれ（go:embed）、Markdown で自由に編集できます。
`{{VERSION}}` は起動中サーバのバージョンに置換されます（この項目だけはソース側で差し込みます）。
編集後は再ビルド・再デプロイで反映されます。
-->

## アプリケーション情報

**mamechat** は、自分たちで運用できるオープンソースの WebSocket チャットです。

### バージョン

- サーバーバージョン: `{{VERSION}}`

### リポジトリ

- 公開リポジトリ: [github.com/mamemomonga/mamechat](https://github.com/mamemomonga/mamechat)
- ライセンスや貢献方法はリポジトリを参照してください。

### 使用している主なソフトウェア

- **バックエンド**: Go / PostgreSQL / Valkey
- **フロントエンド**: React / TypeScript / Vite
- **音声合成**: [VOICEVOX](https://voicevox.hiroshiba.jp/)

### VOICEVOX の利用について

本サービスの読み上げ機能は **VOICEVOX** を利用しています。

- VOICEVOX（© Hiroshiba Kazuyuki）は、各音声ライブラリの利用規約に従って利用しています。
- 各キャラクターの音声を利用する際は、キャラクターごとの利用規約・クレジット表記の条件に従ってください。
- 合成音声の利用にあたっては、VOICEVOX 公式サイトおよび各音声ライブラリの規約をご確認ください。

詳しくは [VOICEVOX 公式サイト](https://voicevox.hiroshiba.jp/) をご覧ください。

### 免責事項

本サービスは現状有姿で提供され、利用によって生じたいかなる損害についても運営者は責任を負いません。
