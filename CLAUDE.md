# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

VRClog Companion is a Windows local resident app that monitors VRChat logs, extracts events (Join/Leave/World), and persists them to SQLite. It provides HTTP API + Web UI for history viewing and Discord Webhook notifications.

**Key principle**: No central server. All data stays on the user's PC only.

## Commands

```bash
# Build
go build -o vrclog ./cmd/vrclog

# Build for Windows (cross-compile) with version
GOOS=windows GOARCH=amd64 go build -ldflags "-X github.com/graaaaa/vrclog-companion/internal/version.Version=0.1.0" -o vrclog.exe ./cmd/vrclog

# Run tests
go test ./...

# Run single test
go test -run TestHealthEndpoint ./internal/api

# Run server (default port 8080)
./vrclog

# Run server with custom port
./vrclog -port 9000

# Verify health endpoint
curl http://127.0.0.1:8080/api/v1/health
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
| `internal/version` | Build version info (ldflags injection) |
| `internal/store` | SQLite persistence (planned) |
| `internal/ingest` | Log monitoring via vrclog-go (planned) |
| `internal/derive` | Current state tracker (planned) |
| `internal/notify` | Discord Webhook (planned) |
| `internal/config` | Config/secrets management (planned) |

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

### Key Design Decisions

- **Deduplication**: `dedupe_key = SHA256(raw_line)` with UNIQUE constraint
- **Security defaults**: Binds to `127.0.0.1` by default. LAN mode requires Basic Auth
- **Config location**: `%LOCALAPPDATA%/vrclog/` (config.json, secrets.dat, vrclog.sqlite)
- **Atomic writes**: Config files use tmp→rename pattern
- **Version injection**: Use `-ldflags "-X .../internal/version.Version=X.Y.Z"` at build time

## PR Rules

1. Keep PRs small
2. `go test ./...` must pass
3. Windows CI must pass
4. Never log secrets (mask them)

## Reference Documents

- `SPEC.md` - Full specification
- `docs/IMPLEMENTATION_PLAN.md` - Milestone breakdown
