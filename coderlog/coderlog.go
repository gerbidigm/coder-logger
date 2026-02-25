// Package coderlog provides a client for streaming log lines to Coder workspace
// startup logs via the Agent API. It is designed to be imported as a library or
// used via the coder-logger CLI.
//
// Key design decisions:
//   - Deterministic UUID v5 IDs derived from source names (same name → same ID).
//   - Token-scoped file cache prevents redundant register calls and detects overflow.
//   - Auto-register on send — no mandatory register step.
//   - 1 MiB cumulative log limit per workspace build; overflow is cached to avoid
//     wasted API calls after the limit is hit.
package coderlog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Fixed namespace for deterministic UUID v5 generation from source names.
// Generated once; do not change.
var logSourceUUIDNamespace = uuid.MustParse("a0c8f2e4-0b3a-4d6e-9f1a-2b7c5d8e0f3a")

// LogSourceIDFromName derives a deterministic UUID v5 from a source name.
// The same name always produces the same ID, which the Coder API accepts.
func LogSourceIDFromName(name string) uuid.UUID {
	return uuid.NewSHA1(logSourceUUIDNamespace, []byte(name))
}

// LogLevel represents the severity of a log entry.
type LogLevel string

const (
	LogLevelTrace LogLevel = "trace"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogEntry is a single log line sent to the agent.
type LogEntry struct {
	CreatedAt time.Time `json:"created_at"`
	Level     LogLevel  `json:"level"`
	Output    string    `json:"output"`
}

// patchLogsRequest is the payload for the PATCH /logs endpoint.
type patchLogsRequest struct {
	LogSourceID uuid.UUID  `json:"log_source_id"`
	Logs        []LogEntry `json:"logs"`
}

// postLogSourceRequest is the payload for the POST /log-source endpoint.
type postLogSourceRequest struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Icon        string    `json:"icon"`
}

// ErrOverflow is returned when the 1 MiB agent log limit has been exceeded.
var ErrOverflow = fmt.Errorf("agent log limit (1 MiB) exceeded — no further logs can be sent until the next workspace build")

// Client sends logs to the Coder workspace agent API.
type Client struct {
	// AgentURL is the base URL for the Coder deployment (CODER_AGENT_URL).
	AgentURL string
	// AgentToken is the workspace agent session token (CODER_AGENT_TOKEN).
	AgentToken string
	// CacheDir is the root config directory for the token-scoped cache.
	// If empty, caching is disabled.
	CacheDir string
	// HTTPClient is optional; http.DefaultClient is used if nil.
	HTTPClient *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// scopeDir returns the token-scoped cache directory path.
func (c *Client) scopeDir() string {
	if c.CacheDir == "" {
		return ""
	}
	h := sha256.Sum256([]byte(c.AgentToken))
	scope := hex.EncodeToString(h[:8])
	return filepath.Join(c.CacheDir, "log-sources", scope)
}

// CheckOverflow returns ErrOverflow if the overflow sentinel exists.
func (c *Client) CheckOverflow() error {
	dir := c.scopeDir()
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, ".overflow")); err == nil {
		return ErrOverflow
	}
	return nil
}

func (c *Client) markOverflow() {
	dir := c.scopeDir()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, ".overflow"), nil, 0o600)
}

// EnsureSource registers a log source if it hasn't been cached yet.
// Uses deterministic UUID v5 from the source name. Idempotent — the Coder API
// accepts re-registration of the same source ID.
func (c *Client) EnsureSource(ctx context.Context, name, icon string) (uuid.UUID, error) {
	if err := c.CheckOverflow(); err != nil {
		return uuid.Nil, err
	}

	id := LogSourceIDFromName(name)

	// Check cache.
	dir := c.scopeDir()
	if dir != "" {
		marker := filepath.Join(dir, name)
		if _, err := os.Stat(marker); err == nil {
			return id, nil // already registered
		}
	}

	// Register via API.
	payload := postLogSourceRequest{ID: id, DisplayName: name, Icon: icon}
	body, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal source: %w", err)
	}

	url := fmt.Sprintf("%s/api/v2/workspaceagents/me/log-source", c.AgentURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return uuid.Nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Coder-Session-Token", c.AgentToken)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return uuid.Nil, fmt.Errorf("register log source: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return uuid.Nil, fmt.Errorf("register log source: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Write cache marker.
	if dir != "" {
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(filepath.Join(dir, name), nil, 0o600)
	}

	return id, nil
}

// SendLines sends one or more log lines immediately (no batching).
// Returns ErrOverflow if the 1 MiB limit is hit (HTTP 413).
func (c *Client) SendLines(ctx context.Context, sourceID uuid.UUID, level LogLevel, lines []string) error {
	if err := c.CheckOverflow(); err != nil {
		return err
	}

	entries := make([]LogEntry, len(lines))
	for i, line := range lines {
		entries[i] = LogEntry{
			CreatedAt: time.Now().UTC(),
			Level:     level,
			Output:    line,
		}
	}

	payload := patchLogsRequest{LogSourceID: sourceID, Logs: entries}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal logs: %w", err)
	}

	url := fmt.Sprintf("%s/api/v2/workspaceagents/me/logs", c.AgentURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Coder-Session-Token", c.AgentToken)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("send logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		c.markOverflow()
		return ErrOverflow
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send logs: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
