package main

import (
	"flag"
	"os"
	"testing"
)

func parseWith(t *testing.T, args ...string) (options, error) {
	t.Helper()
	old := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = old })
	flag.CommandLine = flag.NewFlagSet("airline-mcp", flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stderr)

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = append([]string{"airline-mcp"}, args...)

	return parseFlags()
}

func TestParseFlagsDefaultsToStdio(t *testing.T) {
	o, err := parseWith(t)
	if err != nil {
		t.Fatal(err)
	}
	if o.transport != "stdio" {
		t.Errorf("transport %q, want stdio", o.transport)
	}
	if o.addr != ":8080" {
		t.Errorf("addr %q, want :8080", o.addr)
	}
}

func TestParseFlagsAcceptsHTTP(t *testing.T) {
	o, err := parseWith(t, "-transport", "http", "-addr", ":9000")
	if err != nil {
		t.Fatal(err)
	}
	if o.transport != "http" || o.addr != ":9000" {
		t.Errorf("got %+v", o)
	}
}

func TestParseFlagsRejectsAnUnknownTransport(t *testing.T) {
	if _, err := parseWith(t, "-transport", "carrier-pigeon"); err == nil {
		t.Fatal("expected an error for an unknown transport")
	}
}

func TestParseFlagsReadsDatabaseURLFromTheEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example/db")
	o, err := parseWith(t)
	if err != nil {
		t.Fatal(err)
	}
	if o.databaseURL != "postgres://example/db" {
		t.Errorf("databaseURL = %q", o.databaseURL)
	}
}
