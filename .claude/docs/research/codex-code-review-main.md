# Codexコードレビュー: main

**日時**: 2026-08-19
**総合判定**: CRITICAL
**パースペクティブ数**: 3（正確性 / セキュリティ / パフォーマンス・アーキテクチャ）
**反復深化ラウンド数**: 3/5
**最終信頼度**: thorough（全ラウンドで `analysis_depth: thorough/adequate`, 未解決事項なし）
**ベース**: origin/main (5d312d87d877e75c9214f24ad2e77977ad6800bf)
**HEAD**: 未コミットの作業ツリー（このセッションでコミットは作成していない — 全面刷新実装がすべて working tree 上の変更として存在）
**変更ファイル数**: 108（レビュー対象。`.claude/docs/research/**`、`CLAUDE_IMPLEMENTATION_SPEC.md`、ビルド成果物は対象外）

## 総合評価

`CLAUDE_IMPLEMENTATION_SPEC.md` に基づく全面刷新（旧 Event モデル→Observation/Projector アーキテクチャ）は、設計として一貫しており、セキュリティ観点（認証配線、SQL bind parameter、Diagnostics redaction、Discord送信内容、XSS対策、URL scheme検証）は Perspective B により **所見ゼロ** で確認された。しかし正確性観点で **3件の CRITICAL** が実コード追跡により確認された。いずれも「異常系・再起動系のパス」に潜んでおり、通常のハッピーパステストでは検出されない性質のバグである。マージ前の修正を推奨する。

## パースペクティブ統合

### 一致点
- 認証配線（`/api/v1/health` のみ未認証、他は `wrapAuth`/`wrapSSEAuth` 経由）は正しく実装されている
- SQL は全て bind parameter 化されている（インジェクションなし）
- Discord 通知は World/Player Change のみで、`MediaAttemptUpdated`（メディアURL）は送信対象外
- `internal/projector/media.go` の相関スキャンや `internal/sse/broadcaster.go` の fan-out はこの規模のローカルアプリとして許容範囲

### 不一致点と解決
パースペクティブ間での severity 判定の直接的な矛盾はなかった。ただし Round 3 の追加検証で以下の再評価を実施:
- `internal/api/config.go` の `err.Error()` 露出は、当初 WARNING 想定だったが、LAN モードでの Basic Auth 保護とローカルループバック専用の脅威モデルを踏まえ **SUGGESTION に確定**（Codex 最終判断）

## 所見一覧

### CRITICAL（マージ前に修正必須）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 1 | `internal/api/stream.go` | 57, 138 | SSE 再接続の backlog 上限が `Broadcaster.HighWaterSequence()`（プロセス起動時0、新規insert時のみ進む）を使っており、**プロセス再起動直後**に有効な Last-Event-ID を持つクライアントが再接続すると、DB に永続化済みの Observation が存在するにもかかわらず backlog が **無言で空** になる。クライアントは完全に同期済みと誤認し、後続のライブイベントでカーソルが進むため、欠落は恒久的に検出不能になる。 | `Store.LatestSequence(ctx) (int64, error)` を新設（`SELECT COALESCE(MAX(sequence),0) FROM observations`）し、`Subscribe()` 後に DB から high-water を取得するよう変更。`sendBacklog` は実際に配信した最終 sequence を返すようにし、`handleStream` の `lastSent` をその値で更新（live channel との重複防止）。加えて `maxBacklogObservations = 1000` 件の再生上限を設け、超過時は `writeSSEReset` にフォールバック。SQLite WAL 下でのコミット済みデータのみを読む性質上、この修正に競合リスクはない（Round 3 で検証済み）。 | Perspective A (正確性) → Round 2/3 で修正案検証・確定 |
| 2 | `internal/ingest/status.go` | 61 (`recordError`) | `t.status.LastError = err.Error()` が ingest エラー（`vrclog.Follow` 由来の `os.Open` エラー等）をそのまま保持し、**認証不要**の `GET /api/v1/health` の `last_ingest_error` フィールドとして露出する。ローカルファイルパスが含まれ得る（例: `open /Users/foo/AppData/.../output_log_...txt: no such file or directory`）。 | `internal/ingest/status.go` の `recordError` で **write time** に redact する（read time の `health.go` ではなく、`ingest.Status` を将来読む全ての呼び出し元に対して安全にするため）。`*fs.PathError` は構造的に unwrap して `Op` + サニタイズ済み reason を残し、それ以外は正規表現ベースの絶対パス除去（Windows `C:\...` / Unix `/...`）でフォールバック。 | Perspective A (正確性) → Round 2 で修正案検証・確定 |
| 3 | `internal/ingest/runner.go` | 197-224 (`runSource` の内側リトライループ) | `CommitRecord` が `store.ErrObservationConflict`（同一 ID・異なる内容という**恒久的**な conflict）で失敗した場合も、一時的な DB エラーと**区別せず**同じ bounded backoff（1s〜30s）で無限リトライする。同じ Record は決定的に同じ conflict を再生成するため、30秒間隔での永久リトループに陥り、**それ以降のすべての ingest が恒久的に停止**する。DB ファイルを手動削除する以外に復旧手段がない。 | `commitErr` を `errors.Is(commitErr, store.ErrObservationConflict)` で判定し、一時的エラーとは別経路で処理する。具体的には conflict を Diagnostic として永続化し、当該 Record の cursor を進めて次の Record へ進む（silent overwrite ではなく "skip with audit trail"）。 | Round 3 アドバーサリアルスイープで新規発見。実コード追跡で確認済み |

