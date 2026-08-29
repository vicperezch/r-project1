package main

import (
	"flag"
	"fmt"
	"os"
)

type options struct {
	serversPath string
	model       string
	logDir      string
	autoApprove bool
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.serversPath, "servers", "configs/servers.example.json", "path to the MCP servers config file")
	flag.StringVar(&o.model, "model", envOr("CHATBOT_MODEL", "claude-haiku-4-5"), "Claude model id")
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

	fmt.Println("airline mcp chatbot")
	fmt.Printf("  servers config: %s\n", o.serversPath)
	fmt.Printf("  model:          %s\n", o.model)
	fmt.Printf("  log dir:        %s\n", o.logDir)
	fmt.Printf("  auto approve:   %t\n", o.autoApprove)
	fmt.Println("\nnot wired up yet, see the commit sequence in README.md")
}
