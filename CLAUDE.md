# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

VRClog Companion is a Windows local resident app that monitors VRChat logs, extracts events (Join/Leave/World), and persists them to SQLite. It provides HTTP API + Web UI for history viewing and Discord Webhook notifications.

**Key principle**: No central server. All data stays on the user's PC only.

## Tech Stack

- Go 1.22+, SQLite (WAL mode), React + Vite (embedded via go:embed)
- Target: Windows 11 (macOS supported for development)
- Pure Go SQLite (modernc.org/sqlite, no CGO)

## Commands

```bash
go test ./...                                    # Run all tests
go test -run TestName ./internal/store           # Run single test
go test -tags=integration ./test/integration/... # Integration tests
GOOS=windows GOARCH=amd64 go build -o vrclog.exe ./cmd/vrclog  # Windows build
cd web && npm run build && cd .. && cp -r web/dist/* webembed/  # Build Web UI
cd web && npm run dev                            # Frontend dev server (proxy to :8080)
```

## Architecture

### Data Flow
```
VRChat Log → vrclog-go (parser) → Event → Dedupe Check → SQLite
                                              ↓
                              (on new event only)
                              ├── Derive update (in-memory state)
                              ├── Discord notification
                              └── SSE broadcast → Web UI
```

### Internal Packages

| Package | Purpose |
|---------|---------|
| `internal/api` | HTTP API server (JSON + SSE + Auth + Rate Limiting) |
| `internal/app` | Use case layer (business logic interfaces) |
| `internal/config` | Config/secrets management with atomic writes |
| `internal/derive` | In-memory state tracking (current world, online players) |
| `internal/event` | Shared Event model (`*string` fields, JSON-ready) |
| `internal/ingest` | Log monitoring via vrclog-go, event ingestion |
| `internal/notify` | Discord Webhook notifications with batching |
| `internal/store` | SQLite persistence (WAL, deduplication, cursor pagination) |
| `webembed` | Embedded web UI filesystem (go:embed) |

### Dependency Injection

- `internal/app` defines use case interfaces (e.g., `HealthUsecase`)
- `internal/api` depends only on interfaces, not implementations
- `cmd/vrclog/main.go` wires concrete implementations

## Claude Directives

- Clarify unknowns before starting implementation
- Use `plan mode` for changes spanning 2+ files or design decisions
- Always run `go test ./...` and `GOOS=windows go build ./...` before completing
- Never log secrets - use `Secret` type with `[REDACTED]` output
- Respect existing design patterns; minimize change scope

## Workflow

- **Feature**: Explore → Plan → Code → Test → Commit
- **Bug fix**: Reproduce → Diagnose → Fix → Test → Commit

## Key Design Decisions

- **Atomic writes**: Config files use tmp→rename (POSIX) or MoveFileEx (Windows)
- **Single instance**: Windows uses CreateMutex (session-scoped)
- **Security defaults**: Binds to `127.0.0.1` by default; LAN mode requires Basic Auth
- **Secrets safety**: `SecretsLoadStatus` prevents overwriting corrupted secrets files
- **Config resilience**: Corrupt/missing config falls back to defaults (non-fatal)
- **Cursor pagination**: URL-safe base64 with backward compatibility
- **Timestamps**: Fixed-width RFC3339 (`2006-01-02T15:04:05.000000000Z`) for lexicographic ordering

## Testing Patterns

- **Interface abstraction**: `EventSource` interface allows mocking vrclog-go
- **Clock injection**: `WithClock()` option for deterministic timestamps
- **Timer injection**: `WithAfterFunc()` for deterministic batch tests
- **Integration tests**: `test/integration/` with `//go:build integration` tag

## API Routes

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | /api/v1/health | No | Health check |
| GET | /api/v1/events | If LAN | Query events with cursor pagination |
| GET | /api/v1/stream | If LAN | SSE stream (accepts Basic Auth or token) |
| GET | /api/v1/now | If LAN | Current world and players |
| GET | /api/v1/stats/basic | If LAN | Today's statistics |
| POST | /api/v1/auth/token | If LAN | Issue SSE token (5min TTL) |
| GET | /api/v1/config | If LAN | Get config (secrets excluded) |
| PUT | /api/v1/config | If LAN | Update config |

## PR Rules

1. Keep PRs small
2. `go test ./...` must pass
3. `GOOS=windows go build ./...` must pass
4. Never log secrets (mask them)

## References

- `SPEC.md` - Full specification (Japanese)
