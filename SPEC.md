# VRClog Companion 仕様書

作成日: 2026-08-19
仕様状態: Normative（`CLAUDE_IMPLEMENTATION_SPEC.md` に基づく全面刷新後の製品仕様）

このドキュメントは実装後の現行仕様である。旧 Event モデル・旧 SQLite スキーマ・旧 API との互換性はない。

---

## 0. プロジェクト識別子

- **GitHub リポジトリ名**: `vrclog-companion`
- **Go module path**: `github.com/vrclog/vrclog-companion`
- **配布バイナリ（Windows）**: `vrclog.exe`
- **アプリ表示名**: VRClog Companion（短縮: VRClog）

---

## 1. 概要

### 1.1 目的

VRClog Companion は VRChat のローカルログを受動的に監視し、`vrclog-go` の正規 `Observation` として **ユーザー PC 内の SQLite にのみ永続化** する。その Observation ストリームを World / Presence / Media の利用者向け状態へ投影（Projector）し、ローカル HTTP API + Web UI で提供する。ワールド内で再生できなかったメディアの元 URL を復元し、コピー・ブラウザ起動できることが主要なユースケースである。

### 1.2 基本方針

- 中央サーバー・クラウドアップロード・テレメトリは一切ない
- データはユーザー PC 内にのみ保存される
- LAN アクセスは提供するが、安全側デフォルト（loopback bind, LAN 時は Basic Auth 必須）で意図しない公開を防ぐ
- UI はブラウザのみ配布（Web UI を go:embed で同梱）
- ライブ更新は SSE
- VRChat プロセス・API とは一切連携しない（ログファイルの読み取りのみ）

### 1.3 対応 OS

- Windows 11（本番ターゲット）
- macOS（開発用）

---

## 2. 3リポジトリ契約

```text
vrclog-go          canonical Event, Record/Cursor, Engine, Follow/ReadFile
  ← vrclog-adapters community Adapter（YamaPlayer, iwaSync3）
    ← vrclog-companion（本リポジトリ）
```

Companion だけが所有する責務:

- Adapter 構成（compile-time）
- Record 単位 ingest supervision
- SQLite スキーマと CommitRecord トランザクション
- Projector（World/Presence/Media）と通知ポリシー
- HTTP API / SSE / Web UI

Companion は `vrclog-go` / `vrclog-adapters` の公開契約のみを使用する。独自 Parser・独自 Event 型・独自 Adapter interface は持たない。

---

## 3. データフロー

```text
VRChat output_log
        │
        ▼
vrclog.Follow(ctx, FollowConfig{Cursor}) → iter.Seq2[Record, error]
        │  （LogSnapshot と比較して DeliveryPhase を確定: catch_up | live）
        ▼
Engine.Process(record) → Result{ Observations, Diagnostics }
        │
        ▼
Store.CommitRecord（単一 SQLite トランザクション）
  ├─ observations INSERT（重複検知。内容が異なる衝突は fatal）
  ├─ diagnostics INSERT OR IGNORE
  └─ ingest_cursors UPSERT
        │  COMMIT
        ▼ （新規挿入された Observation のみ、phase 付き）
Projector Manager.Apply(obs)  ← catch_up / live どちらも必ず適用
  ├─ WorldProjector
  ├─ PresenceProjector
  └─ MediaProjector
        │
        ▼ （phase == live のときだけ）
        ├─ SSE Broadcaster（generic `observation` event）
        └─ Notifier（World/Player の Change のみ Discord へ）
```

catch-up（起動時・source 再接続時に読み直した既存ログ由来）の Observation は DB/Projector には反映されるが、SSE/Discord へは一切出さない。live（capture 後に到着したバイト由来）の Observation だけが外部 side effect を発生させる。詳細は §5.5。

---

## 4. Adapter 構成

`internal/adapter.BuildEngine()` がコンパイル時に Engine を構成する。community adapter は集約パッケージを使わず、個別パッケージを明示的に import する — 新しい adapter はこのファイルの差分と review なしに有効化されない。

