# VRClog Companion

VRChat のローカルログを監視し、Join/Leave/World 移動イベントを SQLite に保存するローカル常駐アプリケーション。

## 機能

- VRChat ログ監視（tail）
- イベント永続化（SQLite）
- HTTP API + Web UI
- Discord 通知（Webhook）
- SSE によるリアルタイム更新

詳細は [SPEC.md](./SPEC.md) を参照。

## 開発環境

- Go 1.22+
- Node.js 18+ (Web UI ビルド用)
- Windows 11（ターゲット OS）

## ディレクトリ構成

```
vrclog-companion/
├── cmd/
│   └── vrclog/          # メインエントリポイント
├── internal/
│   ├── api/             # HTTP API サーバー
│   ├── app/             # ユースケース層
│   ├── config/          # 設定管理
│   ├── derive/          # 派生状態（メモリ内追跡）
│   ├── event/           # イベントモデル
│   ├── ingest/          # ログ監視・取り込み
│   ├── notify/          # Discord 通知
│   └── store/           # SQLite 永続化
├── web/                 # Web UI (React + Vite)
├── webembed/            # 埋め込み用 Web UI
├── .github/
│   └── workflows/
│       └── ci.yml       # GitHub Actions CI
├── go.mod
├── SPEC.md              # 仕様書
├── LICENSE              # MIT ライセンス
└── README.md
```

## ビルド・実行

### ビルド

```bash
# Web UI ビルド
cd web && npm install && npm run build && cd ..
cp -r web/dist/* webembed/

# クロスコンパイル（macOS/Linux から Windows 向け）
GOOS=windows GOARCH=amd64 go build -o vrclog.exe ./cmd/vrclog

# ローカル環境向け
go build -o vrclog ./cmd/vrclog
```

### 実行

```bash
# デフォルト（ポート 8080）
./vrclog

# ポート指定
./vrclog -port 9000
```

### 動作確認

```bash
# サーバー起動後
curl http://127.0.0.1:8080/api/v1/health
# {"status":"ok","version":"0.1.0"}

# Web UI
# ブラウザで http://127.0.0.1:8080 を開く
```

## テスト

```bash
go test ./...
```

## CI

GitHub Actions で Windows runner 上のテストを自動実行。

- `push` / `pull_request` でトリガー
- Windows + Linux でテスト
- Windows でビルド確認

## セキュリティ

### LAN モード

`config.json` で `lan_enabled=true` を設定すると、ローカルネットワーク上の他のデバイスからアクセス可能になります。

- **Basic認証は必須**: LAN モード有効時、Basic認証が自動的に有効化されます
- **初回起動時にパスワード自動生成**: 認証情報未設定の場合、強力なランダムパスワードが生成されログに表示されます

### 注意事項

> **警告**: Basic認証はTLSなしでは盗聴に対する保護がありません。
> 信頼できるローカルネットワーク内でのみ使用してください。

- ポート開放（ポートフォワーディング）は**非推奨**です
- インターネットへの公開はサポート対象外です
- 認証情報は `secrets.json` に保存されます
- ブラウザの `EventSource` API は Basic認証ヘッダーを送信できないため、SSE ストリーム (`/api/v1/stream`) へのブラウザからの直接アクセスにはトークン認証を使用します

## ライセンス

MIT License - 詳細は [LICENSE](./LICENSE) を参照。
