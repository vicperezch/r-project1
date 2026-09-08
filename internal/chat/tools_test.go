package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/config"
	"r-project1/internal/host"
	"r-project1/internal/llm"
)

type echoIn struct {
	Text string `json:"text" jsonschema:"text to echo"`
}

// toolHost connects a real host to a real MCP server over HTTP, so the loop is
// exercised end to end rather than against a mock.
func toolHost(t *testing.T) *host.Host {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "stub", Version: "1"}, nil)

	mcp.AddTool(srv, &mcp.Tool{Name: "read_thing", Description: "Read only"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoIn, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: "read: " + in.Text}}}, in, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "write_thing", Description: "Changes data"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoIn, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: "wrote: " + in.Text}}}, in, nil
		})

	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)

	h := host.New(host.Options{ChildStderr: io.Discard})
	h.Connect(context.Background(), &config.File{Servers: map[string]config.Server{
		"stub": {Type: config.TypeHTTP, URL: ts.URL},
	}})
	t.Cleanup(h.Close)
	if h.ConnectedCount() != 1 {
		t.Fatalf("stub server did not connect: %v", h.Failures())
	}
	return h
}

// scriptedLLM replays a fixed sequence of assistant messages and records the
// history it was sent each time.
type scriptedLLM struct {
	replies   []*anthropic.Message
	calls     int
	histories [][]anthropic.MessageParam
	toolsSeen int
}

func (s *scriptedLLM) Model() string { return "stub-model" }

func (s *scriptedLLM) ContextLimit() int64 { return llm.DefaultContextLimit }

func (s *scriptedLLM) Fit(context.Context, *llm.Conversation, []anthropic.ToolUnionParam) (int, error) {
	return 0, nil
}

func (s *scriptedLLM) Stream(_ context.Context, conv *llm.Conversation, tools []anthropic.ToolUnionParam, out io.Writer) (*anthropic.Message, error) {
	snapshot := make([]anthropic.MessageParam, len(conv.Messages()))
	copy(snapshot, conv.Messages())
	s.histories = append(s.histories, snapshot)
	s.toolsSeen = len(tools)

	i := s.calls
	s.calls++
	if i < len(s.replies) {
		return s.replies[i], nil
	}
	_, _ = io.WriteString(out, "all done")
	return assistantMessage("all done"), nil
}

