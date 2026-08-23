# vrclog-companion データ整合性・Media相関強化 指示書兼実装仕様書

## 0. この文書の扱い

この文書は `github.com/vrclog/vrclog-companion` の刷新後アーキテクチャを、実運用可能なデータ整合性へ仕上げるための最上位実装仕様である。

現在の責務分離は維持する。

```text
RecordSource
    ↓
Engine.Process
    ↓
Store.CommitRecord
    ├─ Observations
    ├─ Diagnostics
    └─ Cursor
    ↓
Projector Manager
    ├─ World
    ├─ Presence
    └─ Media
    ↓
API / SSE / UI / Notification
```

互換性維持、旧DB migration、deprecated APIは不要である。

ただし、Observationをsource of truthとし、ProjectorをDBから完全再構築可能にする現在の思想は崩さない。

## 1. 前提となる上流契約

この作業は次の順序で上流が完成した後に行う。

```text
vrclog-go
  → Observation IDからemission index削除
  → Rule ID一意制約
  → Follow修正
  → canonical validation
  → LogSnapshot API

vrclog-adapters
  → 新coreへ追従
  → All()削除
  → YamaPlayer/iwaSync3 error semantics修正
```

上流が未完成の場合、Companion内にコピー型、互換ID生成、独自LogSnapshotを作らない。

最終依存方向：

```text
vrclog-go ← vrclog-adapters ← vrclog-companion
```

一時的なlocal開発には`go.work`を使ってよいが、`go.mod`へ`replace ../...`をcommitしない。

## 2. 今回解決する問題

1. Observation conflict時、競合Observationをdropしてcursorを進めている。
2. source retry counterが正常Record処理後もresetされない。
3. 初回起動・再接続時の過去RecordがSSE/Discordの新着side effectを発生させ得る。
4. Media Projectorのexact target / exact URL相関に時間上限がなく、別再生を永久統合する。
5. `ResourceResolved.Input`がAttemptへ保存されず、元URLを失う。
6. ProjectorのChange/Snapshotに内部pointerが漏れ、後続mutationやraceの余地がある。
7. API serverのready既定値がcommentと逆。
8. SSEのためglobal WriteTimeoutを0にし、通常endpointも無制限になっている。
9. Adapter aggregate APIに依存し、community support setが暗黙に増える。
10. Observation ID変更に対するDB schema境界が必要。
11. CIにrace gateがなく、release workflowがfull testを通さずartifactを公開できる。

## 3. 絶対に維持する不変条件

### 3.1 Store

- 1 RecordのObservation、Diagnostic、cursorは同一SQLite transactionでcommitする。
- transaction失敗時、cursorを進めない。
- 同一Observation IDでcanonical fieldsが完全一致する場合だけduplicateとして受理する。
- 同一IDで内容が異なる場合はintegrity failureであり、どちらかを捨てない。
- commit後にのみProjector/SSE/notificationへ渡す。

### 3.2 Projector

- DB Observationだけからdeterministicにrebuildできる。
- semantic correlation結果をsource of truthとしてDBへ保存しない。
- ambiguousなMedia相関では新しいAttemptを作る。
- World transitionを跨いでAttemptをmergeしない。
- API/Changeへ内部mutable stateを返さない。

### 3.3 delivery side effect

- Catch-up ObservationもStoreとProjectorへ反映する。
- Catch-up ObservationをSSE broadcastしない。
- Catch-up由来ChangeをDiscord等へ通知しない。
- Live Observationだけが外部side effectを発生させる。
- Media URLは従来どおりDiscord通知対象外とする。

## 4. 依存更新とDB schema

## 4.1 上流dependency

完成済みの以下へ更新する。

- `github.com/vrclog/vrclog-go`
- `github.com/vrclog/vrclog-adapters`

`go.mod`へ公開commit/versionを記録する。

## 4.2 Adapter構成を明示する

`internal/adapter/adapter.go`からroot aggregate importを削除する。

推奨構成：

```go
import (
    vrclog "github.com/vrclog/vrclog-go"
    "github.com/vrclog/vrclog-adapters/iwasync3"
    "github.com/vrclog/vrclog-adapters/yamaplayer"
)

func BuildEngine() (...) {
    core := vrclog.NewVRChatAdapter()
    community := []vrclog.Adapter{
        yamaplayer.New(),
        iwasync3.New(),
    }
    // core → YamaPlayer → iwaSync3の順を明示
}
```

新Adapterはこのfileの明示差分とreviewなしに有効化されない。

## 4.3 schema versionを3へ上げる

Observation ID公式が変わるため、既存schema version 2 DBをそのまま使わない。

