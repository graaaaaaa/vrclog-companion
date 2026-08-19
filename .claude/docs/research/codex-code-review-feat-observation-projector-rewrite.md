# Codexコードレビュー: feat-observation-projector-rewrite

**日時**: 2026-08-19
**総合判定**: CRITICAL
**パースペクティブ数**: 3（正確性 / セキュリティ / パフォーマンス）
**反復深化ラウンド数**: 3/5
**最終信頼度**: thorough（全所見を実コードで直接検証済み、幻覚なし）
**ベース**: main (5d312d87d877e75c9214f24ad2e77977ad6800bf)
**HEAD**: feat/observation-projector-rewrite (e2f0f02e1108cc3116a595f3784169d1f98dbbcb)
**変更ファイル数**: 122（10685 insertions, 6247 deletions）

## 総合評価

全面刷新実装（Observation/Projectorアーキテクチャ移行）+ 前回コードレビューの修正一式 + CI修正3コミットを対象にレビューを実施した。前回レビューで指摘されたCRITICAL/WARNING/SUGGESTIONの修正はおおむね正しく実装されているが、新たに**2件のCRITICAL**（本番配線でのbroadcast/Apply順序バグ、Discord送信失敗時のwebhook URL/トークン漏洩）と**8件のWARNING**を検出した。全ての所見は実際のソースコードを直接読んで検証しており、Codexの行番号指摘に幻覚がないことを確認済み。

## パースペクティブ統合

### 一致点
- SQLインジェクション、認証配線、Discord送信対象（World/Player Changeのみ）は健全
- 前回レビューで修正されたSSE backlogバグ、Observation conflict無限リトライ、`Manager.Rebuild`冪等性、config.goのエラーメッセージ露出は、いずれも正しく実装されていることを確認
- npm依存の脆弱性修正（`web/package-lock.json`）やCIワークフロー修正2件はアプリケーションコードへの機能的影響なし

### 不一致点と解決
| 所見 | Perspective A（正確性） | Perspective C（性能/設計） | Round 2での解決 |
|------|------------------------|---------------------------|-----------------|
| `main.go:136` broadcast/Apply順序 | CRITICAL | WARNING | **CRITICAL確定**（`ingest/runner.go`が全insert毎に呼ぶ本番配線のホットパスバグであり、edge caseではないため） |
| `stream.go:137-144` resolveLastEventID | CRITICAL | (未言及) | **WARNINGに格下げ**（`writeSSEReset`→フロントの`resync()`によって安全にフルリフェッチされ、サイレントなデータ損失にはならないため） |

## 所見一覧

### CRITICAL（マージ前に修正必須）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 1 | `cmd/vrclog-companion/main.go` | 136, 138 | ライブingestコールバック`onInsert`が`broadcaster.Broadcast(obs)`を`manager.Apply(obs)`より**先に**呼んでいる。CLAUDE.mdが明記する`CommitRecord → Projector Apply → SSE broadcast + Discord notification`の順序と逆転している。SSE/APIクライアントがイベント受信直後に`/api/v1/state`や`/api/v1/media/recent`を再取得すると、Projector未反映の古い状態を観測しうる。`internal/ingest/runner.go`が全insert毎にこのコールバックを呼ぶため理論上の問題ではなく本番のホットパスで発生する。統合テストのヘルパーはApply→Broadcastの正しい順序を使っているため、このバグはテストで検出されない。 | `manager.Apply(obs)`を先に実行し、成功時のみ`broadcaster.Broadcast(obs)`と`notifier.Enqueue`を呼ぶ順序に変更する。 | Perspective A（実コード確認済み: main.go:136/138） → Round 2で他パースペクティブとの矛盾を解消しCRITICAL確定 |
| 2 | `internal/notify/sender.go` | `Send`メソッド内、`http.NewRequestWithContext`/`client.Do`のエラーハンドリング | Discord送信失敗時（DNS失敗・接続拒否・TLSエラー・タイムアウト等、通常起こりうる一時的ネットワーク障害）、`s.logger.Warn("Discord request failed", "error", err)`が生の`err`をログ出力している。Goの`http.Client.Do`が返す`*url.Error`の`Error()`メソッドは`fmt.Sprintf("%s %q: %s", e.Op, e.URL, e.Err)`でリクエストURL全体を含む。Discord webhook URLは`https://discord.com/api/webhooks/{id}/{token}`の形式でトークンがパスに埋め込まれており、`stripPassword`はuserinfo部分のみ除去してパスは除去しない。`config.Secret`型による`[REDACTED]`保護をバイパスして、webhookトークンがログに平文で出力される。CLAUDE.mdの「Never log secrets」ルールに直接違反。 | `*url.Error`を`errors.As`でunwrapし、`ue.Op`と`ue.Err`のみをログに出す（URL自体は絶対にログしない）ヘルパー関数を実装し、`http.NewRequestWithContext`と`client.Do`両方のエラーログ箇所に適用する。 | Perspective B（自由記述で言及、構造化JSONには未収録） → Round 2で厳密検証（Go標準ライブラリの`url.Error`/`slog`挙動まで裏取り）しCRITICAL確定 |

