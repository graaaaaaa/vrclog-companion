# Codex計画レビュー: vrclog-companion 全面刷新

**日時**: 2026-08-19
**判定**: APPROVE_WITH_CHANGES
**パースペクティブ数**: 3（アーキテクチャ / セキュリティ / 代替案）
**反復深化ラウンド数**: 3/5
**最終信頼度**: 0.86

## 総合評価

Observation Store + Projector アーキテクチャは仕様書に適合し、既存パッケージ境界とも整合する。全体方向は承認可能だが、source restart設計、Projector適用順序、SSE race prevention、route auth matrix、diagnostics redaction、Discord sanitization、MediaProjector境界条件について具体的な設計補強が必要。これらは設計の引き締め事項であり、アーキテクチャ全体の拒否理由ではない。

## パースペクティブ統合

### 一致点
- 全3視点が **APPROVE_WITH_CHANGES** (confidence 0.78) で一致
- Observation + Projector アーキテクチャは要件に適切
- MediaProjector は仕様書が主目的として明記しており、延期不可
- 既存セキュリティ基盤（loopback default, constant-time auth, HMAC SSE tokens, Secret masking）は堅固
- vrclog-go API は確認済みで計画と整合

### 不一致点と解決
- パースペクティブ間の重大な矛盾なし
- C（代替案）が提案した「Kernel-First Renewal」はユーザー判断により却下。仕様書が全機能を同一刷新で要求しているため

## 詳細所見

### アーキテクチャ妥当性
- **RecordSource restart**: Runner が `RecordSourceFactory` を所有し、fatal error 時に LatestCursor を再取得して新 RecordSource を生成する設計に確定
- **Projector 適用順序**: `Apply(obs)` 内で World → Presence reset → Media correlation reset → Changes 返却の順を厳守
- **Startup wiring**: Phase 8（Legacy削除）で扱うのは遅い。起動/停止の具体的コンストラクタ設計を Phase 3-4 で確定すべき
- **Schema v2 検出**: `user_version` チェック → 旧テーブル存在チェックの順序を厳密に（旧 migration は `CREATE TABLE IF NOT EXISTS` で `user_version` 未設定のため）

### 実現可能性
- vrclog-go, vrclog-adapters ともに最新版で存在確認済み
- `adapters.All()` は `[]vrclog.Adapter` を返し、YamaPlayer + iwaSync3 を含む
- Go 1.25 の `iter.Seq2` は Follow/ReadFile/AllObservations で適切に使用可能
- `internal/source` パッケージの分離は過剰 → `internal/ingest` 内に RecordSource interface を維持

### セキュリティ & リスク

**Route auth matrix（確定）**:

| Endpoint | Loopback | LAN Auth | Rate Limited |
|----------|----------|----------|-------------|
| /health | 不要 | 不要 | Yes |
| /observations | 不要 | Basic Auth | Yes |
| /state | 不要 | Basic Auth | Yes |
| /media/recent | 不要 | Basic Auth | Yes |
| /adapters | 不要 | Basic Auth | Yes |
| /stream | 不要 | Basic Auth or SSE Token | Yes |
| /config GET | 不要 | Basic Auth | Yes |
| /config PUT | 不要 | Basic Auth | Yes |
| /stats | 不要 | Basic Auth | Yes |
| /auth/token | 不要 | Basic Auth only | Yes |

**Diagnostics redaction（確定）**:
- DB保存前: raw line 除去、path を `<path:redacted>`、URL を host-only、query params strip、message 512 bytes cap
- API応答前: 二重防御として同じ redaction を再適用

**Discord sanitization（確定）**:
- `allowed_mentions: {parse: []}` で全 ping 無効化
- Markdown 文字 (`*_~\`|>[]()`) をエスケープ
- `@everyone`/`@here`/`<@id>` を zero-width break で中和
- URL テキストの自動リンクを `http[:]//` で中和

**STRIDE 脅威（High）**:
1. ローカル平文永続化（retention/purge 未定義）→ 仕様書が初期刷新での retention 設定追加を明示的に禁止しているため、data minimization で対応
2. LAN Basic Auth over HTTP（TLS なし）→ 明示的 LAN 有効化警告 + TLS proxy ドキュメント
3. 新 API route の auth 漏れ → 上記 route matrix で確定

### 代替案
- **Kernel-First Renewal** (confidence 0.86): 有力だが、仕様書が MediaProjector を主目的として明記しているため却下
- **Refetch-Oriented SSE MVP**: SSE の初期実装として検討可能だが、仕様書が Last-Event-ID recovery を要求しているため最終的には不採用
- **Query-Time Projection**: observation volume が少ないローカルアプリでは機能するが、in-memory projector の方が応答性能に優れ、仕様書の設計に合致