```go
import (
    "github.com/vrclog/vrclog-adapters/yamaplayer"
    "github.com/vrclog/vrclog-adapters/iwasync3"
)

core := vrclog.NewVRChatAdapter()
community := []vrclog.Adapter{
    yamaplayer.New(),
    iwasync3.New(),
}
all := append([]vrclog.Adapter{core}, community...)
engine, err := vrclog.NewEngine(all...)
```

- built-in（`vrchat.core`）を先頭に固定
- community adapter の順序は上記コードの記述順（yamaplayer → iwasync3）で固定
- global init registry・実行時プラグイン読み込み・YAML パターン設定は存在しない
- `vrclog-adapters` の集約 `All()` は存在しない（個別パッケージのみが公開契約）

現在ロードされる Adapter:

| ID | Origin |
|----|--------|
| `vrchat.core` | core |
| `community.yamaplayer` | community |
| `community.iwasync3` | community |

`GET /api/v1/adapters` で参照可能。

---

## 5. Ingest パイプライン

### 5.1 RecordSource

```go
type DeliveryPhase string

const (
    DeliveryCatchUp DeliveryPhase = "catch_up"
    DeliveryLive    DeliveryPhase = "live"
)

type SourceRecord struct {
    Record vrclog.Record
    Phase  DeliveryPhase
}

type RecordSource interface {
    Records(ctx context.Context) iter.Seq2[SourceRecord, error]
}

type RecordSourceFactory interface {
    NewSource(ctx context.Context, cursor *vrclog.Cursor) (RecordSource, error)
}
```

`VRChatSourceFactory` は `vrclog.Follow` を薄くラップする。カーソル付きで `ErrCursorSourceMissing` が発生した場合、警告を一度だけログ出力し、カーソルなしで再開始する（ループしない、同じ `NewSource` 呼び出し内で completes）。`NewSource` は呼ばれるたびに `vrclog.CaptureLogSnapshot` で新しい LogSnapshot を取得し、各 Record の phase をそれで判定する（§5.5）。

### 5.2 Runner

`internal/ingest.Runner` が per-Record トランザクションループを駆動する。

```text
for sr, err := range source.Records(ctx) {
    result := engine.Process(sr.Record)
    for {
        commitResult, err := store.CommitRecord(ctx, RecordCommit{sr.Record, result, now})
        if err == nil { break }
        if conflict { fatal: ErrIntegrityViolation, StateFailed, Run() が error を返す }
        // それ以外の DB エラーは bounded backoff (1s〜30s) でリトライ。次 Record へは進まない。
    }
    for obs := range commitResult.InsertedObservations {
        if err := onInsert(ctx, sr.Phase, obs); err != nil {
            fatal: ErrProjectionFailure, StateFailed, Run() が error を返す
        }
        // onInsert 内部: Projector.Apply（両 phase）→ phase==live のときだけ SSE broadcast + 通知
    }
}
```

- **不変条件**: Observation/Diagnostic の保存と cursor 更新は同一トランザクションでコミットされる
- 0 Observation の Record でも cursor は前進する
- DB エラー（transient）時は同じ Record を bounded backoff でリトライし、次 Record を消費しない
- Source 致命的エラー時は、最後にコミットされた cursor から `RecordSourceFactory` 経由で source を再構築する（bounded backoff, 1s〜30s）。1 Record 以上を正常 commit した source run は progress ありとし、次に source が失敗したときの backoff attempt カウンタを 0 へリセットする
- Observation conflict と post-commit Projector 適用失敗は fatal（§5.4）で、backoff/retry の対象にならない

### 5.3 Duplicate 判定と Conflict の fatal 化

Observation identity は `vrclog.ObservationID` のみで判定する。raw line ハッシュや URL 正規化による重複排除は行わない。

同一 ID が既に存在する場合、以下 9 フィールドを比較する:

`occurred_at, type, payload_json, adapter_id, rule_id, record_id, source_id, source_offset, source_line`

（`sequence`, `ingested_at` は除外）

- 完全一致 → 既知の重複として無視（cursor は前進）
- 不一致 → `ErrObservationConflict` でトランザクション全体をロールバック（cursor は前進しない）

### 5.4 Fatal エラーと controlled shutdown

競合する Observation を dropして再commitする処理は **存在しない**。同一 ID で内容が異なる衝突は決定的な整合性違反であり、以下の扱いになる。

