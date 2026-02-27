# coder-logger

A CLI tool and Go package for streaming log lines to [Coder](https://coder.com) workspace startup logs via the Agent API.

Designed to run **inside a Coder workspace** where `CODER_AGENT_TOKEN` and `CODER_AGENT_URL` are available. Replaces the [curl-based shell script approach](https://gist.github.com/bpmct/39838c2dbf6a205d86cf8bbc60a7c1c9) with a first-class CLI and an importable Go package.

## Quick Start

### Install

Download a binary from the [releases page](https://github.com/gerbidigm/coder-logger/releases), or build from source:

```bash
go install github.com/gerbidigm/coder-logger/cmd/coder-logger@latest
```

### Send a single message

```bash
coder-logger send --source cloud-init "Installing dependencies..."
```

### Pipe logs from a command or file

```bash
tail -f /var/log/cloud-init.log | coder-logger send --source cloud-init
```

### Pre-register a source with a custom icon

```bash
coder-logger register --name cloud-init --icon https://example.com/icon.svg
```

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `CODER_AGENT_URL` | Yes | Base URL of the Coder deployment |
| `CODER_AGENT_TOKEN` | Yes | Workspace agent session token |

Both are automatically available inside Coder workspaces.

## CLI Reference

### `coder-logger register`

Pre-register a named log source. Optional — `send` auto-registers if needed.

| Flag | Required | Default | Description |
|---|---|---|---|
| `--name` | Yes | — | Log source name |
| `--icon` | No | `""` | Icon URL |

### `coder-logger send`

Send log lines to an agent log source.

| Flag | Required | Default | Description |
|---|---|---|---|
| `--source` | Yes | — | Log source name |
| `--icon` | No | `""` | Icon URL |
| `--level` | No | `info` | Log level: `trace`, `debug`, `info`, `warn`, `error` |

**Args mode:** Trailing arguments are joined and sent as a single log line.

**Stdin mode:** If no arguments are given, reads from stdin with batched streaming (50 lines / 250ms flush).

## Using as a Go Package

The `coderlog` package is designed for direct import — e.g., into the [Coder CLI](https://github.com/coder/coder) as a subcommand.

```go
import "github.com/gerbidigm/coder-logger/coderlog"

client := &coderlog.Client{
    AgentURL:   os.Getenv("CODER_AGENT_URL"),
    AgentToken: os.Getenv("CODER_AGENT_TOKEN"),
    CacheDir:   "/tmp/coder-logger",
}

// Register (idempotent — uses deterministic UUID v5 from name).
sourceID, err := client.EnsureSource(ctx, "cloud-init", "https://example.com/icon.svg")

// Send lines directly.
err = client.SendLines(ctx, sourceID, coderlog.LogLevelInfo, []string{"hello world"})

// Or stream from any io.Reader with batching.
err = client.StreamReader(ctx, os.Stdin, sourceID, coderlog.LogLevelInfo)
```

### Key Design Decisions

- **Deterministic source IDs** — UUID v5 derived from source name. Same name always produces the same ID.
- **Token-scoped cache** — Registered sources are cached under `$CONFIG_DIR/log-sources/<sha256(token)[:16]>/` to avoid redundant API calls. New workspace build → new token → fresh cache.
- **Overflow detection** — Agent logs share a cumulative 1 MiB limit per workspace build. On HTTP 413, an `.overflow` sentinel is written and all future sends are blocked immediately (no wasted API calls).

## Terraform / Cloud-Init Example

Use `coder-logger` in a Coder template to stream cloud-init logs to the workspace UI:

```hcl
resource "coder_agent" "main" {
  # ...
  startup_script = <<-EOT
    # Stream cloud-init logs to the Coder UI
    coder-logger send --source cloud-init --icon "https://cloud-init.github.io/images/cloud-init-orange.svg" < /var/log/cloud-init-output.log &
  EOT
}
```

## Development

### Prerequisites

- Go 1.21+
- [Mage](https://magefile.org/) (optional, for cross-compilation)

### Build

```bash
# Build for all targets (linux/amd64, linux/arm64, darwin/arm64)
mage build

# Build for your current platform
mage buildLocal

# Or just use go directly
go build ./cmd/coder-logger
```

### Test

```bash
# Run all tests (parallel by default)
mage test
# or
go test ./...
```

Tests use a mock server that simulates the Coder Agent API, including idempotent source registration, overflow detection, and authentication.

### Lint

```bash
mage lint
# or
go vet ./...
```

### Format

```bash
gofmt -w .
```

### Project Structure

```
cmd/coder-logger/    CLI entrypoint (register + send subcommands)
coderlog/            Importable package
  coderlog.go          Client, EnsureSource, SendLines, cache, overflow
  stream.go            StreamReader (batched stdin/reader streaming)
  coderlog_test.go     Tests
  mock_test.go         Mock Coder Agent API server
magefile.go          Cross-compilation build targets
```

## License

[MIT](LICENSE)
