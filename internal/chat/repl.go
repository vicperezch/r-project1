// Package chat is the terminal REPL: reading input, dispatching slash
// commands, and driving the model.
package chat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"r-project1/internal/host"
	"r-project1/internal/llm"
	"r-project1/internal/mcplog"
)

// Responder is what the REPL needs from the LLM client. Keeping it an
// interface lets the loop be tested without touching the API.
type Responder interface {
	Stream(ctx context.Context, conv *llm.Conversation, tools []anthropic.ToolUnionParam, out io.Writer) (*anthropic.Message, error)
	Model() string
}

// Command is a slash command. Later steps register their own.
type Command struct {
	Name string
	Help string
	Run  func(ctx context.Context, args string) (stop bool, err error)
}

type REPL struct {
	llm      Responder
	conv     *llm.Conversation
	in       io.Reader
	out      io.Writer
	commands map[string]*Command
	host     *host.Host
	log      *mcplog.Logger
}

func New(responder Responder, in io.Reader, out io.Writer) *REPL {
	r := &REPL{
		llm:      responder,
		conv:     llm.NewConversation(),
		in:       in,
		out:      out,
		commands: map[string]*Command{},
	}
	r.registerBuiltins()
	return r
}

func (r *REPL) Register(c *Command) { r.commands[c.Name] = c }

func (r *REPL) Conversation() *llm.Conversation { return r.conv }

func (r *REPL) registerBuiltins() {
	r.Register(&Command{Name: "help", Help: "list the available commands", Run: r.cmdHelp})
	r.Register(&Command{Name: "reset", Help: "forget the conversation so far", Run: r.cmdReset})
	r.Register(&Command{Name: "history", Help: "show how many turns are being resent", Run: r.cmdHistory})
	r.Register(&Command{Name: "quit", Help: "leave the chat", Run: r.cmdQuit})
	r.Register(&Command{Name: "exit", Help: "leave the chat", Run: r.cmdQuit})
}

// lines pumps stdin through a channel so the loop can also watch for context
// cancellation. A blocking read on stdin would otherwise ignore Ctrl+C.
func lines(r io.Reader) (<-chan string, <-chan error) {
	out := make(chan string)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			out <- sc.Text()
		}
		if err := sc.Err(); err != nil {
			errc <- err
		}
	}()
	return out, errc
}

func (r *REPL) Run(ctx context.Context) error {
	r.banner()
	input, errc := lines(r.in)

	for {
		fmt.Fprint(r.out, "\nyou> ")

		var line string
		select {
		case <-ctx.Done():
			fmt.Fprintln(r.out)
			return nil
		case err := <-errc:
			return err
		case l, ok := <-input:
			if !ok {
				fmt.Fprintln(r.out, "\nbye")
				return nil
			}
			line = strings.TrimSpace(l)
		}

		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			stop, err := r.dispatch(ctx, line)
			if err != nil {
				fmt.Fprintf(r.out, "error: %v\n", err)
			}
			if stop {
				return nil
			}
			continue
		}
		// An API failure should not end the session, so it is reported and the
		// loop carries on.
		if err := r.ask(ctx, line); err != nil {
			fmt.Fprintf(r.out, "\nerror: %v\n", err)
		}
	}
}

func (r *REPL) dispatch(ctx context.Context, line string) (bool, error) {
	name, args, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	cmd, ok := r.commands[strings.ToLower(name)]
	if !ok {
		return false, fmt.Errorf("unknown command %q, try /help", name)
	}
	return cmd.Run(ctx, strings.TrimSpace(args))
}

func (r *REPL) ask(ctx context.Context, input string) error {
	r.conv.AddUser(input)

	fmt.Fprint(r.out, "\nclaude> ")
	msg, err := r.llm.Stream(ctx, r.conv, r.Tools(), r.out)
	if err != nil {
		// Take the user turn back so a retry does not stack duplicates.
		r.conv.RemoveLast()
		return err
	}
	fmt.Fprintln(r.out)

	r.conv.Append(msg.ToParam())
	return r.afterMessage(ctx, msg)
}

// Tools and afterMessage are the seams the tool-use loop plugs into later.
// Until then the chatbot is a plain conversational client.
func (r *REPL) Tools() []anthropic.ToolUnionParam { return nil }

func (r *REPL) afterMessage(context.Context, *anthropic.Message) error { return nil }

func (r *REPL) banner() {
	fmt.Fprintf(r.out, "airline mcp chatbot, model %s\n", r.llm.Model())
	fmt.Fprintln(r.out, "type /help for commands, /quit or ctrl-d to leave")
}

func (r *REPL) cmdHelp(context.Context, string) (bool, error) {
	names := make([]string, 0, len(r.commands))
	for n := range r.commands {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(r.out, "commands:")
	for _, n := range names {
		fmt.Fprintf(r.out, "  /%-10s %s\n", n, r.commands[n].Help)
	}
	return false, nil
}

func (r *REPL) cmdReset(context.Context, string) (bool, error) {
	r.conv.Reset()
	fmt.Fprintln(r.out, "conversation cleared")
	return false, nil
}

func (r *REPL) cmdHistory(context.Context, string) (bool, error) {
	n := r.conv.Len()
	if n == 0 {
		fmt.Fprintln(r.out, "no history yet")
		return false, nil
	}
	fmt.Fprintf(r.out, "%d message(s) in history, all resent on every request\n", n)
	return false, nil
}

func (r *REPL) cmdQuit(context.Context, string) (bool, error) {
	fmt.Fprintln(r.out, "bye")
	return true, nil
}
