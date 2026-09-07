package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"r-project1/internal/chat"
	"r-project1/internal/llm"
)

const systemPrompt = `You are a terminal assistant for airline counter and call center staff.

You have access to MCP servers, and the tools they expose appear as tools named server__tool.
Prefer calling a tool over guessing. When a tool reports an error, read it and correct the call
rather than repeating it.

Keep answers short and scannable: this is a terminal. Use plain text, no markdown tables.
When you list flights or passengers, keep the tool's own formatting, it is already aligned.

You also answer ordinary questions that have nothing to do with the airline.`

type options struct {
	serversPath string
	model       string
	logDir      string
	autoApprove bool
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.serversPath, "servers", "configs/servers.example.json", "path to the MCP servers config file")
	flag.StringVar(&o.model, "model", envOr("CHATBOT_MODEL", llm.DefaultModel), "Claude model id")
	flag.StringVar(&o.logDir, "log-dir", envOr("CHATBOT_LOG_DIR", "logs"), "directory for MCP interaction logs")
	flag.BoolVar(&o.autoApprove, "yes", false, "skip the confirmation prompt for state-changing tools")
	flag.Parse()
	return o
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	o := parseFlags()

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		// Not fatal: the SDK also reads credentials from an ant auth profile.
		fmt.Fprintln(os.Stderr, "warning: ANTHROPIC_API_KEY is not set, requests may fail")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := llm.New(llm.Config{Model: o.model, System: systemPrompt})

	if err := chat.New(client, os.Stdin, os.Stdout).Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "chatbot:", err)
		os.Exit(1)
	}
}