### WARNING（修正推奨）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 4 | `internal/projector/manager.go` | `Manager.Rebuild` | `Rebuild` が既存の `m.world`/`m.presence`/`m.media` に直接 replay するため、同一 Manager に対して2回呼ぶと Media の Resources/Errors 等が重複する。現在の本番コードパス（`cmd/vrclog-companion/main.go`）では起動時に一度しか呼ばれないため未発火だが、"DBから完全に再構築可能" という明言された契約に違反する。 | 新しい `Manager` に replay し、成功時のみ `m.world`/`m.presence`/`m.media` を swap する（Codex 提供の完全な実装コードあり）。`TestManager_RebuildTwiceIsIdempotent` を追加し、2回 Rebuild しても Resources/Errors が重複しないことを検証。 | Perspective A (正確性) → Round 2 で FIX_NOW 判定・コード確定 |

### SUGGESTION（改善提案）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 5 | `internal/api/config.go` | 41 (`handlePutConfig`) | `ConfigService.UpdateConfig` のエラーを `err.Error()` のまま 400 レスポンスの public message として返す。config/secrets ファイルの保存失敗時、ローカルパスを含み得る。LAN モードでは Basic Auth 保護下にあり、露出するのはユーザー自身の app-data ディレクトリのパスのみのため、脅威としては軽微。 | 固定の汎用メッセージに置き換えるか、`ConfigService` から型付き/センチネルのバリデーションエラーのみを公開する。 | Round 2 追加発見 → Round 3 で severity を WARNING→SUGGESTION に確定 |
| 6 | `internal/store/query.go` | `since`/`until` フィルタ | `occurred_at` 範囲フィルタと `ORDER BY sequence` の組み合わせが `observations_occurred_idx(occurred_at, sequence)` を完全には活かせない可能性がある。ローカルアプリの現実的なデータ量では問題にならないが、数ヶ月に渡るスパースなクエリでは体感速度に影響し得る。 | 需要が出た場合、`ORDER BY occurred_at DESC, sequence DESC` + 対応カーソルへの変更を検討。現時点では対応不要。 | Perspective C (パフォーマンス) |
| 7 | `web/src/pages/History.tsx` | `setObservations` | "Load more" を繰り返すと React state が無制限に蓄積し、5000〜10000件規模でブラウザメモリを消費する。バックエンドのページングは正しく機能している。 | 表示件数に軽い上限（例: 直近1000件のみ保持）を設けるか、将来的に仮想スクロールを検討。 | Perspective C (パフォーマンス) |

## 対応状況（修正実装）

