package llm

import "testing"

func TestConversationGrowsWithEveryTurn(t *testing.T) {
	c := NewConversation()
	if c.Len() != 0 {
		t.Fatalf("new conversation has %d messages", c.Len())
	}
	c.AddUser("who was Alan Turing?")
	if c.Len() != 1 {
		t.Fatalf("len %d, want 1", c.Len())
	}
	c.AddUser("when was he born?")
	if c.Len() != 2 {
		t.Fatalf("len %d, want 2", c.Len())
	}
}

func TestRemoveLastTakesBackAFailedTurn(t *testing.T) {
	c := NewConversation()
	c.AddUser("first")
	c.AddUser("second")
	c.RemoveLast()
	if c.Len() != 1 {
		t.Fatalf("len %d, want 1", c.Len())
	}
	c.RemoveLast()
	c.RemoveLast() // must not panic on an empty history
	if c.Len() != 0 {
		t.Fatalf("len %d, want 0", c.Len())
	}
}

func TestResetClearsEverything(t *testing.T) {
	c := NewConversation()
	c.AddUser("a")
	c.AddUser("b")
	c.Reset()
	if c.Len() != 0 || len(c.Messages()) != 0 {
		t.Fatalf("reset left %d messages", c.Len())
	}
}

func TestAddUserBlocksIgnoresAnEmptyCall(t *testing.T) {
	c := NewConversation()
	c.AddUserBlocks()
	if c.Len() != 0 {
		t.Fatal("an empty block list must not create a turn")
	}
}

func TestSupportsAdaptiveThinking(t *testing.T) {
	// The default model predates adaptive thinking and 400s if it is sent.
	if SupportsAdaptiveThinking(DefaultModel) {
		t.Errorf("%s must not be sent adaptive thinking", DefaultModel)
	}
	if SupportsAdaptiveThinking("claude-haiku-4-5") || SupportsAdaptiveThinking("claude-sonnet-4-6") {
		t.Error("pre 5 models must not be sent adaptive thinking")
	}
	for _, m := range []string{"claude-opus-5", "claude-sonnet-5", "claude-fable-5"} {
		if !SupportsAdaptiveThinking(m) {
			t.Errorf("%s should support adaptive thinking", m)
		}
	}
}
