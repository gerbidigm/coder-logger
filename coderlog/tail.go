package coderlog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// TailFile watches a file for new lines (similar to tail -f) and sends each
// line to the Client at the given log level. It blocks until the context is
// cancelled. If the file does not yet exist, it retries until it appears or
// the context is cancelled.
func TailFile(ctx context.Context, client *Client, path string, level LogLevel) error {
	var f *os.File
	var err error

	// Wait for the file to appear.
	for {
		f, err = os.Open(path)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("open %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer f.Close()

	// Seek to end — we only want new content.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek %s: %w", path, err)
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			// Trim trailing newline.
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 {
				client.Send(level, line)
			}
		}
		if err != nil {
			if err == io.EOF {
				// No new data yet — poll.
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
					continue
				}
			}
			return fmt.Errorf("read %s: %w", path, err)
		}
	}
}
