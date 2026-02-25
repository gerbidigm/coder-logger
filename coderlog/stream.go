package coderlog

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultBatchSize is the max lines buffered before a flush.
	DefaultBatchSize = 50
	// DefaultFlushInterval is the max time between flushes.
	DefaultFlushInterval = 250 * time.Millisecond
)

// StreamReader reads lines from r and sends them in batches to the Coder agent.
// It flushes every DefaultFlushInterval or DefaultBatchSize lines, whichever
// comes first. Blocks until r is exhausted or ctx is cancelled.
func (c *Client) StreamReader(ctx context.Context, r io.Reader, sourceID uuid.UUID, level LogLevel) error {
	scanner := bufio.NewScanner(r)
	var batch []string
	ticker := time.NewTicker(DefaultFlushInterval)
	defer ticker.Stop()

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		lines := batch
		batch = nil
		return c.SendLines(ctx, sourceID, level, lines)
	}

	// lines is a channel that delivers scanned lines (or signals EOF).
	type scanResult struct {
		line string
		done bool
	}
	ch := make(chan scanResult, DefaultBatchSize)

	go func() {
		defer close(ch)
		for scanner.Scan() {
			select {
			case ch <- scanResult{line: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return flush()
		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		case result, ok := <-ch:
			if !ok {
				// EOF — final flush.
				if err := flush(); err != nil {
					return err
				}
				return scanner.Err()
			}
			batch = append(batch, result.line)
			if len(batch) >= DefaultBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}
