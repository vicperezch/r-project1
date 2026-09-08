package llm

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	// DefaultContextLimit is the token budget for the history that gets resent.
	// Haiku 4.5 has a 200K window, not 1M, so this leaves room for tool
	// definitions and the reply itself.
	DefaultContextLimit = 150_000

	// bytesPerToken is a rule of thumb used only to decide whether asking the
	// API for an exact count is worth a round trip.
	bytesPerToken = 4
)

// startsExchange reports whether a message begins a new user turn. A user
// message carrying tool results continues the previous exchange rather than
// starting one, which is what keeps a tool_use block with its tool_result.
func startsExchange(m anthropic.MessageParam) bool {
	if m.Role != anthropic.MessageParamRoleUser {
		return false
	}
	for _, b := range m.Content {
		if b.OfToolResult != nil {
			return false
		}
	}
	return true
}

// ExchangeStarts returns the index of every message that begins an exchange.
// Trimming only ever cuts at these points, so a tool_use is never separated
// from the tool_result that answers it.
func ExchangeStarts(msgs []anthropic.MessageParam) []int {
	var out []int
	for i, m := range msgs {
		if startsExchange(m) {
			out = append(out, i)
		}
	}
	return out
}

// estimateTokens is a cheap local guess. It exists so the common case costs no
// API call at all.
func estimateTokens(msgs []anthropic.MessageParam) int64 {
	if len(msgs) == 0 {
		return 0
	}
	encoded, err := json.Marshal(msgs)
	if err != nil {
		return 0
	}
	return int64(len(encoded) / bytesPerToken)
}

// CountTokens reports exactly how large the next request would be.
func (c *Client) CountTokens(ctx context.Context, conv *Conversation, tools []anthropic.ToolUnionParam) (int64, error) {
	return c.countTokens(ctx, conv, tools)
}

func (c *Client) apiCountTokens(ctx context.Context, conv *Conversation, tools []anthropic.ToolUnionParam) (int64, error) {
	params := anthropic.MessageCountTokensParams{
		Model:    anthropic.Model(c.model),
		Messages: conv.Messages(),
	}
	if len(c.system) > 0 {
		params.System = anthropic.MessageCountTokensParamsSystemUnion{OfTextBlockArray: c.system}
	}
	for _, t := range tools {
		params.Tools = append(params.Tools, anthropic.MessageCountTokensToolUnionParam{OfTool: t.OfTool})
	}

	res, err := c.api.Messages.CountTokens(ctx, params)
	if err != nil {
		return 0, err
	}
	return res.InputTokens, nil
}

// Fit trims the oldest exchanges until the conversation fits the context
// budget, and reports how many messages it dropped.
//
// The exact count costs an API round trip, so it is only requested once the
// cheap local estimate says the history is getting large. If that request
// fails, the estimate is used rather than failing the user's turn.
func (c *Client) Fit(ctx context.Context, conv *Conversation, tools []anthropic.ToolUnionParam) (int, error) {
	msgs := conv.Messages()
	if len(msgs) == 0 {
		return 0, nil
	}

	estimate := estimateTokens(msgs)
	if estimate < c.contextLimit/2 {
		return 0, nil
	}

	exact, err := c.CountTokens(ctx, conv, tools)
	if err != nil {
		exact = estimate
	}
	if exact <= c.contextLimit {
		return 0, nil
	}

	// Calibrate the local estimator against the exact total, so per exchange
	// numbers are in the same units without asking the API again per exchange.
	scale := 1.0
	if estimate > 0 {
		scale = float64(exact) / float64(estimate)
	}
	target := c.contextLimit * 8 / 10

	starts := ExchangeStarts(msgs)
	if len(starts) <= 1 {
		// One exchange is all there is. Dropping it would lose the question
		// being asked, so it is left alone and the API decides.
		return 0, nil
	}

	for k := 1; k < len(starts); k++ {
		remaining := int64(float64(estimateTokens(msgs[starts[k]:])) * scale)
		if remaining <= target {
			return conv.DropFirst(starts[k]), nil
		}
	}
	// Even the newest exchange alone is over budget: keep just that one.
	return conv.DropFirst(starts[len(starts)-1]), nil
}
