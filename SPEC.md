# VRClog Companion 仕様書

## 0. プロジェクト識別子（命名の確定）

* **GitHubリポジトリ名**：`vrclog-companion`
* **配布バイナリ（Windows）**：`vrclog.exe`
* **プロセス名（Windows）**：`vrclog.exe`
* **アプリ表示名（UI/README）**：VRClog Companion（短縮：VRClog）

---

## 1. 概要

### 1.1 目的

VRChat のローカルログを監視し、Join/Leave/World移動等のイベントを抽出して **ユーザーPC内のSQLiteに永続化**する。
ユーザーは **ローカルHTTP API + Web UI（ブラウザ）** で履歴・現在状態・簡易統計を閲覧できる。
イベント発生時には **Discord Webhook** を用いてスマホ等へ通知を届ける。

### 1.2 基本方針（最重要）

* **中央DB／開発者サーバー無し**（開発者はログもトークンも保持しない）
* データは **ユーザーPC内にのみ保存**
* v1（本仕様）で **LANアクセス** を提供するが、**安全側デフォルト**で意図しない公開を防ぐ
* UIは **ブラウザのみ**（Tauri/Electron等は対象外）
* ライブ更新は **SSE**（必要になるまでこれで十分）
* LINE Notify はサービス終了のため対象外

### 1.3 対応OS

* Windows 11（v1）

---

## 2. スコープ

### 2.1 v1で実現すること

* VRChatログ監視（tail）
* Join/Leave/World移動のイベント化（`vrclog-go` を利用）
* SQLiteへ履歴保存
* 重複排除（再起動・巻き戻り・ローテーション耐性）
* 二重起動防止（単一インスタンス）
* HTTP API提供（履歴／現在状態／簡易統計／SSE）
* Web UI（SPAを同一サーバーから配布）
* LANアクセス（設定ON時）
* Discord通知（Webhook）

### 2.2 v1で実施しないこと

* 開発者運用のサーバー、クラウド同期、中央DB
* LAN外アクセスの公式サポート（VPN/ポート開放等は自己責任）
* Web Push（HTTPS要件等が重いため）
* ネイティブGUI（Tauri/Electron）
* LINE通知（Notify終了／Messaging API等はv2以降検討）

---

## 3. 用語

* **Companion**：ユーザーPC上で常駐する単一実行体（`vrclog.exe`）。
* **Event**：ログから抽出した構造化イベント。
* **Cursor**：ログ取り込み位置（どこまで処理したか）。
* **Dedupe Key**：重複排除のための一意キー。
* **Derive**：イベントから計算する現在状態（今のワールド、同席者）。

---

## 4. ユースケース

### 4.1 主要ユースケース

1. VRChatプレイ中、同じインスタンスに人がJoinしたらDiscord通知が来る
2. 後から「いつ誰と会ったか」「どのワールドにいたか」をWeb UIで見返す
3. 同一LANのスマホから `http://<PC_IP>:<port>` を開き履歴を見る

---

## 5. 全体アーキテクチャ

### 5.1 コンポーネント

* **Ingest**：`vrclog-go`でログ監視→Event受信
* **Persist**：SQLiteへ保存（events）
* **Cursor**：再起動耐性（ingest_cursor）
* **Dedupe**：UNIQUE(dedupe_key)＋衝突時無視
* **Derive**：現在状態トラッカー（メモリ）
* **Notify**：Discord Webhook
* **API**：HTTP API（JSON + SSE）
* **UI**：静的配信（SPA、ブラウザ）

### 5.2 データフロー

1. VRChatログ → `vrclog-go` → Event受信
2. `dedupe_key` 生成
3. `INSERT ... ON CONFLICT DO NOTHING`（新規のみ保存）
4. INSERT成功時のみ：Derive更新 / Discord通知 / SSE配信

---

## 6. 機能要件

## 6.1 ログ監視・イベント生成

* `vrclog-go` を利用し、ログを tail する
* イベント種別（v1）

  * `player_join`
  * `player_left`
  * `world_join`
* ログパス

  * 自動検出（デフォルト）
  * 手動指定（設定）
* 起動時リプレイ（取りこぼし対策）

  * DBの最終イベント時刻を取得し、**安全窓（例：5分）巻き戻して**リプレイする
  * 既存イベントはDedupeで無害化する（再起動で増殖しない）

## 6.2 SQLite永続化

* DBファイル：`vrclog.sqlite`
* DBは履歴のソース・オブ・トゥルース
* SQLite初期化時の推奨設定

  * WALモード
  * busy_timeout
  * 書き込みは短いトランザクション

## 6.3 重複排除・二重通知抑止

* `events.dedupe_key` に `UNIQUE` 制約
* INSERT成功（新規）時のみ通知とSSEを実行
* 二重起動を禁止（単一インスタンス）

