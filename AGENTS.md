You are an experienced, pragmatic software engineering AI agent. Do not over-engineer a solution when a simple one is possible. Keep edits minimal. If you want an exception to ANY rule, you MUST stop and get permission first.

# Project Overview

**coder-logger** — A Go CLI tool and importable package for streaming log lines to [Coder](https://coder.com) workspace startup logs via the Agent API. Designed to run inside a Coder workspace (where `CODER_AGENT_TOKEN` and `CODER_AGENT_URL` are available) and eventually be imported into the `github.com/coder/coder` CLI as a subcommand.

### Technology

- **Language:** Go (1.21+)
- **Dependencies:** `github.com/google/uuid`
- **Build:** `go build ./...`
- **No framework** — standard library HTTP client, `flag` for CLI args.

# Reference

### Directory Structure

```
cmd/coder-logger/    CLI entrypoint (main.go)
coderlog/            Importable package — API client, log streaming, file tailing
go.mod, go.sum       Module definition
```

### Important Files

- `coderlog/coderlog.go` — `Client` struct: EnsureSource (register + cache), SendLines, overflow detection via Coder Agent API.
- `coderlog/stream.go` — `StreamReader()`: batched stdin/reader streaming (50 lines / 250ms flush).
- `cmd/coder-logger/main.go` — CLI with `register` and `send` subcommands.

### Architecture

The `coderlog` package is the importable core. It exposes:

- `Client` — configured with `AgentURL`, `AgentToken`, and `CacheDir`, handles HTTP calls to:
  - `POST /api/v2/workspaceagents/me/log-source` (register a source)
  - `PATCH /api/v2/workspaceagents/me/logs` (send log entries)
- `LogSourceIDFromName()` — deterministic UUID v5 from source name (same name → same ID).
- `StreamReader()` — batched reader streaming helper.
- **Token-scoped cache** under `$CONFIG_DIR/log-sources/<sha256(token)[:16]>/` prevents redundant API calls.
- **Overflow detection** — HTTP 413 → `.overflow` sentinel → blocks future sends until next build.
- The CLI (`cmd/coder-logger`) is a thin wrapper with `register` and `send` subcommands.

### Environment Variables

| Variable | Required | Description |
|---|---|---|
| `CODER_AGENT_URL` | Yes | Base URL of the Coder deployment |
| `CODER_AGENT_TOKEN` | Yes | Workspace agent session token |

### CLI Commands

**`coder-logger register`** — Pre-register a log source (optional).

| Flag | Required | Default | Description |
|---|---|---|---|
| `--name` | Yes | — | Log source name |
| `--icon` | No | `""` | Icon URL |

**`coder-logger send`** — Send log lines (auto-registers the source).

| Flag | Required | Default | Description |
|---|---|---|---|
| `--source` | Yes | — | Log source name |
| `--icon` | No | `""` | Icon URL |
| `--level` | No | `info` | Log level (trace/debug/info/warn/error) |

Trailing args are sent as a single message; if no args, reads stdin with batching.

# Essential Commands

- **Build (all targets):** `mage build` — cross-compiles to `dist/` for linux/amd64, linux/arm64, darwin/arm64.
- **Build (local only):** `mage buildLocal`
- **Build (go only):** `go build ./...`
- **Format:** `gofmt -w .`
- **Lint:** `mage lint` or `go vet ./...`
- **Test:** `mage test` or `go test ./...`
- **Clean:** `mage clean` — removes `dist/`

# Commit and Pull Request Guidelines

- Validate all changes before committing: run `go build ./...`, `go vet ./...`, and `go test ./...`.
- Commit messages use the format: `type: message` (e.g., `feat: add logging endpoint`, `fix: handle null config`).
- PR descriptions must explain **what** changed and **why**.
- Keep PRs focused on a single concern.
