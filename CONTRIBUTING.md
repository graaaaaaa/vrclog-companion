# Contributing to VRClog Companion

Thank you for your interest in contributing to VRClog Companion!

## Prerequisites

- Go 1.25 or later
- Node.js 20 or later (for Web UI development)
- Git

## Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/graaaaa/vrclog-companion.git
   cd vrclog-companion
   ```

2. Install Go dependencies:
   ```bash
   go mod download
   ```

3. Install Web UI dependencies:
   ```bash
   cd web
   npm install
   cd ..
   ```

4. Build the Web UI:
   ```bash
   cd web && npm run build && cd ..
   cp -r web/dist/* webembed/dist/
   ```

5. Run tests:
   ```bash
   go test ./...
   ```

## Development Workflow

### Running in Development Mode

For backend development:
```bash
go run ./cmd/vrclog
```

For frontend development (with hot reload):
```bash
cd web
npm run dev
```
The Vite dev server proxies API requests to `http://127.0.0.1:8080`.

### Branch Strategy

- `main` - stable branch, always deployable
- Feature branches - create from `main`, name with descriptive prefix (e.g., `feat/add-stats-page`, `fix/auth-bug`)

### Commit Messages

Use clear, descriptive commit messages. Examples:
- `feat: add stats page to web UI`
- `fix: handle empty player list in SSE`
- `refactor: extract auth middleware`
- `docs: update API documentation`
- `test: add integration tests for events API`

## Code Style

### Go

- Follow standard Go conventions
- Run `go fmt` before committing
- Keep functions small and focused
- Use meaningful variable names
- Add comments for exported functions

### TypeScript/React

- Use functional components with hooks
- Keep components focused on single responsibility
- Use TypeScript types/interfaces
- Follow existing patterns in the codebase

## Testing

### Running Tests

```bash
# All tests
go test ./...

# Specific package
go test ./internal/store

# With verbose output
go test -v ./...

# Integration tests (requires integration build tag)
go test -tags=integration ./test/integration/...
```

### Writing Tests

- Write tests for new functionality
- Tests should be deterministic (use dependency injection for time, randomness)
- Use table-driven tests where appropriate
- Mock external dependencies (use interfaces)

## Pull Request Process

1. Create a feature branch from `main`
2. Make your changes
3. Ensure all tests pass:
   ```bash
   go test ./...
   ```
4. Ensure Windows build works:
   ```bash
   GOOS=windows GOARCH=amd64 go build ./cmd/vrclog
   ```
5. If you modified the Web UI, rebuild it:
   ```bash
   cd web && npm run build && cd ..
   cp -r web/dist/* webembed/dist/
   ```
6. Push your branch and create a pull request
7. Wait for CI to pass
8. Address any review feedback

### PR Guidelines

- Keep PRs focused and small
- Include a clear description of what changed and why
- Link related issues if applicable
- Never commit secrets or credentials

## Security

- Never log secrets (use `Secret` type which masks values)
- Validate all user input
- Use constant-time comparison for sensitive values
- Follow OWASP guidelines

## Questions?

If you have questions or need help, please open an issue.
