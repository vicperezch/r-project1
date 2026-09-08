package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"r-project1/internal/host"
	"r-project1/internal/mcplog"
)

// AttachHost registers the commands that inspect connected MCP servers.
//
// Tools are deliberately not advertised to the model yet: an assistant turn
// containing tool_use must be answered with tool_result, so offering tools
// before the tool-use loop exists would break the very next request.
func (r *REPL) AttachHost(h *host.Host) {
	r.host = h
	r.Register(&Command{Name: "servers", Help: "show the configured MCP servers and their status", Run: r.cmdServers})
	r.Register(&Command{Name: "tools", Help: "list the tools discovered on those servers", Run: r.cmdTools})
}

// AttachLog registers the command that shows the MCP interaction log.
func (r *REPL) AttachLog(l *mcplog.Logger) {
	r.log = l
	r.Register(&Command{Name: "log", Help: "show recent MCP traffic, /log 40 for more", Run: r.cmdLog})
}

func (r *REPL) cmdLog(_ context.Context, args string) (bool, error) {
	if r.log == nil {
		fmt.Fprintln(r.out, "no interaction log is running")
		return false, nil
	}

	n := 20
	if args != "" {
		parsed, err := strconv.Atoi(strings.Fields(args)[0])
		if err != nil {
			return false, fmt.Errorf("expected a number of entries, got %q", args)
		}
		n = parsed
	}

	entries := r.log.Recent(n)
	if len(entries) == 0 {
		fmt.Fprintf(r.out, "no MCP traffic logged yet, writing to %s\n", r.log.Path())
		return false, nil
	}
	fmt.Fprintf(r.out, "last %d of %d entries, full log at %s\n\n",
		len(entries), r.log.Count(), r.log.Path())
	for _, e := range entries {
		fmt.Fprintln(r.out, mcplog.Format(e))
	}
	return false, nil
}

func (r *REPL) cmdServers(context.Context, string) (bool, error) {
	if r.host == nil {
		fmt.Fprintln(r.out, "no MCP servers configured")
		return false, nil
	}
	servers := r.host.Servers()
	if len(servers) == 0 {
		fmt.Fprintln(r.out, "no MCP servers configured")
		return false, nil
	}

	fmt.Fprintf(r.out, "%d server(s):\n", len(servers))
	for _, s := range servers {
		fmt.Fprintf(r.out, "  %-14s %-13s %-3d tools  %s\n",
			s.Name, s.Status(), len(s.Tools), s.Config.Describe())
		if s.Err != nil {
			fmt.Fprintf(r.out, "  %-14s   error: %v\n", "", s.Err)
		}
	}
	return false, nil
}

func (r *REPL) cmdTools(_ context.Context, args string) (bool, error) {
	if r.host == nil || r.host.ToolCount() == 0 {
		fmt.Fprintln(r.out, "no tools discovered")
		return false, nil
	}

	filter := strings.ToLower(strings.TrimSpace(args))
	fmt.Fprintf(r.out, "%d tool(s) from %d connected server(s):\n",
		r.host.ToolCount(), r.host.ConnectedCount())

	for _, s := range r.host.Servers() {
		if len(s.Tools) == 0 {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(s.Name), filter) {
			continue
		}
		fmt.Fprintf(r.out, "\n%s (%d):\n", s.Name, len(s.Tools))
		for _, e := range s.Tools {
			fmt.Fprintf(r.out, "  %-42s %s\n", e.Name, firstLine(e.Tool.Description))
		}
	}
	return false, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 70
	if len(s) > max {
		s = s[:max-3] + "..."
	}
	return s
}
