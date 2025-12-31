# VRClog Companion

VRChat のローカルログを監視し、Join/Leave/World 移動イベントを SQLite に保存するローカル常駐アプリケーション。

## 機能（v1 予定）

- VRChat ログ監視（tail）
- イベント永続化（SQLite）
- HTTP API + Web UI
- Discord 通知（Webhook）

詳細は [SPEC.md](./SPEC.md) を参照。

## 開発環境

- Go 1.22+
- Windows 11（ターゲット OS）

## ディレクトリ構成

```
vrclog-companion/
├── cmd/
│   └── vrclog/          # メインエントリポイント
│       └── main.go
├── internal/
│   ├── api/             # HTTP API サーバー
│   │   ├── server.go
│   │   └── server_test.go
│   └── config/          # 設定管理（将来実装）
├── web/                 # Web UI（将来実装）
├── docs/
│   └── IMPLEMENTATION_PLAN.md  # 実装計画
├── .github/
│   └── workflows/
│       └── ci.yml       # GitHub Actions CI
├── go.mod
├── SPEC.md              # 仕様書
└── README.md
```

## ビルド・実行

### ビルド

```fish
# クロスコンパイル（macOS/Linux から Windows 向け）
set -x GOOS windows
set -x GOARCH amd64
go build -o vrclog.exe ./cmd/vrclog

# ローカル環境向け
go build -o vrclog ./cmd/vrclog
```

### 実行

```fish
# デフォルト（ポート 8080）
./vrclog

# ポート指定
./vrclog -port 9000
```

### 動作確認

```fish
# サーバー起動後
curl http://127.0.0.1:8080/api/v1/health
# {"status":"ok","version":"0.1.0"}
```

## テスト

```fish
go test ./...
```

## CI

GitHub Actions で Windows runner 上のテストを自動実行。

- `push` / `pull_request` でトリガー
- Windows + Linux でテスト
- Windows でビルド確認

## 変更ファイル一覧（初期コミット）

```
.github/workflows/ci.yml      # CI 設定
cmd/vrclog/main.go            # エントリポイント
docs/IMPLEMENTATION_PLAN.md   # 実装計画
internal/api/server.go        # HTTP サーバー
internal/api/server_test.go   # テスト
go.mod                        # Go モジュール定義
README.md                     # このファイル
SPEC.md                       # 仕様書（既存）
```

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
- ブラウザの `EventSource` API は Basic認証ヘッダーを送信できないため、SSE ストリーム (`/api/v1/stream`) へのブラウザからの直接アクセスは制限されます

## ライセンス

TBD