```go
const CurrentSchemaVersion = 3
```

- 自動migrationは実装しない。
- version 2は明確な`ErrUnsupportedSchema`として拒否する。
- error messageに、停止後DB fileをrename/deleteして再起動する手順を含める。
- schema table構造が同じでも、identity contractが違うためversionを上げる。
- README/SPECへ理由を記載する。

## 5. Ingest integrity

## 5.1 Observation conflictをfatalにする

以下を完全削除する。

```text
diagnosticCodeObservationConflict
dropConflictingObservation
conflict Observationだけを除去して再commitする処理
```

`store.CommitRecord`の現在のatomic transactionとcontent comparisonは維持する。

`ObservationConflictError`発生時：

1. transactionはrollback済みであること。
2. cursorは進まないこと。
3. Runnerは同じResultを改変しないこと。
4. transient DB retryを行わないこと。
5. source retryへ落とさないこと。
6. `StateFailed`へ移行すること。
7. errorを`Runner.Run`から返すこと。
8. processはcontrolled shutdownし、終了code 1にすること。

新しいsentinel/typeを追加してよい。

```go
var ErrIntegrityViolation = errors.New("ingest integrity violation")
```

error chainから`ObservationConflictError`とObservation IDを内部logで確認できること。ただしunauthenticated health responseへpath、URL、payloadを出さない。

## 5.2 fatal / retryable分類

### Fatal

- Observation content conflict
- committed ObservationをProjectorへapplyできない内部不整合
- canonical payload decode failure during live insert
- schema/invariant violation

### Retryable

- source open/stat/list等の一時I/O error
- transient SQLite busy/I/O error
- source iteratorの一時終了

context cancellationはclean shutdown。

## 5.3 OnInsertをerror-awareにする

現在のvoid callbackを変更する。

推奨契約：

```go
type OnInsertFunc func(
    ctx context.Context,
    phase DeliveryPhase,
    obs observation.StoredObservation,
) error
```

- Projector Apply失敗をwarningだけで無視しない。
- DB/cursorは既にcommit済みなのでrollbackできない。
- callback failure時はRunnerをfatal停止する。
- 再起動時にDB rebuildして一貫状態へ戻せる。
- SSE/NotifierはProjector Apply成功後だけ実行する。

## 5.4 mainをcontrolled shutdown可能にする

`log.Fatalf`とgoroutine内log-only errorへ依存しすぎない。

推奨：

```go
func main() {
    if err := run(); err != nil {
        log.Printf("Fatal: %v", err)
        os.Exit(1)
    }
}

func run() error {
    // defer release/db close等
    // server/runner errorを共通error channelへ
    // cancel → notifier → SSE → HTTP → DBの順でshutdown
}
```

Runner fatal errorを受けた場合：

- errorをmain supervisorへ送る。
- context cancel。
- notifier、SSE、HTTPをgraceful stop。
- deferによるDB/lock releaseを実行。
- non-zeroで終了。

server errorも同じshutdown pathを使う。`os.Exit`をdefer実行前に呼ばない。

## 5.5 source retry backoffをprogress後にresetする

`runSource`は最低限次を返すようにする。

```go
lastCursor *vrclog.Cursor
madeProgress bool
err error
```

1 Record以上を正常commitしたsource runはprogressありとする。

- progressがあればouter source retry `attempt = 0`へresetする。
- DB retry counterはRecord commit成功ごとに0へresetする現在の挙動を維持。
- source factory成功だけではprogress扱いにしない。

テスト：

```text
source failure ×3
→ backoff増加
→ Record 1件commit
→ 次のsource failureはinitial delayへ戻る
```

## 6. Catch-up / Live delivery phase

## 6.1 目的

初回起動、app再起動、source再接続時に、source開始前からdiskに存在したログをDB/Projectorへbackfillしつつ、新着通知として扱わない。

timestamp比較は使用しない。`Record.Time`はログ内容の時刻でありdelivery境界ではない。

## 6.2 source item型

RecordSourceがphaseを伝えるよう変更する。

