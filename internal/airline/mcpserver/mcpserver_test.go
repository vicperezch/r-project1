package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/store"
)

// These tests drive the tools through a real MCP session over an in-memory
// transport, so they cover schema inference and the protocol round trip, not
// just the handler bodies. They need the seeded database:
//
//	make db-up
//	make test-db
func sessionWithStore(t *testing.T, st *store.Store) (*mcp.ClientSession, context.Context) {
	t.Helper()
	ctx := context.Background()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(st).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session, ctx
}

// testSession needs the seeded database. Tool registration and schema
// inference do not, so those tests use sessionWithStore(t, nil) instead and
// run everywhere.
func testSession(t *testing.T) (*mcp.ClientSession, context.Context) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping mcp server integration tests")
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect store: %v", err)
	}
	t.Cleanup(st.Close)
	return sessionWithStore(t, st)
}

func callText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func call(t *testing.T, s *mcp.ClientSession, ctx context.Context, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

func TestListToolsExposesFlightTools(t *testing.T) {
	s, ctx := sessionWithStore(t, nil)

	res, err := s.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	for _, name := range []string{
		"search_flights", "get_flight_details",
		"cancel_flight", "list_affected_passengers", "get_booking",
	} {
		tool, ok := got[name]
		if !ok {
			t.Fatalf("tool %q not advertised, got %v", name, got)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has no description", name)
		}
	}

	// Schema inference must mark origin and destination required, and leave the
	// optional fields out of required.
	raw, err := json.Marshal(got["search_flights"].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if strings.Join(schema.Required, ",") != "origin,destination" {
		t.Errorf("required = %v, want [origin destination]", schema.Required)
	}
	for _, p := range []string{"origin", "destination", "date", "include_cancelled"} {
		if _, ok := schema.Properties[p]; !ok {
			t.Errorf("property %q missing from input schema", p)
		}
	}
}

func TestSearchFlightsAcceptsCityNames(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "search_flights", map[string]any{
		"origin": "Guatemala City", "destination": "Mexico City", "date": "2026-09-14",
	})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}

	text := callText(t, res)
	for _, id := range []string{"AV201-2026-09-14", "AV203-2026-09-14", "AV205-2026-09-14"} {
		if !strings.Contains(text, id) {
			t.Errorf("text content missing %s:\n%s", id, text)
		}
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out searchFlightsOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 3 || len(out.Flights) != 3 {
		t.Fatalf("count %d, flights %d, want 3 and 3", out.Count, len(out.Flights))
	}
	if out.Origin.Code != "GUA" || out.Destination.Code != "MEX" {
		t.Errorf("resolved %s to %s, want GUA to MEX", out.Origin.Code, out.Destination.Code)
	}
	if f := out.Flights[0]; f.SeatsAvailable != 2 || f.EconomyAvailable != 2 || f.BusinessAvailable != 0 {
		t.Errorf("AV201 availability = %+v, want 2 total, 0 business, 2 economy", f)
	}
}

func TestSearchFlightsAmbiguousCityIsReported(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "search_flights", map[string]any{"origin": "San", "destination": "GUA"})
	if !res.IsError {
		t.Fatalf("expected a tool error for an ambiguous city, got: %s", callText(t, res))
	}
	text := callText(t, res)
	if !strings.Contains(text, "SAL") || !strings.Contains(text, "SJO") {
		t.Errorf("ambiguity message should name both candidates, got: %s", text)
	}
}

func TestSearchFlightsUnknownCityIsReported(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "search_flights", map[string]any{"origin": "Atlantis", "destination": "GUA"})
	if !res.IsError {
		t.Fatalf("expected a tool error, got: %s", callText(t, res))
	}
}

func TestSearchFlightsRejectsMalformedDate(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "search_flights", map[string]any{
		"origin": "GUA", "destination": "MEX", "date": "14/09/2026",
	})
	if !res.IsError {
		t.Fatalf("expected a tool error for a bad date, got: %s", callText(t, res))
	}
}

func TestSearchFlightsMissingRequiredArgIsRejected(t *testing.T) {
	s, ctx := testSession(t)

	// Schema validation happens before the handler runs.
	res := call(t, s, ctx, "search_flights", map[string]any{"origin": "GUA"})
	if !res.IsError {
		t.Fatalf("expected a validation error for a missing destination, got: %s", callText(t, res))
	}
}

func TestGetFlightDetails(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "get_flight_details", map[string]any{"flight_id": "AV201-2026-09-14"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}

	text := callText(t, res)
	for _, want := range []string{"AV201-2026-09-14", "GUA (Guatemala City)", "18 of 20", "90% full"} {
		if !strings.Contains(text, want) {
			t.Errorf("details text missing %q:\n%s", want, text)
		}
	}

	raw, _ := json.Marshal(res.StructuredContent)
	var out getFlightDetailsOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.LoadFactor != 0.9 {
		t.Errorf("load factor %v, want 0.9", out.LoadFactor)
	}
	if out.Flight.DurationMinutes != 150 {
		t.Errorf("duration %d minutes, want 150", out.Flight.DurationMinutes)
	}
}

func TestGetFlightDetailsUnknownID(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "get_flight_details", map[string]any{"flight_id": "NOPE-2026-01-01"})
	if !res.IsError {
		t.Fatalf("expected a tool error, got: %s", callText(t, res))
	}
	if !strings.Contains(callText(t, res), "search_flights") {
		t.Errorf("error should point at search_flights, got: %s", callText(t, res))
	}
}
