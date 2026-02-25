package coderlog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gerbidigm/coder-logger/coderlog"
)

func TestLogSourceIDFromName(t *testing.T) {
	t.Parallel()

	t.Run("Deterministic", func(t *testing.T) {
		t.Parallel()
		id1 := coderlog.LogSourceIDFromName("cloud-init")
		id2 := coderlog.LogSourceIDFromName("cloud-init")
		if id1 != id2 {
			t.Fatalf("expected same ID, got %s and %s", id1, id2)
		}
	})

	t.Run("DifferentNames", func(t *testing.T) {
		t.Parallel()
		id1 := coderlog.LogSourceIDFromName("cloud-init")
		id2 := coderlog.LogSourceIDFromName("startup-script")
		if id1 == id2 {
			t.Fatalf("expected different IDs for different names, both got %s", id1)
		}
	})
}

func TestEnsureSource(t *testing.T) {
	t.Parallel()

	t.Run("RegistersNewSource", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("test-token")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "test-token",
			CacheDir:   t.TempDir(),
		}

		ctx := context.Background()
		id, err := client.EnsureSource(ctx, "cloud-init", "https://example.com/icon.svg")
		if err != nil {
			t.Fatalf("EnsureSource: %v", err)
		}

		expected := coderlog.LogSourceIDFromName("cloud-init")
		if id != expected {
			t.Fatalf("expected ID %s, got %s", expected, id)
		}

		sources := srv.getSources()
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		src := sources[id]
		if src.DisplayName != "cloud-init" {
			t.Fatalf("expected display name 'cloud-init', got %q", src.DisplayName)
		}
		if src.Icon != "https://example.com/icon.svg" {
			t.Fatalf("expected icon URL, got %q", src.Icon)
		}
	})

	t.Run("IdempotentReRegister", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
			CacheDir:   t.TempDir(),
		}

		ctx := context.Background()
		id1, err := client.EnsureSource(ctx, "my-source", "")
		if err != nil {
			t.Fatalf("first EnsureSource: %v", err)
		}

		// Second call should use cache (no API call).
		id2, err := client.EnsureSource(ctx, "my-source", "")
		if err != nil {
			t.Fatalf("second EnsureSource: %v", err)
		}
		if id1 != id2 {
			t.Fatalf("IDs should match: %s vs %s", id1, id2)
		}

		// Only one source registered on the server (first call hit API, second used cache).
		sources := srv.getSources()
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
	})

	t.Run("CacheMarkerCreated", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		cacheDir := t.TempDir()
		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token-abc",
			CacheDir:   cacheDir,
		}

		ctx := context.Background()
		_, err := client.EnsureSource(ctx, "my-source", "")
		if err != nil {
			t.Fatalf("EnsureSource: %v", err)
		}

		// Verify cache marker file exists.
		entries, _ := filepath.Glob(filepath.Join(cacheDir, "log-sources", "*", "my-source"))
		if len(entries) != 1 {
			t.Fatalf("expected 1 cache marker, found %d", len(entries))
		}
	})

	t.Run("NoCacheDir", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
			// CacheDir intentionally empty
		}

		ctx := context.Background()
		_, err := client.EnsureSource(ctx, "no-cache", "")
		if err != nil {
			t.Fatalf("EnsureSource without cache: %v", err)
		}

		// Second call should still work (hits API again since no cache).
		_, err = client.EnsureSource(ctx, "no-cache", "")
		if err != nil {
			t.Fatalf("second EnsureSource without cache: %v", err)
		}
	})
}

func TestSendLines(t *testing.T) {
	t.Parallel()

	t.Run("SendSingle", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		err := client.SendLines(ctx, id, coderlog.LogLevelInfo, []string{"hello world"})
		if err != nil {
			t.Fatalf("SendLines: %v", err)
		}

		logs := srv.getLogs()
		if len(logs) != 1 {
			t.Fatalf("expected 1 log, got %d", len(logs))
		}
		if logs[0].Output != "hello world" {
			t.Fatalf("expected 'hello world', got %q", logs[0].Output)
		}
		if logs[0].Level != "info" {
			t.Fatalf("expected level 'info', got %q", logs[0].Level)
		}
	})

	t.Run("SendMultiple", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		lines := []string{"line 1", "line 2", "line 3"}
		err := client.SendLines(ctx, id, coderlog.LogLevelWarn, lines)
		if err != nil {
			t.Fatalf("SendLines: %v", err)
		}

		logs := srv.getLogs()
		if len(logs) != 3 {
			t.Fatalf("expected 3 logs, got %d", len(logs))
		}
		for i, l := range logs {
			if l.Output != lines[i] {
				t.Fatalf("log[%d]: expected %q, got %q", i, lines[i], l.Output)
			}
			if l.Level != "warn" {
				t.Fatalf("log[%d]: expected level 'warn', got %q", i, l.Level)
			}
		}
	})

	t.Run("AllLogLevels", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		levels := []coderlog.LogLevel{
			coderlog.LogLevelTrace,
			coderlog.LogLevelDebug,
			coderlog.LogLevelInfo,
			coderlog.LogLevelWarn,
			coderlog.LogLevelError,
		}
		for _, lvl := range levels {
			err := client.SendLines(ctx, id, lvl, []string{"msg"})
			if err != nil {
				t.Fatalf("SendLines level %s: %v", lvl, err)
			}
		}

		logs := srv.getLogs()
		if len(logs) != len(levels) {
			t.Fatalf("expected %d logs, got %d", len(levels), len(logs))
		}
	})
}