### WARNING（修正推奨）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 3 | `cmd/vrclog-companion/main.go` / `internal/api/middleware.go` | main.go:194-216, middleware.go:106-130 | LANモード時、CSRF allowlistが`addr := fmt.Sprintf("%s:%d", host, *port)`（`host="0.0.0.0"`）から構築され、`csrfAllowedHosts = ["0.0.0.0:PORT"]`となる。`isAllowedHost`は`localhost`/`127.0.0.1`/`::1`のみ常時許可し、それ以外はallowlistとの完全一致を要求する。実際のLANブラウザが送る`Origin`は`http://192.168.x.y:PORT`のような実IPであり、`0.0.0.0`とは一致しない。結果として、同一LAN上の別端末からの`POST /api/v1/auth/token`（SSEトークン発行）や`PUT /api/v1/config`が**必ず403で拒否される**。LANモードの主要な用途（別端末からのアクセス）でSSEライブ更新と設定変更が機能しない。 | `csrfMiddleware`内で`r.Host`（実際にリクエストを受けたホスト）も許可集合に含めるか、`isAllowedHost`が`Host`ヘッダーの妥当性を別途検証した上でリクエストのHostを動的に許可する方式に変更する。バインドアドレス`0.0.0.0`をブラウザOrigin allowlistのエントリとして扱わない。 | Round 3 アドバーサリアルスイープ（実コード確認済み: main.go:194-216, middleware.go:106-130） |
| 4 | `internal/notify/notifier.go` | 282-296 | `flush()`が`n.queue`を送信前に即座にクリアする（`changes := n.queue; n.queue = make(...)`）。`handleSendResult`は`SendRetryable`時に`backoffUntil`を更新するのみで、失敗した`changes`をキューに戻す処理がない。Discord一時障害（429/5xx/ネットワークエラー）が発生すると、そのバッチの通知（World/Player Change）が再送されずに恒久的に失われる。バックオフタイマーは設定されるが、次回`flush`実行時にはキューが空のため何も送信されない。 | 送信成功が確定するまで`n.queue`をクリアしない、または`SendRetryable`時に未送信分を`n.queue`の先頭に戻し`backoffUntil`に合わせて再flushをスケジュールする。`SendOK`または意図的なfatal無効化の場合のみキューをクリアする。 | Round 3 アドバーサリアルスイープ（実コード確認済み: notifier.go:282-296） |
| 5 | `internal/api/stream.go` | `handleStream`, `resolveLastEventID` (137-144) | `resolveLastEventID`が、一時的なストアエラー（`ObservationByID`のDB読み取りエラー等）と「未知/期限切れカーソル」を区別せず同じ`event: reset`を送信する。`writeSSEReset`自体はフロントエンド（`useSSE.ts`）が`resync()`で安全にフルリフェッチするため即座のデータ損失にはならない（CRITICALではなくWARNING、Round 2で確定）ものの、一時的なDB障害で正当なLast-Event-IDカーソルを不必要に破棄してしまう誤ったエラーハンドリングではある。 | `found == nil`の場合のみreset送信、`err != nil`の場合はresetを送らずストリームを終了し、クライアントが同じカーソルで再試行できるようにする。 | Perspective A（実コード確認済み: stream.go:137-144） → Round 2でCRITICALからWARNINGへ severity 修正 |
| 6 | `internal/notify/payload.go` | 186 | URL自動リンク無効化用の正規表現`var urlSchemePattern = regexp.MustCompile(`(https?):/{2}`)`が大文字小文字を区別する。この関数が処理する`sanitizeDiscordText`はVRChatのプレイヤー名/ワールド名など攻撃者が影響を与えられる入力を対象とするため、`HTTP://`や`HTTPS://`のような大文字バリアントはゼロ幅スペース挿入によるDiscord自動リンク無効化をバイパスしうる。 | 正規表現に`(?i)`フラグを追加: `regexp.MustCompile(`(?i)(https?):/{2}`)` | Perspective B（自由記述） → Round 2で攻撃者制御入力への到達性を確認しWARNING確定 |
| 7 | `web/src/pages/History.tsx` | 74-93 | 前回修正で追加した`maxRetainedObservations=1000`の上限に達した後も、「Load more」ボタンはAPI側の`next_cursor`（サーバー側のページング残量）に基づいて表示され続ける。クリックするとカーソルは進み新しいページを取得するが、`merged.slice(0, maxRetainedObservations)`で新しく取得した古い行が即座に破棄されるため、UIには何の変化も現れず、無駄なネットワークリクエストだけが発生し続ける。 | 上限到達時に`hasMore`をfalseに固定する（直近N件のみ表示するビューとして確定させる）か、全履歴閲覧が必要な場合は仮想スクロール方式へ移行する。 | Perspective A（実コード確認済み: History.tsx:74-93） |
| 8 | `internal/store/stats.go` | 47-53 | `GetBasicStats(ctx, since, until)`内の`RecentPlayers`クエリ（`SELECT DISTINCT ... ORDER BY sequence DESC LIMIT 5`）が`since`/`until`の時間窓を一切適用していない、全履歴を対象にした直近5名のグローバルクエリになっている。`/api/v1/stats/basic`は「今日の統計」を返す想定（CLAUDE.md）だが、今日の参加者が5名未満の場合、過去の日付の参加者が「今日の直近プレイヤー」として表示されうる。 | `RecentPlayers`クエリにも`WHERE occurred_at >= ? AND occurred_at < ?`を適用し、`GetBasicStats`が受け取る`since`/`until`と一貫させる。 | 直接ソース確認（実コード確認済み: stats.go:47-53） |
| 9 | `web/src/hooks/useSSE.ts` | `connect()`, 78-83 | `connect()`内で`fetchAndSetToken()`が失敗すると（例: 起動直後の`Server.SetReady(false)`によるProjector再構築中の503レスポンス）、`setError('Failed to authenticate')`を設定して即座にreturnし、`EventSource`が一度も生成されない。バックオフ/再接続ロジックは`es.onerror`ハンドラ内にあるが、`EventSource`自体が存在しないため発火しない。`useEffect`は`enabled`が変化しない限り再実行されないため、初回のトークン取得が起動直後の readiness gate ウィンドウに当たって失敗すると、SSE接続が永久に確立されずページの手動リロードが必要になる。 | `connect()`内でトークン取得失敗時にも`onerror`と同様のバックオフ付き再試行（`reconnectTimerRef`経由の`setTimeout(connect, delay)`）をスケジュールする。 | 直接ソース確認（実コード確認済み: useSSE.ts:53-83） |
| 10 | `internal/api/stream.go` | `handleStream`（SSE接続受付部） | 認証済みSSE接続数に上限がなく、グローバル/IP単位の同時接続数キャップが存在しない。レートリミッターはリクエスト開始レートのみを制限し、アクティブなストリーム数は制限しない。大量の同時接続または読み取りの遅いクライアントが、goroutineとbroadcasterのsubscriberチャネルをリソース上限なく消費しうる。 | Subscribe前にグローバル/IP単位のアクティブSSE接続数上限を設け、超過時は429/503で拒否する。`http.ResponseController`を使った書き込みデッドラインの導入も検討。 | Round 3 アドバーサリアルスイープ |
| 11 | `CLAUDE.md` | 93 | 「Permanent Architectural Rules」節が依然として「Last-Event-ID recovery uses the Broadcaster's high-water sequence」と記述しているが、実装は既に`Store.LatestSequence()`（DBベース）を使用するよう修正済み。ドキュメントが古いままだと、将来の実装者が再起動直後にbacklogが欠落するバグを再導入するリスクがある。 | CLAUDE.mdの該当箇所を`Store.LatestSequence()`ベースの実装に合わせて更新する。 | Perspective A（自由記述） → 直接確認済み |

