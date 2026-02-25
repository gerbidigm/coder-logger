// Command coder-logger sends log lines to Coder workspace startup logs.
//
// Usage:
//
//	# Send a single message
//	coder-logger send --source cloud-init "Installing dependencies..."
//
//	# Pipe logs from stdin
//	tail -f /var/log/cloud-init.log | coder-logger send --source cloud-init
//
//	# Pre-register with a custom icon
//	coder-logger register --name cloud-init --icon https://example.com/icon.svg
//
// Required environment variables:
//
//	CODER_AGENT_TOKEN  — workspace agent session token
//	CODER_AGENT_URL    — base URL of the Coder deployment
package main

import (
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
	log.SetFlags(0)
	log.SetPrefix("coder-logger: ")

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "register":
		cmdRegister(os.Args[2:])
	case "send":
		cmdSend(os.Args[2:])
	case "help", "--help", "-h":
		usage()
	default:
		log.Fatalf("unknown command %q. Use 'register' or 'send'.", os.Args[1])
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: coder-logger <command> [flags]

Commands:
  register   Register a named log source with the agent API.
  send       Send log lines to an agent log source.

Environment:
  CODER_AGENT_URL    Base URL of the Coder deployment (required)
  CODER_AGENT_TOKEN  Workspace agent session token (required)
`)
}

func newClient() *coderlog.Client {
	agentURL := os.Getenv("CODER_AGENT_URL")
	agentToken := os.Getenv("CODER_AGENT_TOKEN")
	if agentURL == "" {
		log.Fatal("CODER_AGENT_URL is required")
	}
	if agentToken == "" {
		log.Fatal("CODER_AGENT_TOKEN is required")
	}

	cacheDir, _ := os.UserConfigDir()
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}
	cacheDir = strings.TrimRight(cacheDir, "/")

	return &coderlog.Client{
		AgentURL:   strings.TrimRight(agentURL, "/"),
		AgentToken: agentToken,
		CacheDir:   cacheDir,
	}
}

func cmdRegister(args []string) {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	name := fs.String("name", "", "Log source name (required)")
	icon := fs.String("icon", "", "Icon URL for the log source")
	fs.Parse(args)

	if *name == "" {
		log.Fatal("--name is required")
	}

	client := newClient()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	id, err := client.EnsureSource(ctx, *name, *icon)
	if err != nil {
		log.Fatalf("Failed to register source: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Registered log source %q (id=%s)\n", *name, id)
}

func cmdSend(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	source := fs.String("source", "", "Log source name (required)")
	icon := fs.String("icon", "", "Icon URL for the log source")
	level := fs.String("level", "info", "Log level: trace, debug, info, warn, error, fatal")
	fs.Parse(args)

	if *source == "" {
		log.Fatal("--source is required")
	}

	client := newClient()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sourceID, err := client.EnsureSource(ctx, *source, *icon)
	if err != nil {
		log.Fatalf("Failed to ensure source: %v", err)
	}

	logLevel := coderlog.LogLevel(*level)

	// Args mode: join remaining args as a single log line.
	if fs.NArg() > 0 {
		msg := strings.Join(fs.Args(), " ")
		if err := client.SendLines(ctx, sourceID, logLevel, []string{msg}); err != nil {
			log.Fatalf("Failed to send: %v", err)
		}
		return
	}

	// Stdin mode: stream lines with batching.
	fmt.Fprintln(os.Stderr, "Reading from stdin...")
	if err := client.StreamReader(ctx, os.Stdin, sourceID, logLevel); err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Fatalf("Stream error: %v", err)
	}
}
