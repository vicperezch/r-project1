package host

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/config"
	"r-project1/internal/mcplog"
)

type echoIn struct {
	Text  string `json:"text" jsonschema:"the text to echo back"`
	Times int    `json:"times,omitempty" jsonschema:"how many times to repeat it"`
}

type echoOut struct {
	Echoed string `json:"echoed"`
}

// stubMCPServer stands up a real MCP server over HTTP, so these tests exercise
// the transport, the handshake, tools/list and tools/call, not a mock.
func stubMCPServer(t *testing.T) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "stub", Version: "1.0.0"}, nil)

	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "Echo text back"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoOut, error) {
			n := in.Times
			if n < 1 {
				n = 1
			}
			s := strings.Repeat(in.Text, n)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: s}},
			}, echoOut{Echoed: s}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "boom", Description: "Always fails"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, errors.New("this tool always fails")
		})

	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)
	return ts.URL
}

func connectedHost(t *testing.T, servers map[string]config.Server, opts Options) *Host {
	t.Helper()
	if opts.ChildStderr == nil {
		opts.ChildStderr = io.Discard
	}
	h := New(opts)
	h.Connect(context.Background(), &config.File{Servers: servers})
	t.Cleanup(h.Close)
	return h
}

func TestConnectRegistersNamespacedTools(t *testing.T) {
	url := stubMCPServer(t)
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: url},
		"beta":  {Type: config.TypeHTTP, URL: url},
	}, Options{})

	if h.ConnectedCount() != 2 {
		t.Fatalf("connected to %d servers, want 2 (failures: %v)", h.ConnectedCount(), h.Failures())
	}
	if h.ToolCount() != 4 {
		t.Fatalf("registered %d tools, want 4", h.ToolCount())
	}
	// The same tool name from two servers must stay distinguishable.
	for _, want := range []string{"alpha__echo", "alpha__boom", "beta__echo", "beta__boom"} {
		if _, ok := h.Registry().Get(want); !ok {
			t.Errorf("tool %q not registered", want)
		}
	}
	if n := len(h.Tools()); n != 4 {
		t.Errorf("built %d Anthropic tool params, want 4", n)
	}
}

func TestCallToolReturnsTheServersText(t *testing.T) {
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}, Options{})

	res, err := h.CallTool(context.Background(), "alpha__echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Text)
	}
	if !strings.Contains(res.Text, "hello") {
		t.Errorf("text = %q", res.Text)
	}
	if res.ServerName != "alpha" || res.ToolName != "echo" {
		t.Errorf("result attributed to %s/%s, want alpha/echo", res.ServerName, res.ToolName)
	}
	if res.Duration <= 0 {
		t.Error("duration was not recorded")
	}
}

func TestFailingToolIsAnErrorResultNotAFailedCall(t *testing.T) {
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}, Options{})

	res, err := h.CallTool(context.Background(), "alpha__boom", map[string]any{})
	if err != nil {
		t.Fatalf("a tool that fails must not fail the dispatch: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError so the model can correct itself")
	}
	if !strings.Contains(res.Text, "always fails") {
		t.Errorf("error text lost: %q", res.Text)
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}, Options{})

	if _, err := h.CallTool(context.Background(), "alpha__nope", nil); err == nil {
		t.Fatal("expected an error for an unregistered tool")
	}
}

func TestLargeResultsAreTruncated(t *testing.T) {
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}, Options{MaxResultBytes: 64})

	res, err := h.CallTool(context.Background(), "alpha__echo",
		map[string]any{"text": strings.Repeat("x", 40), "times": 20})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatalf("an 800 byte result should be truncated, got %d bytes", len(res.Text))
	}
	if !strings.Contains(res.Text, "truncated") {
		t.Errorf("no truncation notice: %q", res.Text)
	}
}

