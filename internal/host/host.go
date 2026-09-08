// Package host is the MCP host: it connects to every configured server, keeps
// one registry of all their tools, and dispatches calls to the right session.
//
// Nothing here knows about the airline server specifically, which is what lets
// the chatbot talk to MCP servers written by anyone else.
package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/config"
	"r-project1/internal/mcplog"
)

const (
	DefaultConnectTimeout = 30 * time.Second
	DefaultCallTimeout    = 60 * time.Second
	DefaultMaxResultBytes = 8192
)

// Server is one configured MCP server and how connecting to it went.
type Server struct {
	Name    string
	Config  config.Server
	Session *mcp.ClientSession
	Err     error
	Tools   []*Entry

	cmd *exec.Cmd
}

func (s *Server) Connected() bool { return s.Session != nil && s.Err == nil }

func (s *Server) Status() string {
	switch {
	case s.Config.Disabled:
		return "disabled"
	case s.Err != nil:
		return "failed"
	case s.Session != nil:
		return "connected"
	default:
		return "not connected"
	}
}

type Options struct {
	ClientName     string
	ClientVersion  string
	ConnectTimeout time.Duration
	CallTimeout    time.Duration
	MaxResultBytes int
	// ChildStderr receives the stderr of stdio subprocesses. Without it a
	// misconfigured server fails silently.
	ChildStderr io.Writer
	// Wrap lets the interaction log sit between the session and the transport.
	Wrap func(serverName string, t mcp.Transport) mcp.Transport
	// Log records tool calls. Protocol frames are recorded separately by Wrap.
	Log *mcplog.Logger
}

type Host struct {
	opts     Options
	servers  []*Server
	registry *Registry
	mu       sync.Mutex
}

func New(opts Options) *Host {
	if opts.ClientName == "" {
		opts.ClientName = "airline-mcp-chatbot"
	}
	if opts.ClientVersion == "" {
		opts.ClientVersion = "0.1.0"
	}
	if opts.ConnectTimeout == 0 {
		opts.ConnectTimeout = DefaultConnectTimeout
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = DefaultCallTimeout
	}
	if opts.MaxResultBytes == 0 {
		opts.MaxResultBytes = DefaultMaxResultBytes
	}
	if opts.ChildStderr == nil {
		opts.ChildStderr = os.Stderr
	}
	return &Host{opts: opts, registry: NewRegistry()}
}

// Connect dials every enabled server and lists its tools. A server that fails
// is recorded and skipped, never fatal: one broken server in someone else's
// config must not take down the session.
func (h *Host) Connect(ctx context.Context, cfg *config.File) {
	for _, name := range cfg.Names() {
		sc := cfg.Servers[name]
		s := &Server{Name: name, Config: sc}
		h.servers = append(h.servers, s)

		if sc.Disabled {
			continue
		}
		if err := h.connectOne(ctx, s); err != nil {
			s.Err = err
			continue
		}
	}
}

func (h *Host) connectOne(ctx context.Context, s *Server) error {
	transport, err := h.transportFor(s)
	if err != nil {
		return err
	}
	if h.opts.Wrap != nil {
		transport = h.opts.Wrap(s.Name, transport)
	}

	connectCtx, cancel := context.WithTimeout(ctx, h.opts.ConnectTimeout)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    h.opts.ClientName,
		Version: h.opts.ClientVersion,
	}, nil)

	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", annotateConnectError(s, err))
	}
	s.Session = session

	res, err := session.ListTools(connectCtx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, tool := range res.Tools {
		s.Tools = append(s.Tools, h.registry.Add(s.Name, session, tool))
	}
	return nil
}

// annotateConnectError points at the child's output when a stdio server dies
// during the handshake. The transport reports that as a bare EOF, which says
// nothing about why the process exited.
func annotateConnectError(s *Server, err error) error {
	if s.Config.ResolvedType() != config.TypeStdio {
		return err
	}
	if !strings.Contains(err.Error(), "EOF") {
		return err
	}
	return fmt.Errorf("%w (the server process exited during startup, look for [%s] lines above for what it printed)",
		err, s.Name)
}

func (h *Host) transportFor(s *Server) (mcp.Transport, error) {
	switch s.Config.ResolvedType() {
	case config.TypeHTTP:
		return &mcp.StreamableClientTransport{
			Endpoint:   s.Config.URL,
			HTTPClient: httpClientWithHeaders(s.Config.Headers),
		}, nil

	case config.TypeStdio:
		cmd := exec.Command(s.Config.Command, s.Config.Args...)
		cmd.Env = os.Environ()
		for k, v := range s.Config.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Stderr = prefixWriter{w: h.opts.ChildStderr, prefix: "[" + s.Name + "] "}
		s.cmd = cmd
		return &mcp.CommandTransport{Command: cmd}, nil

	default:
		return nil, fmt.Errorf("unsupported transport %q", s.Config.Type)
	}
}

func httpClientWithHeaders(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}
	return &http.Client{Transport: headerTransport{headers: headers, base: http.DefaultTransport}}
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}
	return t.base.RoundTrip(clone)
}

// prefixWriter tags subprocess stderr with the server it came from, so several
// stdio children sharing one terminal stay tellable apart.
type prefixWriter struct {
	w      io.Writer
	prefix string
}

func (p prefixWriter) Write(b []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line != "" {
			_, _ = io.WriteString(p.w, p.prefix+line+"\n")
		}
	}
	return len(b), nil
}

func (h *Host) Servers() []*Server { return h.servers }

func (h *Host) Registry() *Registry { return h.registry }

func (h *Host) Tools() []anthropic.ToolUnionParam { return h.registry.ToolParams() }

// ConnectedCount and ToolCount are what the startup summary reports.
func (h *Host) ConnectedCount() int {
	n := 0
	for _, s := range h.servers {
		if s.Connected() {
			n++
		}
	}
	return n
}

func (h *Host) ToolCount() int { return h.registry.Len() }

// Failures lists the servers that could not be reached, for the startup notice.
func (h *Host) Failures() []*Server {
	var out []*Server
	for _, s := range h.servers {
		if s.Err != nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (h *Host) Close() {
	for _, s := range h.servers {
		if s.Session != nil {
			_ = s.Session.Close()
		}
	}
}
