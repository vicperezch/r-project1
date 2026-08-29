package main

import (
	"flag"
	"fmt"
	"os"
)

type options struct {
	transport   string
	addr        string
	databaseURL string
}

func parseFlags() (options, error) {
	var o options
	flag.StringVar(&o.transport, "transport", "stdio", "transport to serve on, either stdio or http")
	flag.StringVar(&o.addr, "addr", ":8080", "listen address, used only when transport is http")
	flag.Parse()

	o.databaseURL = os.Getenv("DATABASE_URL")

	if o.transport != "stdio" && o.transport != "http" {
		return o, fmt.Errorf("unknown transport %q, expected stdio or http", o.transport)
	}
	return o, nil
}

func main() {
	o, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	// stdio carries the protocol on stdout, so diagnostics must go to stderr.
	fmt.Fprintln(os.Stderr, "airline mcp server")
	fmt.Fprintf(os.Stderr, "  transport: %s\n", o.transport)
	if o.transport == "http" {
		fmt.Fprintf(os.Stderr, "  addr:      %s\n", o.addr)
	}
	fmt.Fprintf(os.Stderr, "  database:  %s\n", databaseStatus(o.databaseURL))
	fmt.Fprintln(os.Stderr, "\nnot wired up yet, see the commit sequence in README.md")
}

func databaseStatus(url string) string {
	if url == "" {
		return "DATABASE_URL not set"
	}
	return "configured"
}
