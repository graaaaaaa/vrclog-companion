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
| `internal/event` | Shared Event model (`*string` fields, JSON-ready) |
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

### Store Package Structure

The `internal/store` package separates concerns:
- `store.go` - Open/Close, TimeFormat constant
- `events.go` - InsertEvent, QueryEvents, GetLastEventTime, CountEvents
- `cursor.go` - URL-safe base64 cursor encoding/decoding
- `row.go` - DB row ↔ `event.Event` mapping, validation
- `errors.go` - Typed errors (`ErrInvalidCursor`, `ErrInvalidEvent`)
- `migrate.go` - Schema creation (events, ingest_cursor tables)

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

## PR Rules

1. Keep PRs small
2. `go test ./...` must pass
3. `GOOS=windows go build ./...` must pass
4. Never log secrets (mask them)

## Reference Documents

- `SPEC.md` - Full specification
- `docs/IMPLEMENTATION_PLAN.md` - Milestone breakdown
