package mcplog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ProtocolWriter returns the io.Writer to hand to mcp.LoggingTransport for one
// server. mcp.LoggingTransport emits one line per JSON-RPC frame, as
// "write: {json}" for outgoing and "read: {json}" for incoming, so this parses
// those back into entries.
func (l *Logger) ProtocolWriter(server string) io.Writer {
	return &protocolWriter{
		log:       l,
		server:    server,
		weAsked:   map[string]string{},
		theyAsked: map[string]string{},
	}
}

type protocolWriter struct {
	log    *Logger
	server string
	mu     sync.Mutex
	buf    bytes.Buffer

	// A JSON-RPC response carries only an id, so the method it answers is
	// remembered from the matching request. Requests we send and requests the
	// server sends us are tracked separately, because their id spaces are
	// independent and would otherwise collide.
	weAsked   map[string]string
	theyAsked map[string]string
}

func (w *protocolWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No newline yet: put the partial line back and wait for more.
			w.buf.WriteString(line)
			break
		}
		w.record(strings.TrimRight(line, "\r\n"))
	}
	return len(p), nil
}

// resolveMethod names a response after the request it answers.
func (w *protocolWriter) resolveMethod(direction string, id any, method string) string {
	if id == nil {
		return method // a notification, which carries its own method
	}
	key := fmt.Sprint(id)

	if method != "" {
		if direction == DirectionOutgoing {
			w.weAsked[key] = method
		} else {
			w.theyAsked[key] = method
		}
		return method
	}

	// No method means this is a response, so look up what it answers.
	pending := w.weAsked
	if direction == DirectionOutgoing {
		pending = w.theyAsked
	}
	if m, ok := pending[key]; ok {
		delete(pending, key)
		return m
	}
	return ""
}

func (w *protocolWriter) record(line string) {
	if line == "" {
		return
	}

	var direction, payload string
	switch {
	case strings.HasPrefix(line, "write: "):
		direction, payload = DirectionOutgoing, strings.TrimPrefix(line, "write: ")
	case strings.HasPrefix(line, "read: "):
		direction, payload = DirectionIncoming, strings.TrimPrefix(line, "read: ")
	default:
		// Transport level trouble, for example "read error: ...".
		w.log.Write(Entry{
			Level: LevelProtocol, Server: w.server,
			Direction: DirectionIncoming, Error: line, IsError: true,
		})
		return
	}

	e := Entry{Level: LevelProtocol, Server: w.server, Direction: direction}

	var msg struct {
		ID     any             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		e.Error = "unparsed frame: " + payload
		e.IsError = true
		w.log.Write(e)
		return
	}

	e.RPCID = msg.ID
	e.Method = w.resolveMethod(direction, msg.ID, msg.Method)
	if len(msg.Params) > 0 {
		e.Params = msg.Params
	}
	if len(msg.Result) > 0 {
		e.Result = msg.Result
	}
	if len(msg.Error) > 0 {
		e.Error = string(msg.Error)
		e.IsError = true
	}
	w.log.Write(e)
}
