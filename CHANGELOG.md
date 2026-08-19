# Changelog

## Unreleased — Full architectural renewal (breaking)

This release replaces the entire event/storage/API architecture. **There is no compatibility with any prior version**: the Go module path changed, the SQLite schema changed, and every API/SSE contract changed. Old databases are not auto-migrated — stop the app and rename or delete the database file to start fresh.

### Breaking changes

- **Module path**: `github.com/graaaaa/vrclog-companion` → `github.com/vrclog/vrclog-companion`. Entry point moved from `cmd/vrclog/` to `cmd/vrclog-companion/`.
- **Dependency contracts**: now built on the rewritten `vrclog-go` (sealed `Event` interface with 7 kinds, `Record`/`Cursor`/`Observation`, `Engine`, iterator-based `Follow`/`ReadFile`) and the new `vrclog-adapters` module (community `Adapter`s: YamaPlayer, iwaSync3).
- **Storage model**: the old flat `Event` model (`player_join`/`player_left`/`world_join` only, raw-line SHA-256 dedupe) is gone. Persistence is now a canonical `Observation` stream, deduplicated solely by `vrclog.ObservationID`.
- **SQLite schema**: new schema version 2 (`observations`, `ingest_cursors`, `diagnostics`), versioned via `PRAGMA user_version`. The old `events`, `ingest_cursor`, `parse_failures`, `meta_json` tables are gone with **no automatic migration**.
- **Ingest**: the channel-based `EventSource`/`Ingester` pipeline and time-based replay (`CalculateReplaySince`) are replaced by an iterator-based `RecordSource` + `Runner` with per-Record atomic transactions and cursor-only resume.
- **Derived state**: `internal/derive.State` (single switch) is replaced by `internal/projector` — a `Manager` composing `WorldProjector`, `PresenceProjector`, and the new `MediaProjector`, all rebuildable from the database at startup.
- **API**: `/api/v1/events` and `/api/v1/now` are removed. New endpoints: `GET /api/v1/observations`, `GET /api/v1/state`, `GET /api/v1/media/recent`, `GET /api/v1/adapters`. `/api/v1/stats/basic` and `/api/v1/config` keep their paths with updated response shapes.
- **SSE**: per-EventKind event names are gone; `/api/v1/stream` now emits a single generic `event: observation`. Last-Event-ID recovery is race-free via a Broadcaster high-water sequence, with an explicit `event: reset` for unresolvable cursors.
- **Web UI**: rewritten for the new API — `History` shows generic Observations, a new `Media` page and a "Latest Media" card on `Now` support the URL-recovery workflow, `Stats` reflects the new aggregate shape.

### Added

- **Media URL recovery**: `MediaProjector` correlates resource/error Observations across adapters (e.g. YamaPlayer source URL → vrchat.core resolver relay → AVPro error) into a single `MediaAttempt`, exposing the original playable URL even when in-world playback silently failed. Never surfaces signed/resolved URLs as the recommended link, never auto-opens, never fetches external metadata.
- Compile-time Adapter composition (`internal/adapter.BuildEngine`) with a reported `loaded_adapters` count on `/api/v1/health` and full listing on `/api/v1/adapters`.
- Startup readiness gate: every route except `/api/v1/health` returns 503 while the Projector rebuild is in progress.
- Discord notification sanitization: `allowed_mentions: {"parse": []}`, Markdown escaping, and mention/URL auto-link neutralization for untrusted player/world names.
- Diagnostics persistence with deterministic IDs and message redaction (URL stripping, length cap), applied both at write time and at any future API read path.
- `test/e2e`: real `vrclog-adapters` log fixtures exercised through the full pipeline to `GET /api/v1/media/recent`, verifying the recovered URL matches the original exactly.

### Removed

- `internal/event`, `internal/derive` packages
- Old channel-based `ingest.EventSource`/`Ingester`, raw-line dedupe, time-based replay
- Old SQLite tables: `events`, `ingest_cursor`, `parse_failures`, `meta_json`
- `/api/v1/events`, `/api/v1/now`
- `internal/api.Hub` (replaced by `internal/sse.Broadcaster`)
