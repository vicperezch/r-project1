package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// These tests need no database: tool registration and the HTTP transport are
// independent of the store, so a nil store is enough to exercise both.

func TestHealthzReportsOK(t *testing.T) {
	ts := httptest.NewServer(NewHTTPHandler(New(nil), func(context.Context) error { return nil }))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["server"] != Name || body["version"] != Version {
		t.Errorf("body %v, want status ok for %s %s", body, Name, Version)
	}
}

func TestHealthzReportsAnUnreachableDatabase(t *testing.T) {
	ts := httptest.NewServer(NewHTTPHandler(New(nil), func(context.Context) error {
		return errors.New("connection refused")
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 so the orchestrator restarts us", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "unhealthy" || body["error"] != "connection refused" {
		t.Errorf("body %v, want the underlying error reported", body)
	}
}

// TestMCPSessionOverHTTP is the one that matters for docker compose and for
// classmates connecting over the network: a real client, a real session, over
// the streamable HTTP transport.
func TestMCPSessionOverHTTP(t *testing.T) {
	ts := httptest.NewServer(NewHTTPHandler(New(nil), nil))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "http-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + MCPPath}, nil)
	if err != nil {
		t.Fatalf("connect over http: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools over http: %v", err)
	}

	got := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)

	want := []string{
		"apply_reassignment", "cancel_flight", "find_reassignment_options",
		"get_booking", "get_flight_details", "list_affected_passengers", "search_flights",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tools %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool %d is %s, want %s", i, got[i], want[i])
		}
	}
}

func TestUnknownPathIs404(t *testing.T) {
	ts := httptest.NewServer(NewHTTPHandler(New(nil), nil))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}
