// Package coderlog provides a client for streaming logs to the Coder Agent
// startup log API. It can be used as a library or via the coder-logger CLI.
//
// The package registers a log source with the Coder workspace agent, then
// streams log lines (from a file or programmatically) to appear in the
// workspace's startup logs in the Coder UI.
package coderlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LogLevel represents the severity of a log entry.
type LogLevel string

const (
	LogLevelTrace   LogLevel = "trace"
	LogLevelDebug   LogLevel = "debug"
	LogLevelInfo    LogLevel = "info"
	LogLevelWarn    LogLevel = "warn"
	LogLevelError   LogLevel = "error"
	LogLevelFatal   LogLevel = "fatal"
)

// Source identifies a log source registered with the Coder agent.
type Source struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Icon        string    `json:"icon"`
}

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

// Client sends logs to the Coder workspace agent API.
type Client struct {
	// AgentURL is the base URL for the Coder deployment (CODER_AGENT_URL).
	AgentURL string
	// AgentToken is the workspace agent session token (CODER_AGENT_TOKEN).
	AgentToken string
	// HTTPClient is optional; http.DefaultClient is used if nil.
	HTTPClient *http.Client

	// FlushInterval controls how often buffered logs are sent.
	// Defaults to 250ms if zero.
	FlushInterval time.Duration
	// BatchSize is the max number of log lines per request.
	// Defaults to 100 if zero.
	BatchSize int

	mu      sync.Mutex
	buf     []LogEntry
	source  *Source
	cancel  context.CancelFunc
	done    chan struct{}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) flushInterval() time.Duration {
	if c.FlushInterval > 0 {
		return c.FlushInterval
	}
	return 250 * time.Millisecond
}

func (c *Client) batchSize() int {
	if c.BatchSize > 0 {
		return c.BatchSize
	}
	return 100
}

// RegisterSource creates a new log source with the Coder agent.
// It must be called before sending any logs.
func (c *Client) RegisterSource(ctx context.Context, displayName, icon string) (*Source, error) {
	src := &Source{
		ID:          uuid.New(),
		DisplayName: displayName,
		Icon:        icon,
	}

	body, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("marshal source: %w", err)
	}

	url := fmt.Sprintf("%s/api/v2/workspaceagents/me/log-source", c.AgentURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Coder-Session-Token", c.AgentToken)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("register log source: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("register log source: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	c.mu.Lock()
	c.source = src
	c.mu.Unlock()

	return src, nil
}

// Send enqueues a single log entry. Logs are batched and flushed periodically
// once StartFlusher is called, or can be flushed manually with Flush.
func (c *Client) Send(level LogLevel, output string) {
	entry := LogEntry{
		CreatedAt: time.Now().UTC(),
		Level:     level,
		Output:    output,
	}
	c.mu.Lock()
	c.buf = append(c.buf, entry)
	c.mu.Unlock()
}

// StartFlusher begins a background goroutine that periodically sends buffered
// logs. Call Close to stop flushing and send remaining logs.
func (c *Client) StartFlusher(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})

	go func() {
		defer close(c.done)
		ticker := time.NewTicker(c.flushInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Final flush.
				_ = c.Flush(context.Background())
				return
			case <-ticker.C:
				_ = c.Flush(ctx)
			}
		}
	}()
}

// Flush sends all buffered log entries immediately.
func (c *Client) Flush(ctx context.Context) error {
	c.mu.Lock()
	if len(c.buf) == 0 || c.source == nil {
		c.mu.Unlock()
		return nil
	}
	entries := c.buf
	c.buf = nil
	src := c.source
	c.mu.Unlock()

	// Send in batches.
	bs := c.batchSize()
	for i := 0; i < len(entries); i += bs {
		end := i + bs
		if end > len(entries) {
			end = len(entries)
		}
		if err := c.sendBatch(ctx, src.ID, entries[i:end]); err != nil {
			// Re-enqueue unsent entries.
			c.mu.Lock()
			c.buf = append(entries[end:], c.buf...)
			c.mu.Unlock()
			return err
		}
	}
	return nil
}

func (c *Client) sendBatch(ctx context.Context, sourceID uuid.UUID, entries []LogEntry) error {
	payload := patchLogsRequest{
		LogSourceID: sourceID,
		Logs:        entries,
	}
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

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send logs: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Close stops the background flusher and sends any remaining buffered logs.
func (c *Client) Close() error {
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
	return nil
}
