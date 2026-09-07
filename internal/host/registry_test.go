package host

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNamespacedNameFoldsIllegalCharacters(t *testing.T) {
	cases := map[string]string{
		"plain":       NamespacedName("airline", "search_flights"),
		"dots":        NamespacedName("my.server", "do.thing"),
		"spaces":      NamespacedName("my server", "do thing"),
		"punctuation": NamespacedName("a/b:c", "x@y!z"),
	}
	want := map[string]string{
		"plain":       "airline__search_flights",
		"dots":        "my_server__do_thing",
		"spaces":      "my_server__do_thing",
		"punctuation": "a_b_c__x_y_z",
	}
	for k, got := range cases {
		if got != want[k] {
			t.Errorf("%s: got %q, want %q", k, got, want[k])
		}
	}
}

func TestNamespacedNamesAlwaysMatchTheAnthropicPattern(t *testing.T) {
	long := strings.Repeat("x", 200)
	for _, name := range []string{
		NamespacedName("airline", "search"),
		NamespacedName(long, long),
		NamespacedName("", ""),
		NamespacedName("ünïcødé", "tøøl"),
	} {
		if len(name) == 0 || len(name) > MaxToolNameLen {
			t.Errorf("name %q has length %d, must be 1 to %d", name, len(name), MaxToolNameLen)
		}
		for _, c := range name {
			ok := c == '_' || c == '-' ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !ok {
				t.Errorf("name %q contains illegal character %q", name, c)
			}
		}
	}
}

func TestRegistryResolvesCollisions(t *testing.T) {
	r := NewRegistry()
	// Two different server names that sanitize to the same prefix.
	a := r.Add("my.server", nil, &mcp.Tool{Name: "run"})
	b := r.Add("my_server", nil, &mcp.Tool{Name: "run"})
	c := r.Add("my server", nil, &mcp.Tool{Name: "run"})

	if a.Name != "my_server__run" {
		t.Errorf("first got %q", a.Name)
	}
	if b.Name != "my_server__run_2" {
		t.Errorf("second got %q, want a _2 suffix", b.Name)
	}
	if c.Name != "my_server__run_3" {
		t.Errorf("third got %q, want a _3 suffix", c.Name)
	}
	if r.Len() != 3 {
		t.Errorf("registry has %d entries, want 3", r.Len())
	}
	// Every name must still resolve to the right underlying tool.
	for _, e := range []*Entry{a, b, c} {
		got, ok := r.Get(e.Name)
		if !ok || got.ServerName != e.ServerName || got.ToolName != "run" {
			t.Errorf("lookup of %q did not round trip", e.Name)
		}
	}
}

func TestInputSchemaBridging(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"origin": map[string]any{"type": "string", "description": "where from"},
			"limit":  map[string]any{"type": "integer"},
		},
		"required": []any{"origin"},
	}
	got := InputSchema(raw)

	props, ok := got.Properties.(map[string]any)
	if !ok {
		t.Fatalf("properties is %T, want a map", got.Properties)
	}
	if _, ok := props["origin"]; !ok {
		t.Error("origin property was dropped")
	}
	if len(got.Required) != 1 || got.Required[0] != "origin" {
		t.Errorf("required = %v, want [origin]", got.Required)
	}
}

func TestInputSchemaNeverProducesNilProperties(t *testing.T) {
	// The API rejects an object schema whose properties are null, so a server
	// that advertises no arguments must still bridge to an empty object.
	for name, raw := range map[string]any{
		"nil":            nil,
		"empty map":      map[string]any{},
		"no properties":  map[string]any{"type": "object"},
		"null propertes": map[string]any{"type": "object", "properties": nil},
	} {
		got := InputSchema(raw)
		props, ok := got.Properties.(map[string]any)
		if !ok || props == nil {
			t.Errorf("%s: properties is %#v, want an empty map", name, got.Properties)
		}
	}
}

func TestInputSchemaSurvivesATypedSchema(t *testing.T) {
	// A schema that arrives as a struct rather than a map must still bridge,
	// which is why the conversion round trips through JSON.
	type typedSchema struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	got := InputSchema(typedSchema{
		Type:       "object",
		Properties: map[string]any{"pnr": map[string]any{"type": "string"}},
		Required:   []string{"pnr"},
	})
	props := got.Properties.(map[string]any)
	if _, ok := props["pnr"]; !ok {
		t.Error("pnr property was dropped from a typed schema")
	}
	if len(got.Required) != 1 {
		t.Errorf("required = %v", got.Required)
	}
}

func TestToolParamsCarryServerPrefixedDescriptions(t *testing.T) {
	r := NewRegistry()
	r.Add("airline", nil, &mcp.Tool{Name: "search", Description: "Find flights"})
	r.Add("git", nil, &mcp.Tool{Name: "search"})

	params := r.ToolParams()
	if len(params) != 2 {
		t.Fatalf("got %d tool params, want 2", len(params))
	}
	first := params[0].OfTool
	if first.Name != "airline__search" {
		t.Errorf("name %q", first.Name)
	}
	if !strings.Contains(first.Description.Value, "[airline]") {
		t.Errorf("description %q should name the server", first.Description.Value)
	}
	// A tool with no description falls back to its own name, never empty.
	if second := params[1].OfTool; !strings.Contains(second.Description.Value, "search") {
		t.Errorf("description %q", second.Description.Value)
	}
}

func TestParseArgs(t *testing.T) {
	got, err := ParseArgs(`{"origin":"GUA","limit":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if got["origin"] != "GUA" {
		t.Errorf("origin = %v", got["origin"])
	}

	// The model sends an empty object for a no-argument tool, and some send
	// nothing at all.
	for _, raw := range []string{"", "  ", "{}", "null"} {
		got, err := ParseArgs(raw)
		if err != nil {
			t.Errorf("ParseArgs(%q): %v", raw, err)
		}
		if got == nil {
			t.Errorf("ParseArgs(%q) returned a nil map", raw)
		}
	}

	if _, err := ParseArgs(`not json`); err == nil {
		t.Error("expected an error for malformed input")
	}
}

func TestTruncateResult(t *testing.T) {
	s, truncated := truncateResult("hello", 100)
	if truncated || s != "hello" {
		t.Errorf("short input should pass through")
	}

	long := strings.Repeat("a", 500)
	s, truncated = truncateResult(long, 100)
	if !truncated {
		t.Fatal("long input should be truncated")
	}
	if !strings.Contains(s, "truncated") || !strings.Contains(s, "500") {
		t.Errorf("truncation notice missing the real size: %q", s[len(s)-60:])
	}
}

func TestEntryJSONRoundTripIsNotNeeded(t *testing.T) {
	// Guards the assumption that a client side schema is plain JSON data.
	raw := json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`)
	got := InputSchema(raw)
	props := got.Properties.(map[string]any)
	if _, ok := props["a"]; !ok {
		t.Error("a json.RawMessage schema should bridge")
	}
}
