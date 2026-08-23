# Codexコードレビュー: main

**日時**: 2026-08-23
**総合判定**: CRITICAL（**全7件修正済み、`go test -race ./...` / integration / e2e / Windows build 全通過**）
**パースペクティブ数**: 3（正確性 / セキュリティ / パフォーマンス・アーキテクチャ）
**反復深化ラウンド数**: 3/5
**最終信頼度**: thorough（全パースペクティブ、Round 3で収束確認済み）
**ベース**: HEAD (f2aff208880b6728755035c405b67b67b1c7b70c) — 注: mainブランチ自体への未コミット作業ツリー変更のためコミット範囲差分ではなく `git diff HEAD` を使用
**HEAD**: 作業ツリー（未コミット）
**変更ファイル数**: 33（修正30、新規3）

## 総合評価

CLAUDE_HARDENING_SPEC.mdの7フェーズ実装は全体として設計意図に忠実だが、Media相関ロジック（`internal/projector/media.go`）に**実際に再現可能なCRITICAL相関バグ**が1件見つかった — `findByExactURL`がtarget競合チェックを欠いており、異なるオンスクリーンターゲット（例: 2人のプレイヤーが同じURLを再生）が誤って同一MediaAttemptへマージされる。これは人間レビュアーが実テストで実証済み（`TestMedia_TwoPlayersInterleaved_NoMixup`は異なるURLケースのみをカバーしており、同一URL・異なるtargetのケースを見逃していた）。加えて、API/main.goの堅牢化に4件のWARNING、パフォーマンス面に2件のSUGGESTIONが見つかった。いずれも修正案は具体的かつ検証済みで、Codex 3ラウンドの反復深化でアドバーサリアルシナリオへの耐性も確認済み。

## パースペクティブ統合

### 一致点
- 全パースペクティブが、Fatal-conflict rollbackとDeliveryPhaseゲーティングは正しく実装されていると判断
- Perspective AとC（独立実行）がどちらもserver.go:306の静的SPAルートのラップ漏れを指摘（収束的発見）
- セキュリティ観点では新規CVE・シークレット漏洩・ミドルウェアバイパスは検出されず

### 不一致点と解決
矛盾なし。3パースペクティブの判定は独立していたが、severityの食い違いは発生しなかった（Perspective Aの2件のCRITICALはRound 2でCodex自身により1件がWARNINGへ格下げされたのみで、これは新情報＝実アダプタが該当フィールドを使用していないという事実に基づく妥当な格下げ）。

## 所見一覧

### CRITICAL（マージ前に修正必須）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 1 | `internal/projector/media.go` | 338-353 (`findByExactURL`) | `targetConflicts`チェックを欠く。**実テストで再現確認済み**: 異なるtarget（playerA/playerB）が同一URLをresolver_input roleで観測すると誤って1 Attemptへマージされる（正しくは2）。`findSourceDuplicate`/`findSingleRecentCandidate`は同チェックを持つのに`findByExactURL`だけ欠落 | `target *MediaTargetDTO`引数を追加し`targetConflicts(a.Target, target)`でスキップ。呼び出し箇所4箇所（media.go:173, 178, 225, `findCandidate`内258）はすべて既に`target`変数がスコープ内にあり追加配線不要 | Perspective A（Round 2/3で修正案検証・アドバーサリアル耐性確認済み） |