func toolUseMessage(toolName, input string) *anthropic.Message {
	raw := fmt.Sprintf(`{"id":"msg_tool","type":"message","role":"assistant","model":"stub",
		"content":[{"type":"tool_use","id":"toolu_1","name":%q,"input":%s}],
		"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, toolName, input)
	var m anthropic.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		panic(err)
	}
	return &m
}

func runWithHost(t *testing.T, s *scriptedLLM, h *host.Host, input string, autoApprove bool) (*REPL, string) {
	t.Helper()
	var out strings.Builder
	r := New(s, strings.NewReader(input), &out)
	r.SetAutoApprove(autoApprove)
	if h != nil {
		r.AttachHost(h)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	return r, out.String()
}

func historyJSON(t *testing.T, r *REPL) string {
	t.Helper()
	raw, err := json.Marshal(r.Conversation().Messages())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestToolCallRunsAndItsResultGoesBackToTheModel(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__read_thing", `{"text":"hello"}`),
	}}
	r, out := runWithHost(t, s, toolHost(t), "read the thing\n", false)

	if s.calls != 2 {
		t.Fatalf("model called %d times, want 2: one asking for the tool, one after", s.calls)
	}
	if s.toolsSeen == 0 {
		t.Error("tools were not advertised to the model")
	}
	if !strings.Contains(out, "[tool] stub__read_thing") {
		t.Errorf("the call was not shown to the user:\n%s", out)
	}
	if !strings.Contains(out, "[ok]") {
		t.Errorf("the result was not shown:\n%s", out)
	}

	// The second request must carry the tool_use turn and the tool_result.
	hist := historyJSON(t, r)
	if !strings.Contains(hist, "tool_use") || !strings.Contains(hist, "tool_result") {
		t.Errorf("history is missing the tool round trip: %s", hist)
	}
	if !strings.Contains(hist, "read: hello") {
		t.Errorf("the tool output never reached the history: %s", hist)
	}
	if len(s.histories[1]) != 3 {
		t.Errorf("second request sent %d messages, want user, assistant, tool_result", len(s.histories[1]))
	}
}

func TestReadOnlyToolRunsWithoutAPrompt(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__read_thing", `{"text":"x"}`),
	}}
	_, out := runWithHost(t, s, toolHost(t), "go\n", false)
	if strings.Contains(out, "allow it?") {
		t.Errorf("a read only tool should not prompt:\n%s", out)
	}
}

func TestMutatingToolIsRefusedWhenTheUserDeclines(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__write_thing", `{"text":"danger"}`),
	}}
	// "n" answers the approval prompt.
	r, out := runWithHost(t, s, toolHost(t), "write it\nn\n", false)

	if !strings.Contains(out, "allow it?") {
		t.Fatalf("no approval prompt:\n%s", out)
	}
	if !strings.Contains(out, "[skip] declined") {
		t.Errorf("decline not reported:\n%s", out)
	}
	hist := historyJSON(t, r)
	if strings.Contains(hist, "wrote: danger") {
		t.Error("the tool ran despite being declined")
	}
	if !strings.Contains(hist, "declined this tool call") {
		t.Errorf("the model was not told it was declined: %s", hist)
	}
}

func TestMutatingToolRunsWhenTheUserApproves(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__write_thing", `{"text":"ok"}`),
	}}
	r, out := runWithHost(t, s, toolHost(t), "write it\ny\n", false)

	if !strings.Contains(out, "allow it?") {
		t.Fatalf("no approval prompt:\n%s", out)
	}
	if !strings.Contains(historyJSON(t, r), "wrote: ok") {
		t.Error("the approved tool did not run")
	}
}

func TestAutoApproveSkipsThePrompt(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__write_thing", `{"text":"ok"}`),
	}}
	r, out := runWithHost(t, s, toolHost(t), "write it\n", true)

	if strings.Contains(out, "allow it?") {
		t.Errorf("--yes should skip the prompt:\n%s", out)
	}
	if !strings.Contains(historyJSON(t, r), "wrote: ok") {
		t.Error("the tool did not run under auto approve")
	}
}

func TestUnknownToolBecomesAnErrorResultNotACrash(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__does_not_exist", `{}`),
	}}
	r, out := runWithHost(t, s, toolHost(t), "go\n", true)

	if s.calls != 2 {
		t.Fatalf("the turn should continue after an unknown tool, calls %d", s.calls)
	}
	if !strings.Contains(out, "no tool named") {
		t.Errorf("unknown tool not reported:\n%s", out)
	}
	if !strings.Contains(historyJSON(t, r), "tool_result") {
		t.Error("an unknown tool must still produce a tool_result, or the next request is malformed")
	}
}

func TestToolCallsWithNoHostAreReportedNotDropped(t *testing.T) {
	s := &scriptedLLM{replies: []*anthropic.Message{
		toolUseMessage("stub__read_thing", `{"text":"x"}`),
	}}
	r, _ := runWithHost(t, s, nil, "go\n", true)
	if !strings.Contains(historyJSON(t, r), "tool_result") {
		t.Error("a tool_use with no host must still be answered with a tool_result")
	}
}

func TestLoopStopsAfterTheRoundCap(t *testing.T) {
	// A model that asks for the same tool forever.
	var replies []*anthropic.Message
	for i := 0; i < 40; i++ {
		replies = append(replies, toolUseMessage("stub__read_thing", `{"text":"again"}`))
	}
	s := &scriptedLLM{replies: replies}
	_, out := runWithHost(t, s, toolHost(t), "go\n", true)

	if s.calls > DefaultMaxToolRounds {
		t.Errorf("model called %d times, cap is %d", s.calls, DefaultMaxToolRounds)
	}
	if !strings.Contains(out, "stopped after") {
		t.Errorf("the cap was not reported to the user:\n%s", out)
	}
}

func TestHistoryStaysValidAfterTheRoundCap(t *testing.T) {
	var replies []*anthropic.Message
	for i := 0; i < 40; i++ {
		replies = append(replies, toolUseMessage("stub__read_thing", `{"text":"again"}`))
	}
	s := &scriptedLLM{replies: replies}
	r, _ := runWithHost(t, s, toolHost(t), "go\n", true)

	// Every tool_use must be answered, otherwise the next request 400s.
	hist := historyJSON(t, r)
	if strings.Count(hist, `"type":"tool_use"`) != strings.Count(hist, `"type":"tool_result"`) {
		t.Errorf("unbalanced tool_use and tool_result blocks:\n%s", hist)
	}
}

func TestSummariseArgsClipsLongInput(t *testing.T) {
	s := summariseArgs(map[string]any{"blob": strings.Repeat("x", 500)})
	if len(s) > argPreview {
		t.Errorf("args preview is %d chars, want at most %d", len(s), argPreview)
	}
	if summariseArgs(nil) != "{}" {
		t.Error("empty args should render as {}")
	}
}
