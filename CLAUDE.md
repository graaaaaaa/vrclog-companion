# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

VRClog Companion is a Windows local resident app that monitors VRChat logs, extracts events (Join/Leave/World), and persists them to SQLite. It provides HTTP API + Web UI for history viewing and Discord Webhook notifications.

**Key principle**: No central server. All data stays on the user's PC only.

## Commands

```bash
# Build (local)
go build -o vrclog ./cmd/vrclog

# Build for Windows (cross-compile) with version
GOOS=windows GOARCH=amd64 go build -ldflags "-X github.com/graaaaa/vrclog-companion/internal/version.Version=0.1.0" -o vrclog.exe ./cmd/vrclog

# Run tests
go test ./...

# Run single test
go test -run TestInsertEvent_Dedupe ./internal/store

# Run server (default port 8080)
./vrclog

# Run server with custom port
./vrclog -port 9000
```

## Architecture

### Data Flow
```
VRChat Log → vrclog-go (parser) → Event → Dedupe Check → SQLite
                                              ↓
                              (on new event only)
                              ├── Derive update (in-memory state)
                              ├── Discord notification
                              └── SSE broadcast
```

### Internal Packages

| Package | Purpose |
|---------|---------|
| `internal/api` | HTTP API server (JSON + SSE + Auth) |
| `internal/app` | Use case layer (business logic interfaces) |
| `internal/appinfo` | App identity constants (name, dir, mutex, filenames) |
| `internal/config` | Config/secrets management with atomic writes |
| `internal/derive` | In-memory state tracking (current world, online players) |
| `internal/event` | Shared Event model (`*string` fields, JSON-ready) |
| `internal/ingest` | Log monitoring via vrclog-go, event ingestion to SQLite |
| `internal/notify` | Discord Webhook notifications with batching and backoff |
| `internal/singleinstance` | Single instance control (Windows mutex, macOS no-op) |
| `internal/store` | SQLite persistence (WAL mode, deduplication, cursor pagination) |
| `internal/version` | Build version info (ldflags injection) |

### Dependency Injection Pattern

```
cmd/vrclog/main.go
    └── builds dependencies (app.HealthService, etc.)
    └── passes to api.NewServer(addr, health)
            └── server calls health.Handle(ctx) via interface
```

- `internal/app` defines use case interfaces (e.g., `HealthUsecase`)
- `internal/api` depends only on interfaces, not implementations
- `cmd/vrclog/main.go` wires concrete implementations

### API Package Structure

The `internal/api` package provides HTTP server with SSE support:
- `server.go` - Server with ServerOption pattern, route registration
- `hub.go` - SSE Hub (1 goroutine + channel management, no mutex)
- `events.go` - GET /api/v1/events handler with cursor pagination
- `stream.go` - GET /api/v1/stream SSE handler with Last-Event-ID replay
- `middleware.go` - Basic Auth middleware with SHA-256 constant-time comparison
- `json.go` - JSON response helper

Key features:
- WriteTimeout=0 for SSE long-lived connections
- Hub uses channel pattern (register/unregister/broadcast) for thread safety
- SSE `id` field uses cursor format (`base64(ts|id)`) for direct QueryEvents compatibility
- Last-Event-ID replay limited to 5 pages (500 events) best-effort
- Heartbeat every 20 seconds (`":\n\n"`) to prevent proxy timeouts
- Hub.Stop is idempotent (uses sync.Once)

### Store Package Structure

The `internal/store` package separates concerns:
- `store.go` - Open/Close, TimeFormat constant
- `events.go` - InsertEvent, QueryEvents, GetLastEventTime, CountEvents
- `parse_failures.go` - InsertParseFailure for logging parse errors
- `cursor.go` - URL-safe base64 cursor encoding/decoding
- `row.go` - DB row ↔ `event.Event` mapping, validation
- `errors.go` - Typed errors (`ErrInvalidCursor`, `ErrInvalidEvent`)
- `migrate.go` - Schema creation (events, ingest_cursor, parse_failures tables)

### Ingest Package Structure

The `internal/ingest` package handles log monitoring:
- `source.go` - EventSource interface for testing abstraction
- `vrclog_source.go` - vrclog-go watcher implementation with configurable buffer sizes
- `convert.go` - Event conversion + SHA256 dedupe key + Clock interface
- `ingest.go` - Ingester loop coordinating source → store
- `replay.go` - Replay time calculation helper