### WARNING（修正推奨）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 2 | `internal/projector/media.go` | 213-248 (`applyMediaError`) | `ev.Resource.URL`を相関検索にのみ使い、`attachResource`を一度も呼ばない。現状は3実アダプタ（core/yamaplayer/iwasync3）のどれも`MediaErrorObserved.Resource`を設定しないため到達不能だが、将来のアダプタ追加で再発しうる潜在バグ | `ev.Resource != nil`なら`attachResource(attempt, res, target)`をattempt解決後（`isNew`/`newAttempt`ブロックの後）に追加。roleは`ev.Resource.Role`をそのまま使用 | Perspective A（Round 2でCRITICAL→WARNINGへ格下げ、Round 3でCodexが再確認） |
| 3 | `internal/api/server.go` | 306 | 静的SPAキャッチオール（`s.mux.Handle("/", spa)`）が`wrapAuth`を経由せず登録され、readiness gate・15秒`http.TimeoutHandler`・rate limitのいずれも適用されない。本ハードニング作業以前から存在する箇所だが、新設した「SSE以外は有限deadline」という不変条件に対して一貫性を欠く | `s.mux.Handle("/", s.wrapAuth(spa))`。Codexは「起動中に静的シェルへ503が返るUX変化」を許容範囲と判断（server.goのコメントが元々「every route except /api/v1/health returns 503 until rebuild completes」と明言しているため、意図と整合） | Perspective A + Perspective C（独立して収束） |
| 4 | `cmd/vrclog-companion/main.go` | 266-286 | ingest RunnerのgoroutineをJOINせずに`cancel()`直後にnotifier/broadcaster/server/DBのshutdownへ進む。コメントは「Runnerのin-flight CommitRecordが完了してから...」という順序を主張しているが、コードはそれを強制しない。`Broadcaster.Broadcast()`は`Stop()`後も安全（空mapへの反復のみ）と確認済みだが、`notifier.Enqueue`が`notifier.Stop()`後に呼ばれるレース、および`db.Close()`とin-flight `CommitRecord`のオーバーラップの可能性が残る（`database/sql.DB.Close()`自体はin-use接続の完了を待つため後者は致命的ではない） | `runnerDone := make(chan struct{})`をrunner goroutineでdefer closeし、`cancel()`直後・`notifier.Stop()`の前に`select { case <-runnerDone: case <-time.After(5*time.Second): 警告ログ }`を追加。runner goroutine内の`errCh`送信もnon-blocking化（select/default）してjoinとのデッドロックを防止。既存の`TestRunner_CancellationCleanlyStops`（2秒以内のキャンセル応答を保証）と5秒待機は整合的とCodexが確認 | Perspective A（Round 2/3で修正案の妥当性・既存テストとの整合性を検証済み） |
| 5 | `.github/workflows/release.yml` | 8-9 | 新設した`verify` jobが、`release` jobのみに必要な`contents: write`をworkflowスコープから継承している。verify jobはnpm/go build・testのみでリリース権限は不要 | workflowスコープを`contents: read`に変更し、`release` jobにのみ`permissions: contents: write`を追加 | Perspective B（セキュリティ、OWASP A08） |

### SUGGESTION（改善提案）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 6 | `internal/projector/manager.go` | 84-118 (`applyLocked`) | Rebuild中（`m.rebuilding == true`）でもMedia系apply*メソッドが無条件に呼ばれ、`cloneMediaAttempt`によるフルディープコピー（Target + 4スライス）を実行した直後に`if m.rebuilding { return nil, nil }`で結果を破棄している。起動時の一度きりのコストであり、`mediaMaxRecent=50`で上限されるため実害は小さいとCodexは判断 | `applyResourceURL`/`applyResourceResolved`/`applyMediaError`に`emit bool`引数を追加し、状態変更はそのまま行い、`!emit`なら`Change`構築前に`nil`を返す。`applyLocked`から`!m.rebuilding`を渡す | Perspective C（2回の独立実行で収束）、Round 3で確認 |
| 7 | `cmd/vrclog-companion/main.go` | 243 | `errCh := make(chan error, 1)`の容量が1だが、HTTPサーバーとingest Runnerの2つのgoroutineが送信しうる。Finding 4の修正でrunner側がnon-blocking送信になった後は、2件目のfatal errorが（デッドロックではなく）ログにのみ記録され握りつぶされるだけのcosmeticな問題に縮小 | チャネル容量を2へ変更 | Perspective C、Round 3で確認 |

## 代替実装提案

Codexから重複した代替アプローチの提案はなし。各所見について単一の明確な修正案のみが提示され、複数案の評価が必要な状況（`pending_alternatives ≥ 2`）は発生しなかったため、Phase 2cのユーザー確認はスキップした。

## 深化ログ

