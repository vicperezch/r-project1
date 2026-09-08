package llm

import "github.com/anthropics/anthropic-sdk-go"

// Conversation is the message history. Every turn is resent on every request,
// which is what lets "when was he born" resolve against an earlier "who was
// Alan Turing".
type Conversation struct {
	messages []anthropic.MessageParam
}

func NewConversation() *Conversation {
	return &Conversation{}
}

func (c *Conversation) AddUser(text string) {
	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))
}

// AddUserBlocks appends a user turn built from content blocks, which is how
// tool results are returned to the model.
func (c *Conversation) AddUserBlocks(blocks ...anthropic.ContentBlockParamUnion) {
	if len(blocks) == 0 {
		return
	}
	c.messages = append(c.messages, anthropic.NewUserMessage(blocks...))
}

func (c *Conversation) Append(m anthropic.MessageParam) {
	c.messages = append(c.messages, m)
}

// RemoveLast drops the most recent message. The REPL uses it to take back a
// user turn whose request failed, so a retry does not stack duplicate turns.
func (c *Conversation) RemoveLast() {
	if len(c.messages) > 0 {
		c.messages = c.messages[:len(c.messages)-1]
	}
}

// Messages returns the history. The slice is shared with the SDK call, which
// only reads it.
func (c *Conversation) Messages() []anthropic.MessageParam {
	return c.messages
}

// DropFirst removes the oldest n messages and reports how many went.
func (c *Conversation) DropFirst(n int) int {
	if n <= 0 {
		return 0
	}
	if n >= len(c.messages) {
		n = len(c.messages)
	}
	c.messages = c.messages[n:]
	return n
}

func (c *Conversation) Len() int { return len(c.messages) }

// EstimatedTokens is a cheap local approximation, used for display.
func (c *Conversation) EstimatedTokens() int64 { return estimateTokens(c.messages) }

func (c *Conversation) Reset() { c.messages = nil }
