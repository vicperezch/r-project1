package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"r-project1/internal/host"
)

const declinedMessage = "The user declined this tool call. Do not retry it. " +
	"Explain what you were trying to do and ask how they want to proceed."

// runTools executes every tool the model asked for in one assistant turn.
//
// Calls run one at a time rather than concurrently, because the approval
// prompt reads from the terminal and interleaved prompts would be unusable.
// All results are returned together so they can go back in a single message.
func (r *REPL) runTools(ctx context.Context, msg *anthropic.Message) []anthropic.ContentBlockParamUnion {
	var results []anthropic.ContentBlockParamUnion
	for _, block := range msg.Content {
		use, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		results = append(results, r.runTool(ctx, use))
	}
	return results
}

func (r *REPL) runTool(ctx context.Context, use anthropic.ToolUseBlock) anthropic.ContentBlockParamUnion {
	// Tool input is parsed as JSON, never string matched: escaping varies
	// between models.
	args, err := host.ParseArgs(use.JSON.Input.Raw())
	if err != nil {
		fmt.Fprintf(r.out, "  [tool] %s: %v\n", use.Name, err)
		return anthropic.NewToolResultBlock(use.ID, err.Error(), true)
	}

	if r.host == nil {
		msg := fmt.Sprintf("no MCP servers are connected, so %s cannot be called", use.Name)
		fmt.Fprintf(r.out, "  [tool] %s\n", msg)
		return anthropic.NewToolResultBlock(use.ID, msg, true)
	}

	entry, known := r.host.Registry().Get(use.Name)
	if !known {
		msg := fmt.Sprintf("no tool named %q is available", use.Name)
		fmt.Fprintf(r.out, "  [tool] %s\n", msg)
		return anthropic.NewToolResultBlock(use.ID, msg, true)
	}

	fmt.Fprintf(r.out, "  [tool] %s %s\n", use.Name, summariseArgs(args))

	if !r.autoApprove && toolMayMutate(entry) {
		if !r.confirm() {
			fmt.Fprintln(r.out, "  [skip] declined")
			return anthropic.NewToolResultBlock(use.ID, declinedMessage, true)
		}
	}

	res, err := r.host.CallTool(ctx, use.Name, args)
	if err != nil {
		fmt.Fprintf(r.out, "  [fail] %v\n", err)
		return anthropic.NewToolResultBlock(use.ID, err.Error(), true)
	}

	status := "ok"
	if res.IsError {
		status = "tool error"
	}
	note := ""
	if res.Truncated {
		note = ", truncated"
	}
	fmt.Fprintf(r.out, "  [%s] %d bytes in %dms%s\n",
		status, len(res.Text), res.Duration.Milliseconds(), note)

	return anthropic.NewToolResultBlock(use.ID, res.Text, res.IsError)
}

// confirm asks the user to allow one call. It reads from the loop's own input
// channel: a second reader on stdin would steal lines from the REPL.
func (r *REPL) confirm() bool {
	fmt.Fprint(r.out, "  this changes data, allow it? [y/N] ")

	if r.input == nil {
		fmt.Fprintln(r.out)
		return false
	}
	line, ok := <-r.input
	if !ok {
		// Input ended mid prompt, which is not consent.
		fmt.Fprintln(r.out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

const argPreview = 120

func summariseArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprint(args)
	}
	s := string(encoded)
	if len(s) > argPreview {
		s = s[:argPreview-3] + "..."
	}
	return s
}