| ラウンド | Codexへの質問内容 | 解決された項目 | 残った未解決事項 |
|----------|------------------|---------------|----------------|
| 1 | 3パースペクティブ並列（正確性・セキュリティ・パフォーマンス/アーキテクチャ）。Perspective Cは初回試行が実際にはcodex execを呼ばず（0バイト出力）検出・再実行 | Finding 1-5を発見。Finding 1は人間レビュアーが実テストで再現確認（`internal/projector/zz_repro_test.go`で反証、テスト後削除） | Finding 1/2の正確なseverity切り分け、Finding 3/4の具体的修正設計、Finding 5の正確なYAML diff |
| 2 | 「correctness-fixes」（Finding 1/2の修正案検証、他の呼び出し箇所の網羅確認）と「hardening-gaps」（Finding 3/4/5の修正設計、UXへの影響、DB Close安全性）を並列実行。hardening-gaps側は2回サブエージェントがcodex execを実際に待たずに終了する事象が発生し、メインエージェントが直接同期実行して解決 | Finding 1の修正箇所4箇所すべて特定・妥当性確認。Finding 2をCRITICAL→WARNINGへ格下げ。Finding 3/4/5の具体的YAML/コード diff確定 | なし（Codex自身が`more_rounds_needed: false`と回答したが、MIN_DEEPENING_ROUNDS=3のため追加ラウンドを強制実行） |
| 3 | アドバーサリアルドリルダウン: Finding 1修正のnilターゲット・Input/Output対称性検証、Finding 4修正の`db.Close()`安全性・既存テストとの整合性検証、Perspective Cから新たに浮上したFinding 6/7（Rebuild中の無駄なclone、errCh容量）の確認 | 全所見の最終severity確定（Finding 1=CRITICAL、Finding 2-5=WARNING、Finding 6-7=SUGGESTION）。Finding 1修正はnilターゲットケースで正しく動作することを確認 | なし。`convergence.more_rounds_needed: false`、`unresolved_uncertainties`は実運用でのメディア観測件数の見積もりのみ（テレメトリなしのため推定値） |

## 実行モード

- **インタラクティブモード**: true
- **Phase 0 スコープ選択**: 実行（ベースブランチ差分 / ディープ / バランス重視 / 通常基準）
- **採用された Phase 0 デフォルト値**: 深度=3/5ラウンド、重点=balanced、差分範囲=base-branch（実質的にはmainブランチ自体への未コミット作業ツリー差分として`git diff HEAD`で代替）、severity=normal
- **Phase 4 次アクション確認**: 実行予定

## エスカレーション回答（インタラクティブモード時）

| Phase | 質問 | ユーザー回答 | 判定への影響 |
|-------|------|-------------|-------------|
| 0 | 差分範囲 | ベースブランチ差分(推奨) | mainブランチ自体がHEADのため、`git diff HEAD`（作業ツリー差分）を実質的な等価物として採用 |
| 0 | 深さ | ディープ | MIN=3, MAX=5ラウンドを実施、実際に3ラウンドで収束 |
| 0 | 重点観点 | バランス重視 | 正確性・セキュリティ・パフォーマンスを均等にレビュー |
| 0 | 判定厳しさ | 通常基準 | severity変換なし |
| 2c | (該当なし) | - | パースペクティブ間のseverity矛盾なし、複数修正案の競合なしでスキップ |
| 4 | レビュー結果 CRITICAL です。次にどうしますか？ | CRITICAL項目から修正を開始 | 次アクション: Finding 1（findByExactURLのtarget競合バグ）から順に、検証済みの修正案（Finding 1-7）を実装 |

## 不確実性

- Finding 6（Rebuild中のcloneMediaAttempt無駄働き）の実運用での影響度は、テレメトリ不在のため「典型的ユーザーで数百〜数千件のmedia observation」という推定にとどまる（Codex自身の申告）
- 全3パースペクティブとも読み取り専用サンドボックスのため`go test`を実際には実行しておらず、静的コード読解に基づく判断（Finding 1のみ人間レビュアーが実テストで独立検証済み）

## 実行上の注記（サブエージェント信頼性について）

このレビュー実行中、3回にわたりサブエージェントが実際に`codex exec`を待たずに（「バックグラウンドタスクを起動して待つ」という趣旨の応答で）早期リターンする事象が発生した（Perspective C初回、hardening-gaps Round 2、Round 3の一部）。いずれも生raw outputファイルのサイズ検証（100バイト未満は失敗扱い）により検出し、メインエージェントが直接`codex exec`を同期的に再実行することで解決した。最終的にすべてのラウンドで実際のCodex出力を取得しており、捏造されたレビュー結果は本レポートに含まれていない。