ユーザー指示「WARNING/SUGGESTIONも含めて全て修正」に基づき、以下を実装しレビュー後の品質ゲート（`gofmt`, `go vet`, `go build`, `go test ./...`, `go test -race ./...`, integration, e2e, `GOOS=windows go build`, `npm run lint`, `npm run build`）を全て通過済み。

| # | severity | 対応 | 実装内容 |
|---|----------|------|---------|
| 1 | CRITICAL | 修正済み | `Store.LatestSequence` 新設、`internal/api/stream.go` の `handleStream`/`sendBacklog` を再設計。回帰テスト `TestStream_BacklogSurvivesRestart`（再起動直後の backlog 欠落）、`TestStream_LiveEventNotDuplicatedAfterBacklog`（backlog/live 二重配信防止）を追加 |
| 2 | CRITICAL | 修正済み | `internal/ingest/status.go` に `publicErrorMessage`（write-time redaction）を追加。`internal/ingest/status_test.go` を新設（5テスト） |
| 3 | CRITICAL | 修正済み | `store.ObservationConflictError` 型を新設し `internal/ingest/runner.go` で conflict を Diagnostic 化して skip。`TestRunner_ObservationConflictDoesNotBlockIngestForever` を追加 |
| 4 | WARNING | 修正済み | `internal/projector/manager.go` の `Rebuild` を swap 方式に変更。`TestManager_RebuildTwiceIsIdempotent` を追加 |
| 5 | SUGGESTION | 修正済み | `internal/app/config.go` に `ValidationError` 型を新設し、`internal/api/config.go` でクライアント入力起因のエラーのみメッセージを公開・それ以外は汎用メッセージ＋サーバーログに変更。`internal/app/config_test.go` を新設 |
| 6 | SUGGESTION | **見送り**（要ユーザー判断） | Codex 自身の最終判断が「現時点では対応不要」。提案されたカーソル方式変更（`ORDER BY occurred_at` + 複合カーソル）は `SPEC.md`/`CLAUDE.md` に明記された「`sequence` int64 を不透明カーソルとする」設計上の決定を変更する影響の大きい変更であり、実測データ量での問題も未確認のため、指示に反して実装を見送った。対応が必要になった場合は改めて設計レビューから着手することを推奨 |
| 7 | SUGGESTION | 修正済み | `web/src/pages/History.tsx` に `maxRetainedObservations = 1000` の上限を追加（"Load more" 繰り返し時に古い行から破棄） |

### 検討したが対応不要と判断した項目

- **`internal/api` が `internal/projector`/`internal/store` の型に直接依存**（`app` 層専用DTOを経由しない）: このリポジトリの規模（単一チーム・単一リポジトリのローカルデスクトップアプリ）では、抽象化の追加によるリスク低減効果より間接化のコストの方が大きいと判断（ACCEPTABLE_AS_IS）。
- **`internal/ingest/status.go` と `internal/store/diagnostics.go` の2つの redaction ロジックの重複**: 対象データの性質（永続化される Diagnostic の内容 vs. 一時的な公開向けエラーテキスト）が異なるため独立実装のままで問題なし。3つ目の redaction 需要が出た場合のみ `internal/redact` のような中立パッケージへの切り出しを検討。
- **`LatestSequence()` の SQLite WAL 下での競合可能性**: `SELECT MAX(sequence)` はコミット済みデータのみを読み、`CommitRecord` は `tx.Commit()` 成功後にのみ broadcast するため、dirty read や逆転は発生しない。

## 代替実装提案

SSE backlog 修正（CRITICAL #1）について、Codex は3つの候補を比較検討した:

- **(a) 採用**: `Store.LatestSequence()` を DB から都度取得 — プロセス再起動後も正しく機能し、テストや将来の別サーバー構成にも自動的に効く
- (b) 却下: `main.go` から `Broadcaster.SeedHighWater()` で起動時に一度だけ seed する方式 — 特定の起動順序に依存し、テストや別の起動経路で容易に見落とされる脆さがある
- (c) 未提示の別案なし

## 深化ログ