| エラー種別 | sentinel | 挙動 |
|-----------|----------|------|
| Observation content conflict | `ErrIntegrityViolation` | commit 済みの trx はロールバック済み。cursor 不変。diagnostic を書かない。`Runner.Status().State` = `StateFailed`。`Run()` が error を返す |
| Post-commit Projector Apply 失敗 | `ErrProjectionFailure` | Observation は既に commit・cursor 前進済み（ロールバック不可）。`StateFailed`。`Run()` が error を返す。再起動時の Rebuild で一貫状態に戻る |

`cmd/vrclog-companion/main.go` は `run() error` パターンで、Runner の fatal error を error channel 経由で受け取り、cancel → notifier → SSE → HTTP → DB の順で controlled shutdown し、プロセスは非ゼロで終了する。復旧手順は「アプリを停止し、DB ファイルをリネームまたは削除して再起動する」（§6 と同じ）。

fatal エラーの内部ログには `observation_id`, `adapter_id`, `rule_id` を含めて診断可能にするが、`/api/v1/health` のような未認証エンドポイントには固定文字列（`"integrity violation"` / `"projection failure"`）のみを返し、詳細は漏らさない。

### 5.5 Catch-up / Live delivery phase

初回起動・アプリ再起動・source 再接続のたびに、source 開始前から既に disk 上に存在していたログを DB/Projector へ backfill しつつ、新着通知として扱わないための区別。

- `RecordSourceFactory.NewSource` が呼ばれるたび（source retry を含む）に `vrclog.CaptureLogSnapshot` で新しい LogSnapshot を取得する
- 各 Record は `snapshot.Contains(record)`（`record.NextOffset <= captured size`）で phase を判定する。timestamp 比較は使用しない
- capture 時に存在したバイトの範囲内 → `catch_up`、capture 後に追記・新規作成されたファイル由来 → `live`
- source retry で蓄積した Record も同じ仕組みで catch-up 扱いになり、復旧時の通知 burst を防ぐ

| | Store | Cursor | Projector | SSE | Discord |
|---|---|---|---|---|---|
| catch_up | ○ | ○ | ○ | × | × |
| live | ○ | ○ | ○ | ○ | Change filter に従う |

readiness（`/api/v1/health` 以外の 503 解除）は DB schema 検証と起動時 Projector Rebuild の完了のみを意味し、catch-up の完了を待たない。

---

## 6. SQLite スキーマ（version 3）

`PRAGMA user_version` で管理する。**自動マイグレーションはない。**

version 3 は `vrclog-go` の Observation ID 公式変更（emission index 削除）を反映したものである。テーブル構造は version 2 と同一だが、ID の identity contract が変わったため version を上げている。**version 2 の DB はそのまま使えず、明確に拒否される。**

| 検出状態 | 挙動 |
|---------|------|
| `user_version == 3` | テーブル存在検証後に利用 |
| `user_version == 0`、旧テーブルなし | schema 3 を新規作成 |
| `user_version == 0`、旧テーブルあり（`events`/`ingest_cursor`/`parse_failures`） | fatal `ErrUnsupportedSchema` |
| `user_version == 2`（旧 Observation ID 形式） | fatal `ErrUnsupportedSchema` |
| それ以外のバージョン | fatal `ErrUnsupportedSchema` |

fatal 時はアプリを停止し、DB ファイルをリネームまたは削除して再作成する。エラーメッセージにこの手順を含める。

```sql
CREATE TABLE observations (
    sequence       INTEGER PRIMARY KEY AUTOINCREMENT,
    id             TEXT NOT NULL UNIQUE,
    occurred_at    TEXT NOT NULL,
    type           TEXT NOT NULL,
    payload_json   TEXT NOT NULL,
    adapter_id     TEXT NOT NULL,
    rule_id        TEXT NOT NULL,
    record_id      TEXT NOT NULL,
    source_id      TEXT NOT NULL,
    source_offset  INTEGER NOT NULL,
    source_line    INTEGER NOT NULL,
    ingested_at    TEXT NOT NULL
);

CREATE TABLE ingest_cursors (
    source_id     TEXT PRIMARY KEY,
    path          TEXT NOT NULL,
    byte_offset   INTEGER NOT NULL,
    line_number   INTEGER NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE diagnostics (
    id             TEXT PRIMARY KEY,
    record_id      TEXT NOT NULL,
    source_id      TEXT NOT NULL,
    source_offset  INTEGER NOT NULL,
    source_line    INTEGER NOT NULL,
    adapter_id     TEXT,
    rule_id        TEXT,
    code           TEXT NOT NULL,
    message        TEXT NOT NULL,
    created_at     TEXT NOT NULL
);
```

