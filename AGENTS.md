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

- `coderlog/coderlog.go` — `Client` struct: register log source, batch & flush log entries via Coder Agent API.
- `coderlog/tail.go` — `TailFile()`: watches a file for new lines and streams them.
- `cmd/coder-logger/main.go` — CLI: parses flags, reads stdin or tails a file.

### Architecture

The `coderlog` package is the importable core. It exposes:

- `Client` — configured with `AgentURL` and `AgentToken`, handles HTTP calls to:
  - `POST /api/v2/workspaceagents/me/log-source` (register a source)
  - `PATCH /api/v2/workspaceagents/me/logs` (send log entries)
- `TailFile()` — file-following helper that feeds lines into a `Client`.
- The CLI (`cmd/coder-logger`) is a thin wrapper that wires flags/env vars to the package.

### Environment Variables

| Variable | Required | Description |
|---|---|---|
| `CODER_AGENT_URL` | Yes | Base URL of the Coder deployment |
| `CODER_AGENT_TOKEN` | Yes | Workspace agent session token |

### CLI Flags

| Flag | Default | Description |
|---|---|---|
| `--log-file` | _(stdin)_ | Path to a log file to tail |
| `--source-name` | `cloud_init` | Display name for the log source |
| `--source-icon` | cloud-init SVG | Icon URL for the log source |
| `--level` | `info` | Log level (trace/debug/info/warn/error/fatal) |

# Essential Commands

- **Build:** `go build ./...`
- **Build binary:** `go build -o coder-logger ./cmd/coder-logger`
- **Format:** `gofmt -w .`
- **Lint:** `go vet ./...`
- **Test:** `go test ./...`
- **Clean:** `rm -f coder-logger`

# Commit and Pull Request Guidelines

- Validate all changes before committing: run `go build ./...`, `go vet ./...`, and `go test ./...`.
- Commit messages use the format: `type: message` (e.g., `feat: add logging endpoint`, `fix: handle null config`).
- PR descriptions must explain **what** changed and **why**.
- Keep PRs focused on a single concern.