推奨契約：

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
```

名前は変更してよいが、phaseはRecordごとに確定し、Storeへ保存する必要はない。

## 6.3 LogSnapshotの使用

`RecordSourceFactory.NewSource`を呼ぶたびに、Follow開始前にcore APIでsnapshotをcaptureする。

概念：

```go
snapshot, err := vrclog.CaptureLogSnapshot(cfg.LogDir)
if err != nil {
    return nil, err
}
```

各Record：

```go
phase := DeliveryLive
if snapshot.Contains(record) {
    phase = DeliveryCatchUp
}
```

意味：

- source開始前から存在したbytesに完全に含まれるRecordはcatch-up。
- capture後の追記範囲はlive。
- capture後に作られたfileはlive。
- capture時に未改行だった行が後で完成し、NextOffsetがcapture sizeを超える場合live。
- no log filesでcaptureした場合、将来のRecordはすべてlive。

source retryごとに新snapshotをcaptureする。retry中に蓄積したRecordはcatch-up扱いとし、復旧時の通知burstを防ぐ。

cursor missing fallbackでも同じsnapshotを使用してよい。fallbackのために独自timestamp判定を追加しない。

## 6.4 phaseごとの処理

Store commitはphaseに依存しない。

```text
Catch-up:
  Store          YES
  Cursor         YES
  Projector      YES
  SSE            NO
  Discord        NO

Live:
  Store          YES
  Cursor         YES
  Projector      YES
  SSE            YES
  Discord        Change filterに従う
```

`manager.Apply`は両phaseで呼ぶ。

```go
changes, err := manager.Apply(obs)
if err != nil { return fatal }