### 考慮漏れ（Round 2 で解決済み）
- **Duplicate conflict 比較フィールド**: `occurred_at, type, payload_json, adapter_id, rule_id, record_id, source_id, source_offset, source_line` の9フィールドを比較。`sequence, ingested_at` は除外
- **SSE highWater**: subscribe → highWater 読取 → DB backlog (lastSeq, highWater] → live (sequence > highWater)。race safe
- **Health 情報**: adapter count のみ（adapter IDs は認証必須の `/adapters` で提供）

## 反例シナリオ分析（Round 3）

| # | シナリオ | 安全性 | 対応 |
|---|---------|--------|------|
| 1 | 並行 ingest + API query | Safe | SQLite WAL が部分 commit を防止 |
| 2 | Projector crash recovery | Safe | Startup rebuild で復元。SSE は rebuild 中に送信しない |
| 3 | ログ rotation race | Partial | Follow が旧ファイル末尾を drip せず fatal → 未読 tail loss の可能性。vrclog-go の責務。cursor commit 済みの Record は重複チェックで安全 |
| 4 | Media correlation 境界 10.000s | Partial | `<` vs `<=` 未定義 → **`<=` に統一し、境界テスト追加** |
| 5 | 悪意ある表示名の Discord 通知 | Partial | Markdown/@ は中和済み。URL 自動リンクが残る → **`http[:]//` で中和追加** |
| 6 | 大規模 startup rebuild | Partial | データ整合性は安全。UX 上 `/health` を rebuild 前に開始し 503 返却。**`rebuilding` 状態を health に含める** |
| 7 | SSE 不明 Last-Event-ID loop | Partial | reset event 送信後に `id:` を空にして接続切断。**rate-limit 追加** |

## 推奨アクション

1. **RecordSourceFactory パターン採用**: Runner が fatal error 時に LatestCursor → 新 RecordSource 生成
2. **Projector 適用順序を明示**: World → Presence reset → Media reset → Changes
3. **Duplicate 比較フィールド定義**: 9 フィールド比較、sequence/ingested_at 除外
4. **SSE highWater アルゴリズム実装**: subscribe-first, backlog, live の3段階。`lastSent` で dedup
5. **Diagnostics 二重 redaction**: DB保存前 + API応答前
6. **Discord `allowed_mentions` + markdown escape + URL 中和**
7. **Media correlation window**: `<=` に統一、境界テスト追加
8. **Startup rebuild 中に /health 先行起動**: rebuilding 状態表示、他は 503
9. **SSE reset 時に id フィールド空送信 + 接続切断 + rate-limit**
10. **`internal/source` パッケージ不要**: RecordSource interface は `internal/ingest` 内に配置

## 深化ログ

| ラウンド | Codex への質問内容 | 解決された項目 | 残った未解決事項 |
|----------|------------------|---------------|----------------|
| 1 | 3パースペクティブ並列レビュー（アーキテクチャ/セキュリティ/代替案） | 全体方向 APPROVE_WITH_CHANGES、主要リスク特定 | RecordSource restart, Projector順序, route auth, diagnostics redaction, duplicate比較, SSE race, retention, Discord sanitization, over-engineering |
| 2 | アーキテクチャ5項目 + セキュリティ5項目の具体的解決 | RecordSourceFactory, Projector順序, 比較フィールド, SSE highWater, route matrix, redaction戦略, Discord sanitization, retention方針, health情報, over-engineering判定 | なし（全項目解決） |
| 3 | 7つの反例シナリオ（並行query, crash recovery, rotation race, 10s境界, 悪意名前, 大規模rebuild, SSE loop） | 3 Safe, 4 Partial → 5つの具体的な設計補強事項 | なし |

## 実行モード

- **インタラクティブモード**: true
- **Phase 0 スコープ選択**: 実行（計画ファイル / ディープ / バランス重視）
- **採用された Phase 0 デフォルト値**: 深度=3/5 ラウンド、重点=balanced、対象=計画ファイル
- **Phase 4 次アクション確認**: 実行予定

## エスカレーション回答

| Phase | 質問 | ユーザー回答 | 判定への影響 |
|-------|------|-------------|-------------|
| 0 | レビュー対象 | 計画ファイル | 自動検出スキップ |
| 0 | 深さ | ディープ (MIN=3, MAX=5) | 3ラウンド実行 |
| 0 | 重点観点 | バランス重視 | 全次元均等評価 |
| 2c | 代替案選択 | 現行計画を維持 | 仕様書どおり全機能実装 |
| 4 | 次アクション | 計画に反映 | 10件の推奨アクションを計画ファイルに統合して実装開始 |
