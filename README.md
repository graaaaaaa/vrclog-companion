# VRClog Companion

A local resident application that monitors VRChat logs and persists Join/Leave/World events to SQLite.

[日本語版はこちら](./README.ja.md)

## Features

- VRChat log monitoring (tail)
- Event persistence (SQLite with WAL mode)
- HTTP API + Web UI
- Discord notifications (Webhook with batching)
- Real-time updates via SSE

See [SPEC.md](./SPEC.md) for detailed specifications.

## Requirements

- Go 1.25+
- Node.js 20+ (for Web UI build)
- Windows 11 (target OS)

## Directory Structure

```
vrclog-companion/
├── cmd/
│   └── vrclog/          # Main entry point
├── internal/
│   ├── api/             # HTTP API server
│   ├── app/             # Use case layer
│   ├── config/          # Configuration management
│   ├── derive/          # Derived state (in-memory tracking)
│   ├── event/           # Event model
│   ├── ingest/          # Log monitoring and ingestion
│   ├── notify/          # Discord notifications
│   └── store/           # SQLite persistence
├── web/                 # Web UI (React + Vite)
├── webembed/            # Embedded Web UI
├── .github/
│   └── workflows/
│       ├── ci.yml       # GitHub Actions CI
│       └── release.yml  # Release workflow
├── go.mod
├── SPEC.md              # Specification
├── LICENSE              # MIT License
└── README.md
```

## Build & Run

### Build

```bash
# Build Web UI
cd web && npm install && npm run build && cd ..
cp -r web/dist/* webembed/dist/

# Cross-compile for Windows (from macOS/Linux)
GOOS=windows GOARCH=amd64 go build -o vrclog.exe ./cmd/vrclog

# Build for local environment
go build -o vrclog ./cmd/vrclog
```

### Run

```bash
# Default (port 8080)
./vrclog

# Specify port
./vrclog -port 9000
```

### Verify

```bash
# After server startup
curl http://127.0.0.1:8080/api/v1/health
# {"status":"ok","version":"0.1.0"}

# Web UI
# Open http://127.0.0.1:8080 in your browser
```

## API Endpoints

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

## Testing

```bash
go test ./...
```

## CI

GitHub Actions runs tests automatically on Windows runners.

- Triggered on `push` / `pull_request`
- Tests on Windows + Linux
- Build verification on Windows

## Security

### LAN Mode

Setting `lan_enabled=true` in `config.json` allows access from other devices on your local network.

- **Basic Auth is required**: Basic Auth is automatically enabled when LAN mode is on
- **Auto-generated password on first run**: If credentials are not configured, a strong random password is generated and saved to `generated_password.txt` in the data directory

### Important Notes

> **Warning**: Basic Auth provides no protection against eavesdropping without TLS.
> Only use on trusted local networks.

- Port forwarding is **not recommended**
- Internet exposure is not supported
- Credentials are stored in `secrets.json`
- Browser's `EventSource` API cannot send Basic Auth headers, so SSE stream (`/api/v1/stream`) access from browsers uses token authentication

## License

MIT License - See [LICENSE](./LICENSE) for details.