### SUGGESTION（改善提案）

| # | ファイル | 行 | 問題 | 修正案 | 検出元 |
|---|---------|-----|------|--------|--------|
| 12 | `internal/ingest/status.go` | `redactPaths`のフォールバック正規表現 | 非`fs.PathError`のエラー文字列にスペースを含む絶対パスがある場合、正規表現がスペースで途切れ一部のパス断片（例: ` Logs`）が残る可能性がある。ただしRound 2の追跡調査で、この関数の実際の呼び出し経路（`vrclog.Follow`のエラー型、`CommitRecord`のリトライ/conflict経路）ではこのケースに現実的に到達しないことを確認済み。 | 防御的観点からのハードニングとして、`fs.PathError`以外はallowlist方式の固定メッセージ（sentinel error毎の専用文字列）に置き換えることを検討。現時点で緊急性はない。 | Perspective B（自由記述） → Round 2で到達不能と判定、severityをWARNINGからSUGGESTIONへ格下げ |
| 13 | `internal/sse/broadcaster.go` | 113-115 | `HighWaterSequence`のdocコメントが「Used by the SSE handler to bound DB backlog delivery and avoid a subscribe/live race」と記述しているが、実際のbacklog境界には`Store.LatestSequence()`が使われており、この関数は現在テスト以外で使われていない。古いコメントが将来の回帰リスクを生む。 | コメントを削除するか、テスト専用であることを明記し、SSE backlogには`Store.LatestSequence`を使うべき旨を明示する。 | Perspective C（自由記述） → 直接確認済み |

