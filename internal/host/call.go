package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Result is a tool call flattened into what the Anthropic API wants back.
type Result struct {
	ServerName string
	ToolName   string
	Text       string
	IsError    bool
	Truncated  bool
	Duration   time.Duration
}

// CallTool dispatches a namespaced tool call to the session that owns it.
//
// A tool that fails is not an error here: the failure text goes back to the
// model as an error result so it can correct itself. An error return means the
// call could not be made at all.
func (h *Host) CallTool(ctx context.Context, name string, args map[string]any) (Result, error) {
	entry, ok := h.registry.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("no tool named %q is registered", name)
	}

	res := Result{ServerName: entry.ServerName, ToolName: entry.ToolName}

	callCtx, cancel := context.WithTimeout(ctx, h.opts.CallTimeout)
	defer cancel()

	start := time.Now()
	out, err := entry.session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      entry.ToolName,
		Arguments: args,
	})
	res.Duration = time.Since(start)
	if err != nil {
		return res, fmt.Errorf("call %s: %w", name, err)
	}

	res.IsError = out.IsError
	res.Text, res.Truncated = h.flatten(out)
	return res, nil
}

// flatten turns MCP content blocks into the single string a tool_result block
// carries, and caps the size. An unbounded read_file would otherwise eat the
// context window.
func (h *Host) flatten(out *mcp.CallToolResult) (string, bool) {
	var b strings.Builder
	for _, c := range out.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			b.WriteString(v.Text)
			b.WriteByte('\n')
		case *mcp.ImageContent:
			fmt.Fprintf(&b, "[image content, %s, %d bytes, not shown]\n", v.MIMEType, len(v.Data))
		case *mcp.AudioContent:
			fmt.Fprintf(&b, "[audio content, %s, %d bytes, not shown]\n", v.MIMEType, len(v.Data))
		case *mcp.ResourceLink:
			fmt.Fprintf(&b, "[resource link %s]\n", v.URI)
		case *mcp.EmbeddedResource:
			fmt.Fprintf(&b, "[embedded resource %s]\n", resourceURI(v))
		default:
			fmt.Fprintf(&b, "[unsupported content of type %T]\n", c)
		}
	}

	// Some servers answer only with structured content.
	text := strings.TrimRight(b.String(), "\n")
	if text == "" && out.StructuredContent != nil {
		if encoded, err := json.Marshal(out.StructuredContent); err == nil {
			text = string(encoded)
		}
	}
	if text == "" {
		text = "(the tool returned no content)"
	}
	return truncateResult(text, h.opts.MaxResultBytes)
}

func resourceURI(e *mcp.EmbeddedResource) string {
	if e.Resource == nil {
		return "unknown"
	}
	return e.Resource.URI
}

func truncateResult(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	return s[:max] + fmt.Sprintf("\n\n[truncated, %d of %d bytes shown]", max, len(s)), true
}

// ParseArgs turns the raw JSON the model produced into tool arguments. Tool
// input must be parsed as JSON, never string matched, because escaping varies.
func ParseArgs(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("tool input is not a JSON object: %w", err)
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}