if phase == DeliveryLive {
    broadcaster.Broadcast(obs)
    notifier.Enqueue(changes...)
}
```

duplicate Observationで`InsertedObservations`が空ならside effectなし。

### readiness

今回、HTTP readyは「DB schema検証とDBからのProjector rebuildが完了した」ことを意味する。catch-up完了待ちを必須にしない。

- catch-up中もstate endpointは徐々に最新化してよい。
- healthへphaseを追加する場合は正確に終了判定できる仕組みなしに`catching_up`へ固定しない。
- notification suppressionをreadinessと混同しない。

## 7. Media Projector相関

## 7.1 基本原則

- Playback targetの同一性とPlayback attemptの同一性は別である。
- exact URLまたはexact target一致だけで無期限にmergeしない。
- 全matchにtime windowとworld session境界を適用する。
- ambiguityでは新Attempt。
- 誤mergeより分離を優先する。

定数：

```go
const (
    mediaCorrelationWindow      = 10 * time.Second
    mediaSourceDuplicateWindow = 2 * time.Second
)
```

時間比較はboundary inclusiveとし、rebuild/liveで同じ関数を使う。

## 7.2 helper API

以下のようにoccurredAt/windowを必須引数にする。

```go
findByExactTarget(target, occurredAt, window)
findByExactURL(url, occurredAt, window)
findSingleRecentCandidate(target, occurredAt, window)
```

現在の時間無制限helperを残さない。

candidateは必ず：

- current world session内
- `abs(candidate.LastObservedAt - occurredAt) <= window`
- target conflictなし

を満たす。

## 7.3 source URL event

`ResourceRoleSource`は原則、新しいAttemptを開始する。

既存Attemptへmergeしてよいのは、次をすべて満たすduplicate burstだけ。

- exact same URL
- 2秒以内
- targetが互換
- candidateがcurrent world session
- source URLが競合しない

同じtargetでもURLが異なれば新Attempt。

同じURLでも5分後なら新Attempt。

## 7.4 resolver/playback URL event

`resolver_input` / `playback_input`等は次の順でmatchする。

1. exact target、10秒以内、target conflictなし
2. exact URL、10秒以内、target conflictなし
3. 10秒以内のcandidateがちょうど1件
4. それ以外は新Attempt

優先順位をtestで固定する。

## 7.5 ResourceResolved

相関候補：

1. Input URL、10秒以内
2. exact target、10秒以内
3. Output URL、10秒以内
4. exactly one recent candidate
5. 新Attempt

相関後、**InputとOutputの両方**をResourcesへ追加する。

- InputのKind/Role/URLをそのまま保持する。
- OutputのKind/Role/URLをそのまま保持する。
- 同一Observation ID由来の2 Resourceであることが分かるよう、同じAdapterID/RuleID/ObservationID/ObservedAtを持たせてよい。
- Outputを強制的に別Roleへ上書きしない。canonical EventのRoleを尊重する。

この変更により、`ResourceResolved`だけが存在する場合でもInput URLがBestOpenableURL候補になる。

## 7.6 MediaError

相関順：

1. Resource URLがあればexact URL、10秒以内
2. exact target、10秒以内
3. exactly one recent candidate、10秒以内
4. 新Attempt

ambiguousな2候補がある場合、片方へ推測mergeしない。

Error Resourceがある場合、AttemptのResourcesへも追加するか、Error内で保持するかを一貫して決める。少なくともBestOpenableURL選択に利用でき、rebuildで同じ結果になること。

## 7.7 BestOpenableURL

優先順位は維持する。

```text
source
resolver_input
playback_input
```

`resolved`、thumbnail、metadataは通常open candidateにしない。

- URLはHTTP/HTTPSのみopenable。
- signed CDN URLがsource URLを上書きしない。
- 同priorityでは最初に観測したURLを安定して維持してよい。
- `ResourceResolved.Input`を候補に含める。

## 7.8 必須相関テスト

1. 同じURLを5分後に再生 → 2 Attempt。
2. 同じtarget keyで30秒後に別URL → 2 Attempt。
3. 同じsource URLが1秒以内に重複 → 1 Attempt。
4. 同じURLが3秒後にsourceとして再入力 → 2 Attempt。
5. Yama source → VRChat resolver → AVPro errorが10秒内 → 1 failed Attempt。
6. 2 playerが交互に再生 → 混線しない。
7. exact targetが同じでもURL conflict → source eventは新Attempt。
8. ambiguous recent candidate 2件 → 新Attempt。
9. world transition後の同じURL → 新Attempt。
10. `ResourceResolved`単独 → InputがBestOpenableURL、Outputはdetailsのみ。
11. rebuildとlive Applyで完全に同じAttempts。

## 8. Projectorのimmutability

## 8.1 Change

Projector内部のpointerをChangeへ直接入れない。

推奨：

```go
type MediaAttemptUpdated struct {
    Attempt MediaAttempt
    At      time.Time
}
```

World Changeも内部state pointerではなくcopyを返す。

```go
type WorldChanged struct {
    Current  CurrentWorld
    Previous *CurrentWorld // 必要ならdeep copy
    At       time.Time
}
```

既存API互換は不要。

## 8.2 clone helper

`cloneMediaAttempt`等の一元helperを作り、以下をdeep copyする。

- `Target`
- `Resources`
- `Errors`
- `ObservationIDs`
- `AdapterIDs`

`recentSnapshot`、`MediaAttemptUpdated`、API DTO変換は同じclone contractを使う。

## 8.3 テスト

- Change内AttemptをcallerがmutationしてもManager stateが変わらない。
- `RecentMedia()`結果のTarget、sliceをmutationしてもManager stateが変わらない。
- SnapshotのWorld/LatestをmutationしてもManager stateが変わらない。
- concurrent Apply/Snapshot/RecentMediaを`go test -race`で検証する。

## 9. API server

## 9.1 ready既定値

`NewServer`の既定はcommentどおりnot-readyにする。

```go
s.ready.Store(false)
```

startup ownerがrebuild完了後に`SetReady(true)`する。

constructor testを追加する。

## 9.2 Write timeout

SSEのためserver-global `WriteTimeout=0`を維持する場合、通常endpointへroute-level write deadlineを設定する。

推奨：

- non-SSE route: 15秒のwrite deadline
- SSE route: deadlineなし
- `http.NewResponseController(w).SetWriteDeadline(...)`または等価な方法

SSE connectionを15秒で切断してはならない。

別serverへ分けてもよいが、必要以上に構造を増やさない。

テスト：

- 通常handlerは有限deadline下で動く。
- SSE handlerは長時間接続可能。
- existing Last-Event-ID replayが壊れない。

## 10. Status / health

`StateFailed`を追加する。

```go
const (
    StateRunning
    StateRetrying
    StateStopped
    StateFailed
)
```

- fatal integrity errorでFailed。
- context cancellationでStopped。
- transient source/DB errorでRetrying。
- record commit成功でRunning。

unauthenticated healthへ以下を出さない。

- filesystem path
- URL
- payload
- DB contents

既存redactionと最大長を維持する。新しい`ErrSourceTruncated`等を安全な固定messageへmappingする。

## 11. CI・release・依存整理

## 11.1 race job

CIへUbuntu race jobを追加する。

Web embedが必要なら先にweb build/copyを行う。

```bash
go test -race -count=1 ./...
```

Projector concurrent testを含める。

## 11.2 release gate

tag pushだけで未検証artifactを公開しない。

release workflowに`verify` jobを追加し、少なくとも次を実行する。

```text
npm ci
npm run build
npm run lint
gofmt check
go vet ./...
go test ./...
go test -race ./...
go test -tags=integration ./test/integration/...
go test -tags=e2e ./test/e2e/...
```

build/release jobはverify成功に依存する。

既存CIをreusable workflow化して呼び出してもよい。

## 11.3 module cleanup

```bash
go mod tidy
go mod why -m <suspected module>
```

未使用の旧tailer関連間接依存が消えることを確認する。単に手作業でgo.sumを削らず、`go mod tidy`の結果を使う。

## 12. E2E仕様

最低限、以下を自動化する。

### 12.1 URL recovery

入力Observation sequence：

```text
YamaPlayer source YouTube URL
VRChat resolver input/resolve
AVPro playback input
YamaPlayerまたはAVPro error
```

期待：

- 1 Media Attempt。
- Status failed。
- BestOpenableURLは元YouTube URL。
- resolved/signed URLはdetailsへ残るがBestではない。

### 12.2 ResourceResolved only

- Input YouTube/watch URL。
- Output signed media URL。
- 先行URL Observationなし。

期待：InputがBestOpenableURL。

### 12.3 Catch-up suppression

1. source開始前にログfixtureを作る。
2. LogSnapshot capture後にsource開始。
3. catch-up Recordをcommit。

期待：

- DB Observationあり。
- Projector state更新。
- SSE broadcast 0。
- Notification 0。

その後新しい行をappend。

期待：

- Live Observation。
- SSE 1。
- 対象Changeならnotification 1。

### 12.4 Conflict

1. 同じObservation IDの既存rowを異なるpayloadで用意。
2. Recordをcommit。

期待：

- transaction rollback。
- cursor不変。
- Observation dropなし。
- Runner StateFailed。
- Runがerror。
- main supervisorがgraceful shutdown pathへ入る。

### 12.5 Media identity

- same URL 5m later。
- same target different URL 30s later。
- duplicate burst 1s。
- two players interleaved。

期待は§7.8どおり。

## 13. 推奨実装フェーズ

### Phase 1: 上流追従とschema v3

- dependency更新
- explicit Adapter composition
- schema version 3
- compile/test修正

### Phase 2: conflict integrity

- failing test
- drop logic削除
- StateFailed
- fatal error propagation
- main controlled shutdown
- OnInsert error化

### Phase 3: catch-up/live

- SourceRecord/DeliveryPhase
- LogSnapshot integration
- side-effect suppression
- E2E

### Phase 4: Media correlation

- bounded exact match
- source duplicate window
- Input/Output両方保持
- correlation test suite

### Phase 5: immutable Projector

- clone helper
- Change value化
- mutation/race test

### Phase 6: API/runtime hardening

- ready false
- route write deadline
- backoff reset
- health status

### Phase 7: CI/release/cleanup

- race
- release verify
- `go mod tidy`
- README/SPEC更新

## 14. 禁止事項

- conflict Observationをdropしてcursorを進める
- semantic conflictをDiagnosticへ降格する
- exact URL/targetの無期限merge
- timestampだけによるcatch-up判定
- catch-up SSE/Discord通知
- Projector内部pointerの外部公開
- `adapters.All()`相当の再実装
- old DB automatic migration
- media URL metadata fetch
- automatic browser open
- signed URLをsourceより優先
- communityログprefixをCompanionで直接parse
- `vrclog-go`型のコピー

## 15. 受け入れコマンド

Web buildがGo embedに必要な場合、先に実行する。

```bash
cd web
npm ci
npm run lint
npm run build
cd ..