- `ingest_cursors.path` は再開処理専用であり、Observation API には一切出さない
- raw line は既定で保存しない（Diagnostic の message も URL 除去 + 512byte 上限で redact）
- WAL mode, busy_timeout 5s, `_txlock=immediate`（CommitRecord が確実に書き込みロックを取得する）

---

## 7. Projector

Observation は永続化された事実、Projector はそれを決定的に投影する派生状態。DB から常に再構築可能。

### 7.1 Manager 適用順序

`Manager.Apply(obs)` は以下の順序を厳守する:

1. World transition（`world.joining_observed` の場合）
2. Presence reset（definitive transition の場合のみ）
3. Media session reset（同上）
4. Change 一覧を返却

`Manager.Rebuild(ctx, allObservations)` は起動時に sequence 昇順で全 Observation を再生し、Change 発行を抑制する（通知・SSE を発生させない）。

### 7.2 WorldProjector

- `world.joining_observed` が definitive transition。同一 world ID + instance ID は重複として無視
- `world.entering_observed` の名前は pending として保持し、15 秒以内（`OccurredAt` 基準、wall clock ではない）の joining と merge する
- entering → joining, joining → entering のどちらの順序でも同じ最終状態になる
- definitive transition のみ `WorldChanged` を発行し、Presence をリセットする

### 7.3 PresenceProjector

- キーは Player.ID が非空ならそれ、空なら trim 済み DisplayName
- 重複 join は no-op、未知の left は no-op
- World transition による reset では `PlayerLeft` を発行しない（離脱通知はしない）

### 7.4 MediaProjector

初期 status は `observed` / `failed` のみ（`playing` は判定材料がないため作らない）。

**BestOpenableURL 優先順位**: `source` > `resolver_input` > `playback_input` > なし。`resolved`（signed CDN URL 等）は対象外。同一優先度内では最初に観測された URL を維持する。`ResourceResolved` の `Input` は元の Role（通常 `resolver_input`）のまま保持されるため、`ResourceResolved` 単独（先行する `ResourceURLObserved` なし）でも Input が BestOpenableURL 候補になる。

**時間窓**（すべて `OccurredAt` 基準、`<=` で境界を含む — rebuild と live で同一の判定になる）:

| 定数 | 値 | 用途 |
|------|-----|------|
| `mediaCorrelationWindow` | 10 秒 | exact target / exact URL / 単一曖昧候補、すべてに一律適用 |
| `mediaSourceDuplicateWindow` | 2 秒 | `role=source` の重複バースト判定専用 |

**`role=source` の相関規則**（§7.4.1）: 原則として常に新規 Attempt を開始する。既存 Attempt へ merge してよいのは、次を **すべて** 満たす「重複バースト」だけ — exact 同一 URL、`mediaSourceDuplicateWindow`（2秒）以内、target が競合しない、current world session。同一 target でも URL が異なれば新規、同一 URL でも 2 秒を超えれば新規。exact target 一致だけでは `role=source` の merge 条件に **ならない**。

**`resolver_input` / `playback_input` 等（`role=source` 以外の `ResourceURLObserved`）の相関順位**:

1. exact target（component + key 完全一致、`mediaCorrelationWindow` 以内、target 競合なし）
2. exact URL 一致（`mediaCorrelationWindow` 以内、target 競合なし）
3. 単一の曖昧でない直近候補（`mediaCorrelationWindow` 以内、同一 world session、target 競合なし）
4. 新規 Attempt

**`ResourceResolved` の相関順位**:

1. Input URL 一致（`mediaCorrelationWindow` 以内）
2. exact target（同上）
3. Output URL 一致（同上）
4. 単一の曖昧でない直近候補
5. 新規 Attempt

相関後、**Input と Output の両方**を Resources へ追加する。それぞれ canonical Event の Kind/Role/URL をそのまま保持し、Output を強制的に `role=resolved` へ上書きしない（実質的に core adapter は Output に `resolved` を設定するため、通常は同じ結果になる）。

**MediaErrorObserved の相関順位**:

1. Resource URL があれば exact URL 一致（`mediaCorrelationWindow` 以内）
2. exact target（同上）
3. 単一の曖昧でない直近候補
4. 新規 Attempt

いずれも複数の曖昧な候補（2件以上）がある場合は merge せず新規 Attempt にする（誤 merge より分離を優先）。

World transition を跨いだ correlation は行わない（`currentWorldInstanceID` でスコープ）。直近履歴（最大 50 件）は世代を跨いで保持される。

`LatestOpenableMedia` は世代を問わず直近の BestOpenableURL 保持 Attempt を返す（failed でも対象）。

### 7.5 Immutability

`Change`（`MediaAttemptUpdated.Attempt`, `WorldChanged.Current`/`Previous`, `WorldNameUpdated.Current`）と `Snapshot`、`RecentMedia()` の返り値はすべて内部状態から独立した deep copy である。呼び出し側が返り値やそのスライス・ポインタフィールドを mutate しても、Manager の内部状態や後続の `Snapshot()`/`RecentMedia()` の結果には一切影響しない。`cloneMediaAttempt` が単一の clone 契約として、Change 発行・`recentSnapshot`・将来の API DTO 変換すべてで使われる。

---

## 8. API

Base path: `/api/v1`

### 8.1 認証

| Endpoint | Loopback | LAN モード |
|----------|----------|-----------|
| `GET /health` | 不要 | 不要 |
| その他すべて | 不要 | Basic Auth 必須（`/stream` は SSE token も可） |

`/auth/token` は Basic Auth のみ受理する（SSE token での自己更新は不可）。

起動直後の Projector rebuild 中は `/health` 以外すべて `503 {"status":"rebuilding"}` を返す。

### 8.2 `GET /api/v1/health`

```json
{
  "status": "ok | degraded",
  "database": "ok | error",
  "ingest": "running | retrying | stopped | failed | rebuilding",
  "last_ingest_error": "",
  "last_record_at": "",
  "loaded_adapters": 3
}
```

`ingest: "failed"` は fatal な integrity violation または post-commit projection failure の後の終端状態を示す（`status` は他の non-running 状態と同様 `degraded`。専用の health status は設けない）。secret・path・URL は一切含まない。

### 8.3 `GET /api/v1/observations`

Query: `cursor`（sequence）, `limit`（1-500, default 100）, `type`（exact）, `adapter_id`（exact）, `since`, `until`（RFC3339）

デフォルト順序は sequence 降順。`type`/`adapter_id` はアローリストを使わず SQL bind parameter で照合する。

```json
{
  "items": [{
    "sequence": 42,
    "id": "...",
    "occurred_at": "...",
    "type": "resource.url_observed",
    "payload": {},
    "adapter_id": "community.yamaplayer",
    "rule_id": "youtube_resolve_url",
    "record": { "id": "...", "source_id": "...", "offset": 1234, "line": 52 },
    "ingested_at": "..."
  }],
  "next_cursor": 41
}
```

`next_cursor` は該当なしでも常にキーとして存在し、値は `null`（フィールド省略はしない — クライアントの `!== null` 判定を壊すため）。local path・raw line は含まない。

### 8.4 `GET /api/v1/state`

```json
{
  "world": { "id": "...", "name": "...", "instance_id": "...", "joined_at": "..." },
  "players": [{ "id": "...", "display_name": "...", "joined_at": "..." }],
  "latest_openable_media": { "attempt_id": "...", "url": "...", "status": "failed", "observed_at": "..." }
}
```

### 8.5 `GET /api/v1/media/recent`

Query: `limit`（1-50, default 20）。新しい順で MediaAttempt を返す。

### 8.6 `GET /api/v1/adapters`