### 検討したが対応不要と判断した項目

- **`cmd/vrclog-companion/main.go:65-72`（パスワードファイル書き込み失敗時のコンソール出力フォールバック）**: 初回起動時、LAN Basic Authパスワードのファイル書き込みが失敗した場合のみコンソールに表示する、意図的なローカルデスクトップアプリのUXパターン（Grafana/Portainer等の初回起動時認証情報表示と同様）。CLAUDE.mdの「Never log secrets」ルールは`Secret`型による継続的な構造化ログ/config応答を対象としており、この一度限りの対話的フォールバックとは性質が異なる。Round 2で厳密検証しACCEPTABLE_AS_ISと判定（ACCEPTABLE_AS_IS）。
- **`internal/store/query.go`のsince/until索引ミスマッチ**: 既存レビューラウンドで対応不要と判定済み（sequence不透明カーソル設計を変更する影響の大きい変更のため）。今回のスイープでも新規の実害証拠は見つからず、再指摘なし。
- **媒体URL漏洩経路の全面確認**: `internal/projector/media.go`、`internal/notify/*`、`web/src/components/SafeLink.tsx`を追跡した結果、媒体URLがDiscordペイロード構築前にフィルタされていること、UIリンクは`SafeLink`経由の明示的ユーザークリックのみで開かれることを確認し、漏洩経路は発見されなかった。

## 深化ログ

| ラウンド | Codexへの質問内容 | 解決された項目 | 残った未解決事項 |
|----------|------------------|---------------|----------------|
| 1 | 3パースペクティブ並列初期レビュー（正確性/セキュリティ/パフォーマンス） | CRITICAL 2件・WARNING 2件・SUGGESTION 2件を検出。技術的トラブル（`codex exec`のstdinハング、ゾンビプロセスによるファイル競合）を検知・対処し、全出力を実コードで幻覚チェック済み | broadcast/Apply順序のseverity矛盾（A: CRITICAL, C: WARNING）、B自由記述のwebhook URL漏洩候補の要検証、resolveLastEventIDのCRITICAL妥当性 |
| 2 | severity矛盾解消、webhook URL漏洩の厳密検証、path redaction到達可能性検証 | broadcast/Apply順序→CRITICAL確定、resolveLastEventID→WARNINGへ格下げ、webhook URL漏洩→CRITICAL確定（Go標準ライブラリ挙動まで裏取り）、パスワードコンソールフォールバック→問題なし確定、URL scheme正規表現→WARNING確定、path redaction→到達不能でSUGGESTIONへ格下げ | なし（全項目解決） |
| 3 | 最終アドバーサリアルスイープ（並行性・リソース枯渇・媒体URL漏洩・認証境界・CI修正の機能影響） | 新規WARNING3件を発見（SSE無制限接続、LANモードCSRF host不一致、Discord通知バッチロスト）。媒体URL漏洩経路なし、CI修正3件はアプリケーションコードに機能影響なしを確認 | なし（`more_rounds_needed: false`、全項目収束） |

## 実行モード

- **インタラクティブモード**: true
- **Phase 0 スコープ選択**: 実行（差分範囲=ベースブランチ差分 / 深さ=ディープ(MIN=3,MAX=5) / 重点=バランス重視 / severity=通常基準）
- **採用された Phase 0 デフォルト値**: 深度=3/5 ラウンド、重点=balanced、差分範囲=base-branch、severity=normal
- **Phase 4 次アクション確認**: 実行済み

## 対応状況（修正実装）

ユーザー指示「WARNING/SUGGESTIONも含めて全て修正」に基づき、CRITICAL 2件・WARNING 8件・SUGGESTION 2件の計13件を全て実装。品質ゲート（`gofmt`, `go vet`, `go build`, `go test ./...`, `go test -race ./...`, integration, e2e, `GOOS=windows go build`, `npm run lint`, `npm run build`）を全て通過済み。

