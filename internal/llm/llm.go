// Package llm wraps the Anthropic Messages API and owns the conversation
// history that gives the chatbot its context across turns.
package llm

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	DefaultModel     = "claude-haiku-4-5"
	DefaultMaxTokens = 4096
)

type Config struct {
	Model     string
	MaxTokens int64
	System    string
	// ContextLimit is the token budget for resent history. Zero means
	// DefaultContextLimit.
	ContextLimit int64
}

type Client struct {
	api          anthropic.Client
	model        string
	maxTokens    int64
	system       []anthropic.TextBlockParam
	contextLimit int64

	// countTokens is a field so tests can measure without a network call.
	countTokens func(context.Context, *Conversation, []anthropic.ToolUnionParam) (int64, error)
}

func New(cfg Config) *Client {
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.ContextLimit <= 0 {
		cfg.ContextLimit = DefaultContextLimit
	}
	c := &Client{
		api:          anthropic.NewClient(),
		model:        cfg.Model,
		maxTokens:    cfg.MaxTokens,
		contextLimit: cfg.ContextLimit,
	}
	if cfg.System != "" {
		c.system = []anthropic.TextBlockParam{{Text: cfg.System}}
	}
	c.countTokens = c.apiCountTokens
	return c
}

func (c *Client) Model() string { return c.model }

func (c *Client) ContextLimit() int64 { return c.contextLimit }

// Stream sends the whole conversation and writes assistant text to out as it
// arrives. The accumulated message is returned so the caller can append it to
// history and, later, inspect it for tool use.
func (c *Client) Stream(ctx context.Context, conv *Conversation, tools []anthropic.ToolUnionParam, out io.Writer) (*anthropic.Message, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: c.maxTokens,
		Messages:  conv.Messages(),
	}
	if len(c.system) > 0 {
		params.System = c.system
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	// Haiku 4.5 rejects output_config.effort and has no adaptive thinking, so
	// neither is sent unless the configured model is known to support it.
	if SupportsAdaptiveThinking(c.model) {
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
	}

	stream := c.api.Messages.NewStreaming(ctx, params)
	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return nil, fmt.Errorf("accumulate stream event: %w", err)
		}
		delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && out != nil {
			_, _ = io.WriteString(out, text.Text)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return &msg, nil
}

// SupportsAdaptiveThinking reports whether a model accepts adaptive thinking.
// Everything the project defaults to is older than 4.6, where sending it is a
// 400, so this stays a deliberate allow list rather than a guess.
func SupportsAdaptiveThinking(model string) bool {
	return strings.HasPrefix(model, "claude-opus-5") ||
		strings.HasPrefix(model, "claude-sonnet-5") ||
		strings.HasPrefix(model, "claude-fable-5")
}
