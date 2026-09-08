package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/chat"
	"r-project1/internal/config"
	"r-project1/internal/host"
	"r-project1/internal/llm"
	"r-project1/internal/mcplog"
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
	repl := chat.New(client, os.Stdin, os.Stdout)

	logger, err := mcplog.New(o.logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v, MCP traffic will not be written to disk\n", err)
		logger = mcplog.Discarding()
	}
	defer logger.Close()
	repl.AttachLog(logger)

	// A missing or broken servers file is not fatal: the chatbot is still a
	// usable assistant without any MCP server attached.
	if h := connectServers(ctx, o.serversPath, logger); h != nil {
		defer h.Close()
		repl.AttachHost(h)
	}

	if err := repl.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "chatbot:", err)
		os.Exit(1)
	}
}

func connectServers(ctx context.Context, path string, logger *mcplog.Logger) *host.Host {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v, continuing with no MCP servers\n", err)
		return nil
	}

	h := host.New(host.Options{
		ChildStderr: os.Stderr,
		Log:         logger,
		// Every JSON-RPC frame, including initialize and tools/list, is
		// captured by wrapping the transport.
		Wrap: func(name string, t mcp.Transport) mcp.Transport {
			return &mcp.LoggingTransport{Transport: t, Writer: logger.ProtocolWriter(name)}
		},
	})
	h.Connect(ctx, cfg)

	fmt.Fprintf(os.Stderr, "connected to %d of %d MCP server(s), %d tool(s) available\n",
		h.ConnectedCount(), len(h.Servers()), h.ToolCount())
	if p := logger.Path(); p != "" {
		fmt.Fprintf(os.Stderr, "logging MCP traffic to %s\n", p)
	}
	for _, s := range h.Failures() {
		fmt.Fprintf(os.Stderr, "  %s failed: %v\n", s.Name, s.Err)
	}
	return h
}