| # | severity | 対応 | 実装内容 |
|---|----------|------|---------|
| 1 | CRITICAL | 修正済み | `cmd/vrclog-companion/main.go` の `onInsert` で `manager.Apply` を `broadcaster.Broadcast` より先に実行するよう順序変更 |
| 2 | CRITICAL | 修正済み | `internal/notify/sender.go` に `safeRequestErr` を追加し、`*url.Error` をunwrapしてURL自体をログに出さないよう変更 |
| 3 | WARNING | 修正済み | `internal/api/middleware.go` の `csrfMiddleware` が `r.Host` も許可ホスト集合に含めるよう変更。`TestCSRFMiddleware_AllowsLANOriginMatchingRequestHost` 等を追加 |
| 4 | WARNING | 修正済み | `internal/notify/notifier.go` に `requeue` を追加し、`SendRetryable` 時にバッチをキューへ戻すよう変更。`TestNotifier_RetryableFailureResendsAfterBackoff` を追加 |
| 5 | WARNING | 修正済み | `internal/api/stream.go` の `resolveLastEventID` を、`found == nil` の場合のみreset送信するよう変更。`TestResolveLastEventID_*` を追加 |
| 6 | WARNING | 修正済み | `internal/notify/payload.go` の `urlSchemePattern` に `(?i)` フラグを追加。`TestPayload_SanitizesUppercaseURLScheme` を追加 |
| 7 | WARNING | 修正済み | `web/src/pages/History.tsx` で上限到達時に `hasMore` をfalseにするよう変更 |
| 8 | WARNING | 修正済み | `internal/store/stats.go` の `RecentPlayers` クエリに `since`/`until` フィルタを追加。`TestGetBasicStats_RecentPlayersRespectsWindow` を追加 |
| 9 | WARNING | 修正済み | `web/src/hooks/useSSE.ts` の `connect()` でトークン取得失敗時にもバックオフ付き再試行をスケジュールするよう変更 |
| 10 | WARNING | 修正済み | `internal/api/sselimit.go` を新設し、グローバル/IP単位のSSE同時接続数上限（100/20）を追加。`TestSSEConnLimiter_*` を追加 |
| 11 | WARNING | 修正済み | `CLAUDE.md` のSSE Last-Event-ID recovery の記述を `Store.LatestSequence()` ベースに更新 |
| 12 | SUGGESTION | 修正済み | `internal/ingest/status.go` に既知の `vrclog.Follow` sentinel error 用のallowlistを追加（防御的ハードニング）。`TestPublicErrorMessage_KnownSentinelsGetFixedMessages` を追加 |
| 13 | SUGGESTION | 修正済み | `internal/sse/broadcaster.go` の `HighWaterSequence` のdocコメントを実装に合わせて更新 |

## エスカレーション回答（インタラクティブモード時）

| Phase | 質問 | ユーザー回答 | 判定への影響 |
|-------|------|-------------|-------------|
| 0 | 差分範囲/深さ/重点観点/判定厳しさ | ベースブランチ差分 / ディープ(MIN=3,MAX=5) / バランス重視 / 通常基準 | Round数=3、focus均等、severity変換なしで確定 |
| 4 | レビュー結果 CRITICAL（2件）です。次にどうしますか？ | WARNING/SUGGESTIONも含めて全て修正 | CRITICAL 2件・WARNING 8件・SUGGESTION 2件、計13件全てを実装対象とする |

Phase 2c のエスカレーション条件（severity不一致・複数未評価代替案・ラウンド上限到達）は、broadcast/Apply順序の矛盾のみ該当したが Round 2 の反復深化で解消されたため、ユーザーへの追加確認は発生しなかった。

## 不確実性

- Codexサブエージェントの一部（`codex exec`実行担当）が、指示に反して`nohup`によるデタッチプロセスを使用したり、旧い出力ファイルへの書き込み競合を起こしたりする技術的トラブルが複数回発生した。全てのケースで、汚染された可能性のある出力は破棄し、隔離されたクリーンな再実行の結果のみを採用し、かつ全ての最終所見を実際のソースコードに対して直接検証することで、レポートの正確性を担保した。
- Codexのサンドボックス環境（read-only、ネットワーク遮断）内では`go test ./...`の一部実行やnpm auditの実行ができなかった旨の報告があったが、これらは本セッション内で別途全て実行し pass 済み（`/codex-plan-review`後の実装時、および先行するCI修正作業時に確認済み）。