## 6.4 HTTP API

* prefix：`/api/v1`
* `health` / `now` / `events` / `stats/basic` / `stream(SSE)` / `auth/token` / `config`

## 6.5 Web UI（ブラウザのみ）

* `/` でSPA配信
* 画面（v1）

  * Now：現在状態＋直近イベント（SSE）
  * History：履歴一覧（期間／種別フィルタ）
  * Stats：簡易統計
  * Settings：ログパス、LAN公開、認証、Discord、通知設定

## 6.6 Discord通知

* ユーザーがDiscord側でWebhook URLを作成し、設定に貼り付ける必要がある
* 通知トリガー（v1）

  * Join/Leave/World移動
* バッチ化（スパム抑止）

  * デフォルト 3 秒（設定可能）
* 失敗時

  * 429：バックオフ再送
  * 401/403：設定不備としてUIに表示し通知停止

---

## 7. セキュリティ要件

## 7.1 バインド

* デフォルト：`127.0.0.1:<port>`（LAN公開OFF）
* LAN公開ON：`0.0.0.0:<port>`

## 7.2 認証

* LAN公開ON時：**HTTP Basic Auth必須**

  * 初回ON時に強いランダムパスワードを生成し保存
  * UI上で表示（ユーザーが変更可能）
* localhostのみ：認証は任意

## 7.3 注意喚起（README/UIに明記）

* TLSなしBasic認証は盗聴耐性がないため、LAN内限定で運用すること
* ポート開放・インターネット公開は非推奨、行う場合は自己責任（サポート外）

## 7.4 CORS

* 同一オリジン前提で最小化
* LAN公開時でも許可Originを限定（保守的）

---

## 8. 設定管理仕様

## 8.1 分類

* config（非機密）

  * port, lan_enabled, log_path, ui設定, バッチ秒数等
* secrets（機密）

  * discord_webhook_url
  * basic_auth_password（ユーザー名も必要なら）

## 8.2 保存場所（Windows）

`%LOCALAPPDATA%/vrclog/`

* `config.json`
* `secrets.dat`（または `secrets.json`、推奨は暗号化領域）
* `vrclog.sqlite`
* `logs/`

## 8.3 書き込み要件

* atomic write（tmp→rename）
* schema_version
* 機密情報はログに出さない（マスク）

## 8.4 secrets保護

* v1最低限：ファイル権限（同一ユーザーのみ）
* 可能なら：Windows保護APIで暗号化（v1またはv1.1）

## 8.5 安全状態の強制

* lan_enabled を true にする操作では

  * Basic Auth を強制ON
  * パスワード未設定なら生成
  * 警告表示
    を必ず実行する

---

## 9. DB設計（SQLite）

## 9.1 `events`（必須）

| 列              | 型          | 説明            |
| -------------- | ---------- | ------------- |
| id             | INTEGER PK | 単調増加ID        |
| ts             | TEXT       | イベント時刻（UTC推奨） |
| type           | TEXT       | イベント種別        |
| player_name    | TEXT NULL  | プレイヤー名        |
| player_id      | TEXT NULL  | 取得できる場合       |
| world_id       | TEXT NULL  |               |
| world_name     | TEXT NULL  |               |
| instance_id    | TEXT NULL  |               |
| meta_json      | TEXT NULL  | 拡張JSON        |
| dedupe_key     | TEXT       | 重複排除キー        |
| ingested_at    | TEXT       | 取り込み時刻        |
| schema_version | INTEGER    | v1=1          |

制約：

* `UNIQUE(dedupe_key)`

推奨インデックス：

* `(ts)`
* `(type, ts)`
* `(player_name, ts)`（任意）
* `(world_id, instance_id, ts)`（任意）

## 9.2 `ingest_cursor`（必須）

| 列               | 型          | 説明                  |
| --------------- | ---------- | ------------------- |
| id              | INTEGER PK |                     |
| source_path     | TEXT       | 追跡ログファイル            |
| source_identity | TEXT       | パス+mtime+size等のハッシュ |
| byte_offset     | INTEGER    | 最後に処理した位置           |
| updated_at      | TEXT       |                     |

※ v1は「リプレイ＋Dedupe」で成立するため、byte_offsetが未実装でも致命ではないが、将来の精度向上のためにテーブルは用意する。

## 9.3 `parse_failures`（推奨）

| 列          | 型          | 説明 |
| ---------- | ---------- | -- |
| id         | INTEGER PK |    |
| ts_guess   | TEXT NULL  |    |
| line       | TEXT       | 生行 |
| reason     | TEXT       |    |
| log_file   | TEXT       |    |
| created_at | TEXT       |    |

---

## 10. 重複排除仕様（詳細）

### 10.1 dedupe_key の基本

* 生行（raw_line）を保存せず、ハッシュのみ利用する
* v1の現実解（推奨）：

  * `dedupe_key = SHA256(raw_line)`