Key features:
- On startup, replays events from (last_event_time - 5 min) via WithReplaySinceTime
- Requires WithIncludeRawLine(true) for SHA256 dedupe key
- Handles ParseError by saving to parse_failures table
- Context cancellation stops watcher cleanly (no goroutine leaks)
- Interface abstraction allows unit testing without vrclog-go dependency
- Clock injection for deterministic testing (avoids time.Now() in tests)
- Nil-channel pattern for independent channel closure handling
- Configurable buffer sizes (default: event=64, error=16) to reduce DB backpressure
- `OnInsertFunc` callback for triggering side effects (derive/notify) on new events

### Derive Package Structure

The `internal/derive` package manages ephemeral in-memory state:
- `state.go` - State struct tracking current world and online players

Key features:
- Thread-safe via sync.RWMutex (API server and ingester may access concurrently)
- Returns `DerivedEvent` indicating what changed (WorldChanged, PlayerJoined, PlayerLeft)
- Clears player list on world change
- Deduplicates player join events (same player joining twice returns nil)
- Pure in-memory (no persistence, rebuilt on restart if needed)
- PlayerID-first keying (falls back to PlayerName if ID empty) for rename tolerance

### Notify Package Structure

The `internal/notify` package handles Discord Webhook notifications:
- `timer.go` - AfterFunc injection for testable batch timers
- `backoff.go` - Exponential backoff calculation with jitter
- `payload.go` - Discord embed payload builder
- `sender.go` - Sender interface and DiscordSender implementation
- `notifier.go` - Notifier with batching, filtering, and Run() loop

Key features:
- Configurable batch interval (`DiscordBatchSec`, default 3 seconds)
- Notification filtering via `NotifyOnJoin/Leave/WorldJoin` config flags
- Multiple events batched into single Discord request (up to 10 embeds)
- Exponential backoff on 429/transient errors (1s initial, 5min max, 0.2 jitter)
- Fatal stop on 401/403 (invalid webhook, stored in Status)
- Best-effort flush on shutdown
- AfterFunc injection for deterministic batch tests

### Key Design Decisions

- **Deduplication**: `dedupe_key = SHA256(raw_line)` with UNIQUE constraint, ON CONFLICT DO NOTHING
- **SQLite settings**: WAL mode, busy_timeout=5000ms, modernc.org/sqlite (pure Go, no CGO)
- **Timestamps**: Fixed-width RFC3339 format (`2006-01-02T15:04:05.000000000Z`) for correct lexicographic ordering
- **Cursor pagination**: URL-safe base64 (RawURLEncoding), backward compatible with StdEncoding
- **Security defaults**: Binds to `127.0.0.1` by default. LAN mode requires Basic Auth
- **Config location**: `%LOCALAPPDATA%/vrclog/` on Windows, `~/.config/vrclog/` on other platforms
- **Atomic writes**: Config files use tmp→rename (POSIX) or MoveFileEx (Windows)
- **Single instance**: Windows uses CreateMutex (session-scoped), macOS is no-op for development
- **Secrets masking**: `Secret` type with `String()` returning `[REDACTED]` for log safety
- **Config resilience**: Corrupt/missing config falls back to defaults with warning (non-fatal)
- **Clock injection**: `Clock` interface in `ingest/convert.go` allows deterministic time testing
- **Nil-channel pattern**: Both `vrclog_source.go` and `ingest.go` use nil-channel pattern to handle independent channel closures without losing events

## Testing Patterns

- **Interface abstraction**: `EventSource` interface allows mocking vrclog-go for unit tests
- **Clock injection**: Use `WithClock(clock)` option to inject test clocks for deterministic timestamps
- **Buffer configuration**: Use `WithEventBufferSize`/`WithErrorBufferSize` options to control channel throughput in tests
- **Replay calculation**: Use `CalculateReplaySinceWithClock` for deterministic replay time tests
- **Timer injection**: Use `WithAfterFunc(fakeAfterFunc)` in notify package for deterministic batch tests
- **Mock sender**: `MockSender` in tests allows verifying Discord payloads without HTTP calls
- **Fake timer handle**: `FakeTimerHandle` with `Fire()` method triggers batch flush synchronously

## API Routes

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | /api/v1/health | No | Health check |
| GET | /api/v1/events | If LAN | Query events with cursor pagination |
| GET | /api/v1/stream | If LAN | SSE stream with Last-Event-ID replay |

Query parameters for `/api/v1/events`:
- `since`, `until` - RFC3339 timestamps
- `type` - `player_join`, `player_left`, `world_join`
- `limit` - Max items per page
- `cursor` - Pagination cursor from previous response

## PR Rules

1. Keep PRs small
2. `go test ./...` must pass
3. `GOOS=windows go build ./...` must pass
4. Never log secrets (mask them)

## Reference Documents

- `SPEC.md` - Full specification
- `docs/IMPLEMENTATION_PLAN.md` - Milestone breakdown
