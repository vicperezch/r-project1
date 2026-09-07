package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadInfersTransportFromShape(t *testing.T) {
	f, err := Load(write(t, `{"mcpServers":{
		"a":{"command":"foo","args":["-x"]},
		"b":{"url":"http://localhost:8080/mcp"}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Servers["a"].ResolvedType(); got != TypeStdio {
		t.Errorf("a resolved to %q, want stdio", got)
	}
	if got := f.Servers["b"].ResolvedType(); got != TypeHTTP {
		t.Errorf("b resolved to %q, want http", got)
	}
}

func TestLoadAcceptsStreamableHTTPAliases(t *testing.T) {
	for _, alias := range []string{"http", "streamable-http", "streamableHttp", "HTTP"} {
		s := Server{Type: alias, URL: "http://x/mcp"}
		if got := s.ResolvedType(); got != TypeHTTP {
			t.Errorf("alias %q resolved to %q, want http", alias, got)
		}
	}
}

func TestNamesAreSortedSoRegistrationIsStable(t *testing.T) {
	f, err := Load(write(t, `{"mcpServers":{
		"zulu":{"command":"z"},"alpha":{"command":"a"},"mike":{"command":"m"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := f.Names()
	want := []string{"alpha", "mike", "zulu"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names %v, want %v", got, want)
		}
	}
}

func TestValidationRejectsIncompleteEntries(t *testing.T) {
	cases := map[string]string{
		"stdio without command": `{"mcpServers":{"a":{"type":"stdio"}}}`,
		"http without url":      `{"mcpServers":{"a":{"type":"http"}}}`,
		"neither":               `{"mcpServers":{"a":{}}}`,
		"unsupported type":      `{"mcpServers":{"a":{"type":"carrier-pigeon","url":"x"}}}`,
	}
	for name, body := range cases {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadRejectsAnEmptyFile(t *testing.T) {
	if _, err := Load(write(t, `{"mcpServers":{}}`)); err == nil {
		t.Error("expected an error for a config with no servers")
	}
}

func TestLoadReportsAMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestTheShippedExampleConfigIsValid(t *testing.T) {
	f, err := Load("../../configs/servers.example.json")
	if err != nil {
		t.Fatalf("the example config must load: %v", err)
	}
	for _, n := range []string{"airline", "filesystem", "git"} {
		if _, ok := f.Servers[n]; !ok {
			t.Errorf("example config is missing the %s server", n)
		}
	}
}

func TestDescribe(t *testing.T) {
	s := Server{Command: "npx", Args: []string{"-y", "pkg"}}
	if got := s.Describe(); got != "stdio npx -y pkg" {
		t.Errorf("describe = %q", got)
	}
	h := Server{URL: "http://x/mcp"}
	if got := h.Describe(); got != "http http://x/mcp" {
		t.Errorf("describe = %q", got)
	}
}