| ラウンド | Codexへの質問内容 | 解決された項目 | 残った未解決事項 |
|----------|------------------|---------------|----------------|
| 1 | 3パースペクティブ並列レビュー（正確性/セキュリティ/パフォーマンス・アーキテクチャ）、108ファイルの全面刷新diffを重点ファイル指定付きで確認 | CRITICAL 2件（SSE backlog, health path leak）、WARNING 2件（Rebuild非冪等性, API層依存）を発見。セキュリティ観点は所見ゼロ | SSE修正の具体案、path leak修正の具体案、API層依存の是非、Rebuild冪等性対応要否 |
| 2 | 3並列で深掘り: (1)SSE修正案の妥当性検証とWAL下での競合有無, (2)path leak修正案の具体化と他の漏洩箇所探索, (3)API層依存とRebuild冪等性の最終判定 | SSE修正: `LatestSequence()`案を確定、`sendBacklog`の戻り値設計・bounded replay(1000件)を確定。Path leak: write-time redaction・`fs.PathError`構造的unwrap案を確定。新規発見: `internal/api/config.go`のerr.Error()露出。API層依存: ACCEPTABLE_AS_IS確定。Rebuild: FIX_NOW確定、swap実装コード確定 | config.go新規発見のseverity未確定、WALレース理論的検証未実施、redaction重複のDRY要否未検証、CommitRecord conflict時のRunnerリトライ挙動未検証 |
| 3 | アドバーサリアル最終スイープ: (1)LatestSequenceのWALレース理論検証, (2)redaction DRY要否, (3)CommitRecord conflict時のRunner無限リトライシナリオ, (4)config.go severityの最終確定 | WALレース: 実バグでないと確認。DRY: 独立実装のままで良いと確定。**新規CRITICAL発見**: ErrObservationConflict時の無限リトライで ingest が恒久停止するバグを実コード追跡で確認。config.go: SUGGESTIONに確定 | なし（全項目解決、`more_rounds_needed: false`） |

## 実行モード

- **インタラクティブモード**: true
- **Phase 0 スコープ選択**: 実行（差分範囲=ベースブランチ差分 / 深さ=ディープ / 重点=バランス重視 / severity=通常基準）
- **採用された Phase 0 デフォルト値**: 深度=3/5 ラウンド、重点=balanced、差分範囲=base-branch、severity=normal
- **Phase 4 次アクション確認**: 実行済み

## エスカレーション回答（インタラクティブモード時）

| Phase | 質問 | ユーザー回答 | 判定への影響 |
|-------|------|-------------|-------------|
| 0 | 差分範囲/深さ/重点観点/判定厳しさ | ベースブランチ差分 / ディープ(MIN=3,MAX=5) / バランス重視 / 通常基準 | Round数=3、focus均等、severity変換なしで確定 |
| 4 | レビュー結果 CRITICAL（3件）です。次にどうしますか？ | WARNING/SUGGESTIONも含めて全て修正 | CRITICAL 3件・WARNING 1件・SUGGESTION 2件（#5, #7）を実装し回帰テストを追加。SUGGESTION #6（`internal/store/query.go` の since/until 索引ミスマッチ）はCodex自身の判定が「現時点では対応不要」であり、提案対応（`ORDER BY occurred_at` + カーソル方式変更）は `SPEC.md`/`CLAUDE.md` が明記する「sequence を不透明カーソルとする」設計を変更する影響の大きい変更のため、実装せず保留としてユーザーに報告 |

Phase 2c のエスカレーション条件（severity 不一致・複数未評価代替案・ラウンド上限到達）はいずれも該当しなかったため、ユーザーへの追加確認は発生しなかった。

## 不確実性

- 108ファイル全ての完全な全文精査は、重点指定されたファイル（store/projector/ingest/SSE/API境界）を中心に実施しており、それ以外の細部まで完全網羅ではない（Perspective A自己申告）
- ローカルでの `go test`/`govulncheck` 実行は Codex 側では行っていない（read-only レビューのため。実際の `go test ./...` は本セッション内で別途全て実行し pass 済み）
- Round 3 の WAL レース検証は、修正コードがまだ working tree に反映されていない時点での**設計評価**であり、実装後の再検証が望ましい

## エスカレーション候補

なし。全ての CRITICAL/WARNING 所見に対して具体的な修正コードが確定しており、実装判断のみが残っている。
