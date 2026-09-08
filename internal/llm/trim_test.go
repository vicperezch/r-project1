package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// A realistic shape: a question, an assistant turn asking for a tool, the tool
// result, then the final answer.
func toolExchange(question, toolID string) []anthropic.MessageParam {
	return []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
		anthropic.NewAssistantMessage(anthropic.NewToolUseBlock(toolID, map[string]any{"q": question}, "search")),
		anthropic.NewUserMessage(anthropic.NewToolResultBlock(toolID, "some result", false)),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("the answer")),
	}
}

func TestExchangeStartsIgnoresToolResultTurns(t *testing.T) {
	var msgs []anthropic.MessageParam
	msgs = append(msgs, toolExchange("first", "t1")...)
	msgs = append(msgs, toolExchange("second", "t2")...)

	starts := ExchangeStarts(msgs)
	// Only the two real questions start an exchange. The tool_result user turns
	// must not, or trimming could cut a tool_use away from its result.
	if len(starts) != 2 || starts[0] != 0 || starts[1] != 4 {
		t.Fatalf("starts = %v, want [0 4]", starts)
	}
}

func TestStartsExchange(t *testing.T) {
	if !startsExchange(anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))) {
		t.Error("a plain user message starts an exchange")
	}
	if startsExchange(anthropic.NewUserMessage(anthropic.NewToolResultBlock("t", "r", false))) {
		t.Error("a tool result continues an exchange, it does not start one")
	}
	if startsExchange(anthropic.NewAssistantMessage(anthropic.NewTextBlock("hi"))) {
		t.Error("an assistant message never starts an exchange")
	}
}

func TestDropFirst(t *testing.T) {
	c := NewConversation()
	for _, q := range []string{"a", "b", "c"} {
		c.AddUser(q)
	}
	if got := c.DropFirst(2); got != 2 || c.Len() != 1 {
		t.Fatalf("dropped %d leaving %d, want 2 and 1", got, c.Len())
	}
	if got := c.DropFirst(99); got != 1 || c.Len() != 0 {
		t.Fatalf("dropped %d leaving %d, want 1 and 0", got, c.Len())
	}
	if got := c.DropFirst(0); got != 0 {
		t.Errorf("dropping zero returned %d", got)
	}
	if got := c.DropFirst(-1); got != 0 {
		t.Errorf("dropping a negative returned %d", got)
	}
}

// fitClient builds a client with a tiny budget and a local token counter, so
// these tests neither need credentials nor make a network call.
func fitClient(limit int64) *Client {
	c := New(Config{Model: "claude-haiku-4-5", ContextLimit: limit})
	c.countTokens = func(_ context.Context, conv *Conversation, _ []anthropic.ToolUnionParam) (int64, error) {
		return estimateTokens(conv.Messages()), nil
	}
	return c
}

func TestFitLeavesASmallConversationAlone(t *testing.T) {
	c := fitClient(DefaultContextLimit)
	conv := NewConversation()
	conv.AddUser("hello")

	dropped, err := c.Fit(t.Context(), conv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 || conv.Len() != 1 {
		t.Errorf("dropped %d from a tiny conversation", dropped)
	}
}

func TestFitTrimsWholeExchangesOnly(t *testing.T) {
	conv := NewConversation()
	// Four exchanges, each carrying a bulky tool result.
	for _, q := range []string{"first", "second", "third", "fourth"} {
		for _, m := range toolExchange(q+strings.Repeat(" padding", 200), "t-"+q) {
			conv.Append(m)
		}
	}
	before := conv.Len()

	c := fitClient(400)
	dropped, err := c.Fit(t.Context(), conv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dropped == 0 {
		t.Fatal("an oversized conversation should have been trimmed")
	}
	if dropped%4 != 0 {
		t.Errorf("dropped %d messages, which is not a whole number of 4 message exchanges", dropped)
	}
	if conv.Len() != before-dropped {
		t.Errorf("length %d does not match %d minus %d", conv.Len(), before, dropped)
	}
	// Whatever survives must still begin with a real question.
	if !startsExchange(conv.Messages()[0]) {
		t.Error("trimming left the history starting mid exchange")
	}
}

func TestFitNeverOrphansAToolResult(t *testing.T) {
	conv := NewConversation()
	for i, q := range []string{"a", "b", "c", "d", "e", "f"} {
		_ = i
		for _, m := range toolExchange(q+strings.Repeat(" filler", 300), "id-"+q) {
			conv.Append(m)
		}
	}
	c := fitClient(300)
	if _, err := c.Fit(t.Context(), conv, nil); err != nil {
		t.Fatal(err)
	}

	// Walk what is left: a tool_result may only appear after a tool_use.
	seenToolUse := 0
	seenToolResult := 0
	for _, m := range conv.Messages() {
		for _, b := range m.Content {
			if b.OfToolUse != nil {
				seenToolUse++
			}
			if b.OfToolResult != nil {
				seenToolResult++
				if seenToolResult > seenToolUse {
					t.Fatal("a tool_result survived without the tool_use it answers")
				}
			}
		}
	}
}

func TestFitKeepsTheLastExchangeEvenIfItIsTooBig(t *testing.T) {
	conv := NewConversation()
	conv.AddUser(strings.Repeat("enormous ", 5000))

	c := fitClient(10)
	dropped, err := c.Fit(t.Context(), conv, nil)
	if err != nil {
		t.Fatal(err)
	}
	// There is only one exchange and it is the question being asked, so
	// dropping it would lose the request entirely.
	if dropped != 0 || conv.Len() != 1 {
		t.Errorf("dropped %d, the only exchange must be kept", dropped)
	}
}

func TestEstimateTokensGrowsWithContent(t *testing.T) {
	small := estimateTokens([]anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))})
	large := estimateTokens([]anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(strings.Repeat("hi ", 1000)))})
	if !(large > small && small >= 0) {
		t.Errorf("estimates %d and %d are not ordered", small, large)
	}
	if estimateTokens(nil) != 0 {
		t.Error("an empty history should estimate zero")
	}
}

func TestFitFallsBackToTheEstimateWhenCountingFails(t *testing.T) {
	conv := NewConversation()
	for _, q := range []string{"a", "b", "c", "d"} {
		for _, m := range toolExchange(q+strings.Repeat(" padding", 200), "t-"+q) {
			conv.Append(m)
		}
	}

	c := New(Config{Model: "claude-haiku-4-5", ContextLimit: 400})
	c.countTokens = func(context.Context, *Conversation, []anthropic.ToolUnionParam) (int64, error) {
		return 0, errors.New("network is down")
	}

	// A failure to measure must not fail the user's turn: the local estimate
	// is used instead and trimming still happens.
	dropped, err := c.Fit(context.Background(), conv, nil)
	if err != nil {
		t.Fatalf("Fit should not surface a counting failure: %v", err)
	}
	if dropped == 0 {
		t.Error("trimming should still have happened using the estimate")
	}
}

func TestFitSkipsCountingWhenTheHistoryIsSmall(t *testing.T) {
	conv := NewConversation()
	conv.AddUser("short question")

	called := false
	c := New(Config{Model: "claude-haiku-4-5", ContextLimit: DefaultContextLimit})
	c.countTokens = func(context.Context, *Conversation, []anthropic.ToolUnionParam) (int64, error) {
		called = true
		return 0, nil
	}
	if _, err := c.Fit(context.Background(), conv, nil); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("a small history should cost no API round trip")
	}
}