rm -rf webembed/dist
mkdir -p webembed/dist
cp -R web/dist/* webembed/dist/

gofmt -w .
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go test -tags=integration -count=1 ./test/integration/...
go test -tags=e2e -count=1 ./test/e2e/...
go test -run 'Test.*Conflict|Test.*CatchUp|Test.*Media|Test.*Snapshot|Test.*Backoff' -count=20 ./...
go mod tidy
git diff --exit-code -- go.mod go.sum
```

必要に応じてWindowsでも全unit/integration/E2Eを実行する。

## 16. 完了条件

- Observation conflictで1 byteもcursorが進まず、processがfatal終了する。
- source progress後にretry backoffがinitialへ戻る。
- catch-upはDB/Projectorへ入るがSSE/Discordへ出ない。
- live recordだけがside effectを発生させる。
- 同じURLやtargetを時間無制限にmergeしない。
- `ResourceResolved.Input`を失わない。
- Change/Snapshotがimmutable copyである。
- server既定not-ready。
- SSE以外のwriteが有限deadline。
- schema v2を明確に拒否し、v3 fresh DBで動く。
- community Adapter組み込みが明示的。
- CI raceとrelease verifyが存在する。
- URL recovery E2Eが成功する。

## 17. 完了報告

以下を報告する。

```text
- 使用したvrclog-go / vrclog-adapters commit/version
- DB schema version
- Observation conflictのfatal path
- DeliveryPhase contract
- Media correlation rule
- ResourceResolved Input/Outputの扱い
- public API/JSON変更
- 実行したunit/race/integration/E2E結果
- DBをrename/deleteする必要があること
```

明示的な指示なしにcommit、push、tag、releaseは行わない。
