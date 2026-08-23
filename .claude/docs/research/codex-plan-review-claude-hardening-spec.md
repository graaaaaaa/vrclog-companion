# Codex計画レビュー: vrclog-companion Hardening Plan

**日時**: 2026-08-23
**判定**: APPROVE_WITH_CHANGES
**パースペクティブ数**: 3（アーキテクチャ / セキュリティ / 代替案）
**反復深化ラウンド数**: 3/5
**最終信頼度**: 0.85

## 総合評価

プランはアーキテクチャ的に健全であり、仕様書の11の問題を体系的に解決する。7フェーズの実装順序は依存関係を正しく反映している。3つのパースペクティブと3ラウンドの反復深化で特定された懸念は全て解決可能であり、プランの根本的な修正は不要。以下の5点の改善を組み込むことで、より堅牢な実装となる。

## パースペクティブ統合

### 一致点
- OnInsert error taxonomy: Broadcast/Enqueue は void であり、Apply のみが error source。Plan の fatal 化アプローチは正しい
- Conflict fatal exit: 正常な VRChat ログからは ObservationConflict は発生しない。Fatal 終了は integrity violation の正しい対応
- SourceRecord/DeliveryPhase: 最小かつクリーンなアプローチ。RecordSource interface 変更は妥当
- Schema v3 rejection: startup 前の fatal error のみ。API endpoint には到達しない
- 既存 SSE connection limiter (global=100, per-IP=20) で DoS 懸念は緩和済み

### 不一致点と解決
- **OnInsert error の分類**: Round 1 で3パースペクティブ全てが「projector failure と delivery failure の conflation」を指摘。Round 2 で Broadcast/Enqueue が void であることを確認し、懸念は解消。ただし運用診断のため `ErrProjectionFailure` を `ErrIntegrityViolation` と分離することを推奨
- **SourceRecord over-engineering**: Perspective C が指摘。Round 2 で「最小のアプローチ」と確認済み
- **Write deadline sufficiency**: Round 3 で `SetWriteDeadline` がハンドラ実行時間を制限しないことを確認。`http.TimeoutHandler` の追加を推奨

## 詳細所見

### アーキテクチャ妥当性
- Phase 2 の `run() error` パターンへの移行は正しい
- DeliveryPhase を Phase 2 で OnInsertFunc signature に追加し Phase 3 で使用する設計は、interface churn を最小化する
- Media correlation の時間窓追加は既存 `withinWindow` helper を再利用でき、低コスト
- clone helper の一元化（`cloneMediaAttempt`）は DRY 原則に沿う

### 実現可能性
- Go 1.25 の `http.NewResponseController` が利用可能で、route-level deadline は実現可能
- 上流依存は完了済み（ユーザー確認済み）
- **改善点**: `go get @latest` ではなく exact pseudo-version/tag を pin すべき

### セキュリティ & リスク
- `publicErrorMessage` に `ErrIntegrityViolation` → `"integrity violation"` の固定マッピングは計画済み
- Schema v3 rejection error は startup log のみに出力、API endpoint には到達しない
- ResourceResolved.Input 保存は既存の `resourceRolePriority` で制御され、media URL の外部露出は増えない
- Discord sanitization は Change type switch で allowlist 制御済み。Value 型化後も同じ switch を維持

### 代替案
- Perspective C の「Integrity-first minimal」（Phase 1+2 のみ先行）は価値あるが、仕様書が全フェーズを要求しているため分割は不適切
- 「Split projection from fanout」は Broadcast/Enqueue が void であるため不要
- 「Source-agnostic replay suppression」は interface churn 回避だが、SourceRecord の方がクリーン

### 考慮漏れ（プランに追加すべき項目）

1. **ErrProjectionFailure sentinel の分離**: `ErrIntegrityViolation`（DB integrity）と `ErrProjectionFailure`（post-commit Apply failure）を区別する。運用時の誤診断を防止
2. **Apply totality**: 未知 EventKind はデコード可能なら no-op、デコード不可なら失敗+診断情報（sequence, observation_id, type, remediation）
3. **Manager lifecycle guard**: Rebuild 前の Apply 呼び出しを防止する runtime guard（`ErrNotRebuilt`）
4. **Handler timeout**: `SetWriteDeadline` に加え、非SSE route に `http.TimeoutHandler` (15s) を適用
5. **Conflict 診断情報**: fatal log に adapter ID, rule ID, event kind, 既存/新規 canonical hash を含める

## 推奨アクション

1. **プランに反映すべき改善** (5件):
   - Phase 2: `ErrProjectionFailure` を `ErrIntegrityViolation` と別 sentinel にする
   - Phase 2: conflict fatal log に adapter ID, rule ID, canonical hash を含める
   - Phase 5: Manager に lifecycle guard 追加（Apply before Rebuild 防止）
   - Phase 6: `SetWriteDeadline` に加え `http.TimeoutHandler` を非SSE route に適用
   - Phase 4: `TestMedia_RebuildMatchesLive` に同一 OccurredAt の ambiguity edge case を追加

2. **そのまま維持** (変更不要):
   - OnInsert の fatal 化アプローチ
   - SourceRecord/DeliveryPhase 設計
   - Schema v3 rejection + DB path の error message
   - 7フェーズの実装順序
   - cloneMediaAttempt の deep copy 設計

## 深化ログ

| ラウンド | Codexへの質問内容 | 解決された項目 | 残った未解決事項 |
|----------|------------------|---------------|----------------|
| 1 | 3パースペクティブ並列（Architecture, Security, Alternatives） | file:line参照全件正確、基本アーキテクチャ妥当 | OnInsert conflation, conflict DoS, LogSnapshot TOCTOU, write deadline, SourceRecord over-eng |
| 2 | Error taxonomy (OnInsert scope, conflict crash-loop, post-commit recovery) + Edge cases (LogSnapshot, write deadline, schema UX, SourceRecord) | OnInsert は Apply のみ、conflict は正常ログから発生しない、schema v3 UX は safe、SourceRecord は最小 | Apply totality, log rotation gap, write deadline vs handler timeout |
| 3 | Adversarial: deterministic Apply failure, Rebuild/Apply race, write deadline sufficiency, correlation rebuild fidelity | Apply は fail-fast + 診断情報、lifecycle guard 推奨、TimeoutHandler 追加推奨、correlation は deterministic | なし |

## 実行モード

- **インタラクティブモード**: true
- **Phase 0 スコープ選択**: 実行（プランファイル / ディープ / バランス）
- **採用された Phase 0 デフォルト値**: 深度=3/5 ラウンド、重点=balanced、対象=プランファイル
- **Phase 4 次アクション確認**: 実行予定

## エスカレーション回答（インタラクティブモード時）

| Phase | 質問 | ユーザー回答 | 判定への影響 |
|-------|------|-------------|-------------|
| 0 | レビュー対象 | プランファイル | プランファイルを対象にレビュー |
| 0 | 深さ | ディープ | MIN=3, MAX=5 ラウンド |
| 0 | 重点観点 | バランス重視 | 全観点を均等にレビュー |
| 2c | (該当なし) | - | 矛盾・代替案・未解決なしでスキップ |
