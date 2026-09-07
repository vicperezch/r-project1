package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"r-project1/internal/llm"
)

// stubLLM records what the REPL sent it, so the tests can assert on the
// conversation history without calling the API.
type stubLLM struct {
	reply         string
	err           error
	calls         int
	historySizes  []int
	lastUserInput string
}

func (s *stubLLM) Model() string { return "stub-model" }

func (s *stubLLM) Stream(_ context.Context, conv *llm.Conversation, _ []anthropic.ToolUnionParam, out io.Writer) (*anthropic.Message, error) {
	s.calls++
	s.historySizes = append(s.historySizes, conv.Len())
	if s.err != nil {
		return nil, s.err
	}
	_, _ = io.WriteString(out, s.reply)
	return assistantMessage(s.reply), nil
}

func assistantMessage(text string) *anthropic.Message {
	raw := fmt.Sprintf(`{"id":"msg_stub","type":"message","role":"assistant","model":"stub",
		"content":[{"type":"text","text":%q}],"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":1}}`, text)
	var m anthropic.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		panic(err)
	}
	return &m
}

func run(t *testing.T, s *stubLLM, input string) (*REPL, string) {
	t.Helper()
	var out strings.Builder
	r := New(s, strings.NewReader(input), &out)
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	return r, out.String()
}

// This is the requirement 2 test: the second question only makes sense if the
// first exchange was resent with it.
func TestConversationContextIsResentOnEveryTurn(t *testing.T) {
	s := &stubLLM{reply: "Alan Turing was a British mathematician."}
	r, _ := run(t, s, "who was Alan Turing?\nwhen was he born?\n")

	if s.calls != 2 {
		t.Fatalf("model called %d times, want 2", s.calls)
	}
	// First request carries just the question. Second carries question,
	// answer, follow-up.
	if s.historySizes[0] != 1 {
		t.Errorf("first request sent %d messages, want 1", s.historySizes[0])
	}
	if s.historySizes[1] != 3 {
		t.Errorf("second request sent %d messages, want 3 so the pronoun resolves", s.historySizes[1])
	}
	if r.Conversation().Len() != 4 {
		t.Errorf("history ended at %d messages, want 4", r.Conversation().Len())
	}
}

func TestAssistantRepliesAreEchoed(t *testing.T) {
	s := &stubLLM{reply: "a British mathematician"}
	_, out := run(t, s, "who was Alan Turing?\n")
	if !strings.Contains(out, "a British mathematician") {
		t.Errorf("reply not written to output:\n%s", out)
	}
}

func TestResetClearsContext(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	r, out := run(t, s, "first question\n/reset\nsecond question\n")

	if !strings.Contains(out, "conversation cleared") {
		t.Errorf("reset not acknowledged:\n%s", out)
	}
	// After the reset the second request carries only the new question.
	if s.historySizes[1] != 1 {
		t.Errorf("request after reset sent %d messages, want 1", s.historySizes[1])
	}
	if r.Conversation().Len() != 2 {
		t.Errorf("history is %d, want 2", r.Conversation().Len())
	}
}

func TestFailedRequestDoesNotEndTheSessionOrStackTurns(t *testing.T) {
	s := &stubLLM{err: errors.New("rate limited")}
	r, out := run(t, s, "one\ntwo\n")

	if s.calls != 2 {
		t.Fatalf("session ended early, model called %d times", s.calls)
	}
	if !strings.Contains(out, "rate limited") {
		t.Errorf("error not reported to the user:\n%s", out)
	}
	// Both user turns were taken back, so nothing accumulated.
	if r.Conversation().Len() != 0 {
		t.Errorf("history is %d, want 0 after both requests failed", r.Conversation().Len())
	}
	if s.historySizes[1] != 1 {
		t.Errorf("second attempt sent %d messages, want 1, not a stacked duplicate", s.historySizes[1])
	}
}

func TestQuitStopsTheLoop(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	_, out := run(t, s, "/quit\nthis should never be asked\n")
	if s.calls != 0 {
		t.Errorf("model called %d times after /quit", s.calls)
	}
	if !strings.Contains(out, "bye") {
		t.Errorf("no goodbye:\n%s", out)
	}
}

func TestEOFExitsCleanly(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	_, out := run(t, s, "a question\n")
	if !strings.Contains(out, "bye") {
		t.Errorf("EOF should exit cleanly:\n%s", out)
	}
}

func TestUnknownCommandIsReportedNotSentToTheModel(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	_, out := run(t, s, "/nonsense\n")
	if s.calls != 0 {
		t.Error("a slash command must not reach the model")
	}
	if !strings.Contains(out, "unknown command") || !strings.Contains(out, "/help") {
		t.Errorf("unhelpful message:\n%s", out)
	}
}

func TestBlankLinesAreIgnored(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	_, _ = run(t, s, "\n   \n\n")
	if s.calls != 0 {
		t.Errorf("blank input reached the model %d times", s.calls)
	}
}

func TestHelpListsRegisteredCommands(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	_, out := run(t, s, "/help\n")
	for _, want := range []string{"/help", "/reset", "/history", "/quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %s:\n%s", want, out)
		}
	}
}

func TestRegisteredCommandsAreDispatched(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	var gotArgs string
	var out strings.Builder
	r := New(s, strings.NewReader("/servers foo bar\n"), &out)
	r.Register(&Command{Name: "servers", Help: "test", Run: func(_ context.Context, args string) (bool, error) {
		gotArgs = args
		return false, nil
	}})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotArgs != "foo bar" {
		t.Errorf("args %q, want %q", gotArgs, "foo bar")
	}
}

func TestHistoryCommandReportsTurnCount(t *testing.T) {
	s := &stubLLM{reply: "ok"}
	_, out := run(t, s, "/history\na question\n/history\n")
	if !strings.Contains(out, "no history yet") {
		t.Errorf("expected an empty history message:\n%s", out)
	}
	if !strings.Contains(out, "2 message(s) in history") {
		t.Errorf("expected a turn count:\n%s", out)
	}
}