```json
{ "adapters": [{ "id": "vrchat.core", "origin": "core" }] }
```

### 8.7 `GET /api/v1/stats/basic`, `GET/PUT /api/v1/config`, `POST /api/v1/auth/token`

既存パターンを維持。Stats は observations テーブルの集計と Projector Manager の直近メディア件数から算出する。

---

## 9. SSE

`GET /api/v1/stream` は単一イベント種別のみ送信する。

```text
id: <observation-id>
event: observation
data: <observation API JSON>
```

type ごとに SSE event 名は分けない。

### 9.1 Last-Event-ID recovery

1. Broadcaster に subscribe
2. 現在の high-water sequence を取得
3. Last-Event-ID から sequence を解決できなければ `event: reset`（`id:` 空）を送信して切断
4. `(lastSeq, highWater]` の Observation を DB から backlog 送信
5. 以降は live channel から `sequence > lastSent` のみ送信（dedup）

subscribe と high-water 取得の間にコミットされた Observation は backlog か live channel のいずれかで必ずカバーされる。

### 9.2 Backpressure

per-client バッファが溢れた場合は ingest をブロックせず切断する。クライアントは Last-Event-ID で再接続して復旧する。

---

## 10. 通知（Discord）

対象: definitive `WorldChanged`, `PlayerJoined`, `PlayerLeft` の Change のみ。

非対象: `WorldNameUpdated`, `MediaAttemptUpdated`, startup rebuild 中の Change, duplicate。

**サニタイズ**: 送信ペイロードは常に `allowed_mentions: {"parse": []}` を含める。プレイヤー名・ワールド名は Markdown 制御文字をエスケープし、`@everyone`/`@here`/`<@id>` のメンショントリガーと `http(s)://` の自動リンクをゼロ幅スペースで無害化する。

メディア URL は一切送信しない。

---

## 11. セキュリティ・プライバシー

- デフォルトは loopback bind。LAN モードのみ Basic Auth + rate limit + auth failure lockout + CSRF protection を有効化
- Basic Auth は TLS なしでは盗聴保護がないため、LAN モードは信頼できるネットワークでのみ使用する
- Diagnostics の message は DB 保存前・API 応答前の二重で redact する（URL 除去、512byte 上限）
- Media URL は Discord へ送信しない、外部メタデータを取得しない、自動で開かない
- ブラウザで開く操作は `http`/`https` スキームのみ許可
- raw log line は DB にも API にも出さない
- `Server` は既定で not-ready（`SetReady(true)` を呼ぶまで `/health` 以外 503）
- SSE（`/api/v1/stream`）は無期限接続を許すため server-global write timeout を無効化しているが、それ以外のルートは `http.TimeoutHandler`（既定 15 秒）でハンドラ実行時間全体を bound する — global timeout=0 だけでは非 SSE ルートが無制限に stall しうるため

---

## 12. テスト方針

- `internal/store`: schema 検証、CommitRecord 原子性、query
- `internal/ingest`: Runner の DB/source リトライ、cursor missing fallback、VRChatSource 統合
- `internal/projector`: World 二段階 merge、Presence、Media correlation（YamaPlayer/iwaSync3 シナリオ、境界値）
- `internal/api`, `internal/sse`: ルーティング、認証、SSE backlog/race
- `test/integration`: 実 SQLite + 実 HTTP サーバーでの統合テスト
- `test/e2e`: `vrclog-adapters` の実フィクスチャを通した media URL recovery E2E

---

## 13. ビルド

```bash
gofmt -w .
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go test -tags=integration -count=1 ./test/integration/...
go test -tags=e2e -count=1 ./test/e2e/...
GOOS=windows GOARCH=amd64 go build ./cmd/vrclog-companion/

cd web && npm ci && npm run lint && npm run build

go mod tidy
git diff --exit-code -- go.mod go.sum
```

CI は Windows/Ubuntu 双方でのユニット/integration/E2E テストに加え、Ubuntu 上で `go test -race` を独立ジョブとして実行する。Release workflow は `verify` job（上記コマンド一式）が成功したときのみ Windows バイナリのビルド・公開に進む — tag push だけで未検証の artifact が公開されることはない。