func TestOverflow(t *testing.T) {
	t.Parallel()

	t.Run("OverflowOnSend", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()
		// Set a tiny limit to trigger overflow quickly.
		srv.mu.Lock()
		srv.maxLogBytes = 100
		srv.mu.Unlock()

		cacheDir := t.TempDir()
		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
			CacheDir:   cacheDir,
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		// Send enough to overflow.
		bigLine := strings.Repeat("x", 200)
		err := client.SendLines(ctx, id, coderlog.LogLevelInfo, []string{bigLine})
		if !errors.Is(err, coderlog.ErrOverflow) {
			t.Fatalf("expected ErrOverflow, got: %v", err)
		}

		// Verify overflow sentinel was written.
		entries, _ := filepath.Glob(filepath.Join(cacheDir, "log-sources", "*", ".overflow"))
		if len(entries) != 1 {
			t.Fatalf("expected .overflow sentinel, found %d", len(entries))
		}
	})

	t.Run("OverflowBlocksSubsequentSends", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()
		srv.mu.Lock()
		srv.maxLogBytes = 50
		srv.mu.Unlock()

		cacheDir := t.TempDir()
		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
			CacheDir:   cacheDir,
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		// Trigger overflow.
		_ = client.SendLines(ctx, id, coderlog.LogLevelInfo, []string{strings.Repeat("x", 100)})

		// Subsequent send should fail immediately without API call.
		err := client.SendLines(ctx, id, coderlog.LogLevelInfo, []string{"small"})
		if !errors.Is(err, coderlog.ErrOverflow) {
			t.Fatalf("expected ErrOverflow on subsequent send, got: %v", err)
		}
	})

	t.Run("OverflowBlocksEnsureSource", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		cacheDir := t.TempDir()
		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
			CacheDir:   cacheDir,
		}

		// Manually create overflow sentinel.
		scopeDir := filepath.Join(cacheDir, "log-sources")
		// We need the actual scope dir — just create one with .overflow.
		os.MkdirAll(filepath.Join(scopeDir, "dummy"), 0o700)

		// Actually, use the client to trigger overflow properly.
		srv.mu.Lock()
		srv.maxLogBytes = 10
		srv.mu.Unlock()

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")
		_ = client.SendLines(ctx, id, coderlog.LogLevelInfo, []string{strings.Repeat("x", 100)})

		// Now EnsureSource should also fail.
		_, err := client.EnsureSource(ctx, "new-source", "")
		if !errors.Is(err, coderlog.ErrOverflow) {
			t.Fatalf("expected ErrOverflow from EnsureSource, got: %v", err)
		}
	})
}

func TestStreamReader(t *testing.T) {
	t.Parallel()

	t.Run("StreamsLines", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		input := "line one\nline two\nline three\n"
		reader := strings.NewReader(input)

		err := client.StreamReader(ctx, reader, id, coderlog.LogLevelDebug)
		if err != nil {
			t.Fatalf("StreamReader: %v", err)
		}

		logs := srv.getLogs()
		if len(logs) != 3 {
			t.Fatalf("expected 3 logs, got %d", len(logs))
		}
		expected := []string{"line one", "line two", "line three"}
		for i, l := range logs {
			if l.Output != expected[i] {
				t.Fatalf("log[%d]: expected %q, got %q", i, expected[i], l.Output)
			}
			if l.Level != "debug" {
				t.Fatalf("log[%d]: expected level 'debug', got %q", i, l.Level)
			}
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		err := client.StreamReader(ctx, strings.NewReader(""), id, coderlog.LogLevelInfo)
		if err != nil {
			t.Fatalf("StreamReader empty: %v", err)
		}

		logs := srv.getLogs()
		if len(logs) != 0 {
			t.Fatalf("expected 0 logs, got %d", len(logs))
		}
	})

	t.Run("OverflowDuringStream", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("")
		defer srv.Close()
		srv.mu.Lock()
		srv.maxLogBytes = 20
		srv.mu.Unlock()

		cacheDir := t.TempDir()
		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "token",
			CacheDir:   cacheDir,
		}

		ctx := context.Background()
		id, _ := client.EnsureSource(ctx, "test", "")

		input := "short\n" + strings.Repeat("x", 100) + "\n"
		err := client.StreamReader(ctx, strings.NewReader(input), id, coderlog.LogLevelInfo)
		if err == nil {
			t.Fatal("expected error from overflow during stream")
		}
	})
}

func TestAuthentication(t *testing.T) {
	t.Parallel()

	t.Run("WrongToken", func(t *testing.T) {
		t.Parallel()
		srv := newMockServer("correct-token")
		defer srv.Close()

		client := &coderlog.Client{
			AgentURL:   srv.URL(),
			AgentToken: "wrong-token",
		}

		ctx := context.Background()
		_, err := client.EnsureSource(ctx, "test", "")
		if err == nil {
			t.Fatal("expected auth error")
		}
		if !strings.Contains(err.Error(), "401") {
			t.Fatalf("expected 401 in error, got: %v", err)
		}
	})
}
