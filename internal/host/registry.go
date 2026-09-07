package host

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Anthropic tool names must match ^[a-zA-Z0-9_-]{1,128}$, but MCP servers are
// free to name tools anything. Everything outside that set is folded to _.
const (
	MaxToolNameLen  = 128
	NameSeparator   = "__"
	maxServerPrefix = 40
)

var invalidNameChar = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// Entry is one tool from one server, under the name the model sees.
type Entry struct {
	Name       string
	ServerName string
	ToolName   string
	Tool       *mcp.Tool
	session    *mcp.ClientSession
}

type Registry struct {
	entries []*Entry
	byName  map[string]*Entry
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]*Entry{}}
}

// Add registers a tool under server__tool, resolving collisions with a numeric
// suffix. Two servers exporting the same tool name is expected, not an error.
func (r *Registry) Add(serverName string, session *mcp.ClientSession, t *mcp.Tool) *Entry {
	base := NamespacedName(serverName, t.Name)
	name := base
	for i := 2; ; i++ {
		if _, taken := r.byName[name]; !taken {
			break
		}
		suffix := fmt.Sprintf("_%d", i)
		name = truncate(base, MaxToolNameLen-len(suffix)) + suffix
	}

	e := &Entry{Name: name, ServerName: serverName, ToolName: t.Name, Tool: t, session: session}
	r.entries = append(r.entries, e)
	r.byName[name] = e
	return e
}

func (r *Registry) Get(name string) (*Entry, bool) {
	e, ok := r.byName[name]
	return e, ok
}

func (r *Registry) Entries() []*Entry { return r.entries }

func (r *Registry) Len() int { return len(r.entries) }

// ToolParams converts every registered tool into an Anthropic tool definition.
func (r *Registry) ToolParams() []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(r.entries))
	for _, e := range r.entries {
		p := anthropic.ToolParam{
			Name:        e.Name,
			Description: anthropic.String(describe(e)),
			InputSchema: InputSchema(e.Tool.InputSchema),
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &p})
	}
	return out
}

// describe prefixes the server name so the model can tell two similarly named
// tools from different servers apart.
func describe(e *Entry) string {
	d := strings.TrimSpace(e.Tool.Description)
	if d == "" {
		d = e.ToolName
	}
	return fmt.Sprintf("[%s] %s", e.ServerName, d)
}

func NamespacedName(server, tool string) string {
	s := truncate(invalidNameChar.ReplaceAllString(server, "_"), maxServerPrefix)
	t := invalidNameChar.ReplaceAllString(tool, "_")
	name := s + NameSeparator + t
	if name == NameSeparator {
		return "tool"
	}
	return truncate(name, MaxToolNameLen)
}

func truncate(s string, n int) string {
	if n < 1 {
		n = 1
	}
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// InputSchema converts an MCP tool's JSON Schema into the Anthropic shape.
//
// mcp.Tool.InputSchema is typed any. Read from a client session it holds a
// map[string]any, but a server-side value can be a typed schema struct, so this
// round trips through JSON rather than asserting on a concrete type.
func InputSchema(raw any) anthropic.ToolInputSchemaParam {
	// An object schema with no properties is still required to be an object,
	// so the zero value is an empty map, never nil.
	out := anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
	if raw == nil {
		return out
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return out
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return out
	}
	if schema.Properties != nil {
		out.Properties = schema.Properties
	}
	out.Required = schema.Required
	return out
}
