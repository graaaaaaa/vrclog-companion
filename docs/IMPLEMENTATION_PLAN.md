# VRClog Companion 実装計画

## 概要

VRChat のローカルログを監視し、イベントを SQLite に永続化するローカル常駐アプリ。
HTTP API + Web UI で履歴閲覧、Discord Webhook で通知を提供する。

---

## マイルストーン

### Milestone 0: プロジェクト基盤（本PR）

**目標**: ビルド可能な骨組み + CI

- [x] ディレクトリ構成（cmd/, internal/, web/, docs/）
- [x] go.mod 初期化（Go 1.22）
- [x] 最小 HTTP サーバー（`/api/v1/health` のみ）
- [x] GitHub Actions CI（Windows runner + `go test ./...`）
- [x] README に実行方法記載

**完了条件**: `go test ./...` が Windows CI で通る

---

### Milestone 1: 設定管理 + データディレクトリ

**目標**: 設定ファイルの読み書き基盤

- [ ] `%LOCALAPPDATA%/vrclog/` ディレクトリ管理
- [ ] `config.json` 読み書き（atomic write）
- [ ] schema_version 対応
- [ ] 設定構造体定義

**PR分割案**:
- PR1-1: 設定ディレクトリ・パス解決
- PR1-2: config.json 読み書き

---

### Milestone 2: SQLite 永続化

**目標**: イベント保存基盤

- [ ] SQLite 初期化（WAL, busy_timeout）
- [ ] `events` テーブル作成
- [ ] `ingest_cursor` テーブル作成
- [ ] マイグレーション基盤
- [ ] 重複排除（dedupe_key UNIQUE）

**PR分割案**:
- PR2-1: SQLite 接続・初期化
- PR2-2: events テーブル + CRUD
- PR2-3: ingest_cursor テーブル

---

### Milestone 3: ログ監視 (Ingest)

**目標**: VRChat ログからイベント抽出

- [ ] `vrclog-go` ライブラリ統合
- [ ] ログパス自動検出
- [ ] tail 監視
- [ ] イベント生成（player_join, player_left, world_join）
- [ ] dedupe_key 生成（SHA256）

**PR分割案**:
- PR3-1: vrclog-go 統合・基本イベント受信
- PR3-2: SQLite 保存連携
- PR3-3: 起動時リプレイ

---

### Milestone 4: 派生状態 (Derive)

**目標**: 現在状態のメモリ管理

- [ ] 現在ワールド追跡
- [ ] 同席プレイヤー集合管理
- [ ] last_event_id 管理

**PR分割案**:
- PR4-1: Derive 構造体 + 更新ロジック

---

### Milestone 5: HTTP API 拡張

**目標**: フル API 実装

- [ ] `GET /api/v1/state`
- [ ] `GET /api/v1/events`（クエリ対応）
- [ ] `GET /api/v1/stats/basic`
- [ ] `GET /api/v1/stream`（SSE）

**PR分割案**:
- PR5-1: state エンドポイント
- PR5-2: events エンドポイント
- PR5-3: stats エンドポイント
- PR5-4: SSE stream

---

### Milestone 6: Discord 通知

**目標**: Webhook 通知実装

- [ ] Discord Webhook 送信
- [ ] バッチ化（デフォルト 3 秒）
- [ ] エラーハンドリング（429, 401/403）
- [ ] secrets.dat 保存

**PR分割案**:
- PR6-1: secrets 管理
- PR6-2: Discord Webhook 基本送信
- PR6-3: バッチ化・リトライ

---

### Milestone 7: セキュリティ

**目標**: LAN 公開時の認証

- [ ] バインドアドレス切り替え（127.0.0.1 / 0.0.0.0）
- [ ] HTTP Basic Auth
- [ ] パスワード自動生成
- [ ] CORS 設定

**PR分割案**:
- PR7-1: バインド切り替え
- PR7-2: Basic Auth ミドルウェア

---

### Milestone 8: Web UI

**目標**: SPA 配信

- [ ] 静的ファイル embed
- [ ] Now 画面
- [ ] History 画面
- [ ] Stats 画面
- [ ] Settings 画面

**PR分割案**:
- PR8-1: 静的配信基盤
- PR8-2〜: 各画面実装

---

### Milestone 9: 仕上げ

**目標**: 配布準備

- [ ] 二重起動防止
- [ ] 自動起動設定
- [ ] goreleaser 設定
- [ ] README / チュートリアル

---

## 技術選定

| 項目 | 選定 | 理由 |
|------|------|------|
| 言語 | Go 1.22 | クロスコンパイル容易、シングルバイナリ |
| HTTP | net/http (+ chi 検討) | 標準で十分、必要に応じて chi 追加 |
| DB | SQLite (mattn/go-sqlite3) | ローカル完結、WAL 対応 |
| ログ解析 | vrclog-go | 既存ライブラリ活用 |
| フロント | 未定 (React/Svelte/Vanilla) | Milestone 8 で決定 |

---

## PR ルール

1. 各 PR は小さく保つ（レビュー容易性）
2. `go test ./...` が通ること
3. Windows CI で動作確認
4. 機密情報をログに出さない
