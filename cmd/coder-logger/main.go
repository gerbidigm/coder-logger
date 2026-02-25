// Command coder-logger streams log lines to a Coder workspace's startup logs.
//
// It can tail a file or read from stdin:
//
//	coder-logger --log-file /var/log/cloud-init.log
//	some-command | coder-logger
//
// Required environment variables:
//
//	CODER_AGENT_TOKEN  — workspace agent session token
//	CODER_AGENT_URL    — base URL of the Coder deployment
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gerbidigm/coder-logger/coderlog"
)

func main() {
	logFile := flag.String("log-file", "", "Path to a log file to tail (omit to read stdin)")
	sourceName := flag.String("source-name", "cloud_init", "Display name for the log source")
	sourceIcon := flag.String("source-icon", "https://cloud-init.github.io/images/cloud-init-orange.svg", "Icon URL for the log source")
	level := flag.String("level", "info", "Log level: trace, debug, info, warn, error, fatal")
	flag.Parse()

	agentURL := os.Getenv("CODER_AGENT_URL")
	agentToken := os.Getenv("CODER_AGENT_TOKEN")

	if agentURL == "" {
		log.Fatal("CODER_AGENT_URL is required")
	}
	if agentToken == "" {
		log.Fatal("CODER_AGENT_TOKEN is required")
	}

	// Trim trailing slash.
	agentURL = strings.TrimRight(agentURL, "/")

	logLevel := coderlog.LogLevel(*level)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := &coderlog.Client{
		AgentURL:   agentURL,
		AgentToken: agentToken,
	}

	_, err := client.RegisterSource(ctx, *sourceName, *sourceIcon)
	if err != nil {
		log.Fatalf("Failed to register log source: %v", err)
	}

	client.StartFlusher(ctx)
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: error closing client: %v", err)
		}
	}()

	if *logFile != "" {
		fmt.Fprintf(os.Stderr, "Tailing %s...\n", *logFile)
		if err := coderlog.TailFile(ctx, client, *logFile, logLevel); err != nil {
			if ctx.Err() != nil {
				return // clean shutdown
			}
			log.Fatalf("Tail error: %v", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Reading from stdin...")
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			client.Send(logLevel, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Fatalf("Stdin error: %v", err)
		}
	}
}