func TestABrokenServerDoesNotStopTheOthers(t *testing.T) {
	h := connectedHost(t, map[string]config.Server{
		"good":           {Type: config.TypeHTTP, URL: stubMCPServer(t)},
		"broken":         {Type: config.TypeHTTP, URL: "http://127.0.0.1:1/mcp"},
		"missing-binary": {Type: config.TypeStdio, Command: "definitely-not-a-real-binary-xyz"},
	}, Options{ConnectTimeout: 3 * time.Second})

	if h.ConnectedCount() != 1 {
		t.Fatalf("connected %d, want only the good server", h.ConnectedCount())
	}
	if h.ToolCount() != 2 {
		t.Errorf("tools %d, want the 2 from the good server", h.ToolCount())
	}
	failures := h.Failures()
	if len(failures) != 2 {
		t.Fatalf("recorded %d failures, want 2", len(failures))
	}
	for _, f := range failures {
		if f.Err == nil || f.Status() != "failed" {
			t.Errorf("%s: status %q err %v", f.Name, f.Status(), f.Err)
		}
	}
	// The working server is still fully usable.
	if _, err := h.CallTool(context.Background(), "good__echo", map[string]any{"text": "ok"}); err != nil {
		t.Errorf("good server should still work: %v", err)
	}
}

func TestDisabledServersAreSkipped(t *testing.T) {
	h := connectedHost(t, map[string]config.Server{
		"off": {Type: config.TypeHTTP, URL: stubMCPServer(t), Disabled: true},
	}, Options{})

	if h.ConnectedCount() != 0 || h.ToolCount() != 0 {
		t.Fatalf("disabled server was connected: %d servers, %d tools", h.ConnectedCount(), h.ToolCount())
	}
	if s := h.Servers()[0]; s.Status() != "disabled" {
		t.Errorf("status %q, want disabled", s.Status())
	}
}

func TestToolCallsAreLoggedAsAPairedRequestAndResponse(t *testing.T) {
	log := mcplog.Discarding()
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}, Options{Log: log})

	if _, err := h.CallTool(context.Background(), "alpha__echo", map[string]any{"text": "hi"}); err != nil {
		t.Fatal(err)
	}

	var calls []mcplog.Entry
	for _, e := range log.Recent(0) {
		if e.Level == mcplog.LevelCall {
			calls = append(calls, e)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("logged %d call entries, want a request and a response", len(calls))
	}
	req, resp := calls[0], calls[1]
	if req.Direction != mcplog.DirectionRequest || resp.Direction != mcplog.DirectionResponse {
		t.Errorf("directions %q and %q", req.Direction, resp.Direction)
	}
	if req.Seq == 0 || req.Seq != resp.Seq {
		t.Errorf("seqs %d and %d must match to pair up", req.Seq, resp.Seq)
	}
	if req.Server != "alpha" || req.Tool != "echo" {
		t.Errorf("request attributed to %s/%s", req.Server, req.Tool)
	}
	if resp.Result == nil {
		t.Error("response entry carries no result")
	}
}

func TestFailingToolCallIsStillLogged(t *testing.T) {
	log := mcplog.Discarding()
	h := connectedHost(t, map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}, Options{Log: log})

	if _, err := h.CallTool(context.Background(), "alpha__boom", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	entries := log.Recent(0)
	last := entries[len(entries)-1]
	if !last.IsError || last.Direction != mcplog.DirectionResponse {
		t.Errorf("tool failure not recorded as an error response: %+v", last)
	}
}

func TestProtocolFramesAreCapturedThroughWrap(t *testing.T) {
	log := mcplog.Discarding()
	h := New(Options{
		ChildStderr: io.Discard,
		Log:         log,
		Wrap: func(name string, tr mcp.Transport) mcp.Transport {
			return &mcp.LoggingTransport{Transport: tr, Writer: log.ProtocolWriter(name)}
		},
	})
	h.Connect(context.Background(), &config.File{Servers: map[string]config.Server{
		"alpha": {Type: config.TypeHTTP, URL: stubMCPServer(t)},
	}})
	t.Cleanup(h.Close)

	methods := map[string]bool{}
	for _, e := range log.Recent(0) {
		if e.Level == mcplog.LevelProtocol && e.Method != "" {
			methods[e.Method] = true
		}
	}
	// Requirement 3 is about all traffic, not only tool calls: the handshake
	// and discovery must show up too.
	for _, want := range []string{"initialize", "tools/list"} {
		if !methods[want] {
			t.Errorf("protocol log is missing %q, captured %v", want, methods)
		}
	}
}