* 将来拡張（任意）：

  * `SHA256(source_identity + ":" + offset + ":" + type + ":" + SHA256(raw_line))`

### 10.2 重複時の動作

* DBに挿入されなかった（衝突）イベントは

  * Discord通知しない
  * SSE配信しない
  * Derive更新しない（または更新しない方を推奨）

---

## 11. 派生状態（Derive）仕様

* `world_join`：現在ワールド更新、同席者集合をリセット（v1）
* `player_join`：集合に追加
* `player_left`：集合から削除
* `last_event_id`：最後にINSERTされたevents.id

---

## 12. HTTP API仕様（v1）

### 12.1 `GET /api/v1/health`

```json
{ "status": "ok", "version": "0.1.0" }
```

### 12.2 `GET /api/v1/now`

現在のワールドとオンラインプレイヤーを返す。

```json
{
  "world": {
    "world_id": "wrld_...",
    "world_name": "Example World",
    "instance_id": "12345~private(usr_...)~region(jp)",
    "joined_at": "2025-01-01T12:00:00.000000000Z"
  },
  "players": [
    {
      "player_name": "Alice",
      "player_id": "usr_...",
      "joined_at": "2025-01-01T12:05:00.000000000Z"
    }
  ]
}
```

* `world` はワールド未参加時は `null`
* `players` はワールド未参加時は空配列

### 12.3 `GET /api/v1/events`

* クエリ：`since`, `until`, `type`, `limit`, `cursor`
* レスポンス：

```json
{ "items": [ ... ], "next_cursor": "..." }
```

### 12.4 `GET /api/v1/stats/basic`

* 今日のJoin数、直近の人、ワールド遷移回数（簡易）

### 12.5 `GET /api/v1/stream`（SSE）

* `id:` はカーソル形式（base64エンコード、`ts|id`）
* `event:` はイベント種別（`player_join`, `player_left`, `world_join`）
* `data:` はイベントJSON
* 切断時に購読解除
* `Last-Event-ID` ヘッダまたは `last_event_id` クエリパラメータでの再接続リプレイ対応
* ハートビート: 20秒間隔でコメント送信

認証（LAN公開時）:
* Basic認証ヘッダ、または
* `?token=...` クエリパラメータ（`POST /api/v1/auth/token` で発行）
* ブラウザの `EventSource` API は Basic認証ヘッダを送信できないため、トークン認証を使用

### 12.6 `POST /api/v1/auth/token`

SSE接続用の一時トークンを発行する（LAN公開時のみ）。

```json
{
  "token": "base64_encoded_token",
  "expires_in": 300
}
```

* Basic認証が必要
* トークンはSSE接続時のクエリパラメータ `?token=...` で使用
* 有効期限: 5分

### 12.7 `GET /api/v1/config`

設定情報を取得する（シークレットは除外）。

```json
{
  "port": 8080,
  "lan_enabled": false,
  "log_path": "",
  "discord_batch_sec": 3,
  "notify_on_join": true,
  "notify_on_leave": true,
  "notify_on_world_join": true
}
```

### 12.8 `PUT /api/v1/config`

設定を更新する。

* リクエストボディ: 更新する設定項目のJSON
* レスポンス: `{ "success": true, "restart_required": false }`
* `restart_required: true` の場合、ポート変更等で再起動が必要

---

## 13. Web UI仕様（v1）

* Now / History / Stats / Settings
* レスポンシブ必須
* LAN公開ON時はBasic認証を通してアクセスする
* PWAは任意（Pushはv1対象外）

---

## 14. 配布・導入（v1）

### 14.1 配布

* GitHub Releases に `vrclog.exe` を提供
* ソースはOSS

### 14.2 初回チュートリアル（必須）

* ログパス確認（自動検出）
* Discord Webhook設定
* LAN公開（OFF推奨、ON時は認証必須・警告表示）
* Start/Stop

### 14.3 自動起動

* デフォルトOFF
* 設定からON可能（チュートリアルで説明）

---

## 15. 完了定義（v1）

* 2重起動不可
* 再起動してもイベントが増殖しない（dedupe）
* LAN OFFでは外部からアクセス不可
* LAN ONではBasic認証なしでAPI/UIにアクセス不可
* 新規イベントのみDiscord通知
* SSEでUIがリアルタイム更新
* DBがWAL設定で運用可能

---

## 付録：推奨リポジトリ構造

* `cmd/vrclog/`（ビルド成果物は vrclog.exe）
* `internal/store/`（SQLite）
* `internal/ingest/`（vrclog-go）
* `internal/derive/`（Now状態）
* `internal/notify/`（Discord）
* `internal/api/`（HTTP+SSE+Auth）
* `web/`（SPA、ビルド成果物をembed）

