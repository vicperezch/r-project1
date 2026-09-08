// Package mcplog records every interaction with every MCP server.
//
// Two levels feed one timeline. The protocol level comes from wrapping each
// transport in mcp.LoggingTransport, which sees every JSON-RPC frame including
// initialize and tools/list. The call level is written by the host and carries
// what the frames do not: which server, which tool, how long it took, and
// whether the result had to be truncated.
package mcplog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Levels and directions used in the log.
const (
	LevelCall     = "call"
	LevelProtocol = "protocol"

	DirectionRequest  = "request"
	DirectionResponse = "response"
	DirectionOutgoing = "outgoing"
	DirectionIncoming = "incoming"
)

const defaultRing = 200

type Entry struct {
	Time       time.Time `json:"ts"`
	Seq        int64     `json:"seq"`
	Level      string    `json:"level"`
	Server     string    `json:"server"`
	Direction  string    `json:"direction"`
	Method     string    `json:"method,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	RPCID      any       `json:"rpc_id,omitempty"`
	Params     any       `json:"params,omitempty"`
	Result     any       `json:"result,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	IsError    bool      `json:"is_error,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type Logger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
	path string

	seq  atomic.Int64
	ring []Entry
	max  int
}

// New opens a fresh log file for this run under dir.
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("mcp-%s.jsonl", time.Now().UTC().Format("20060102-150405")))
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	return &Logger{file: f, enc: json.NewEncoder(f), path: path, max: defaultRing}, nil
}

// Discarding returns a logger that keeps the in-memory ring but writes no
// file, which is what tests use.
func Discarding() *Logger {
	return &Logger{max: defaultRing}
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// NextSeq allocates the id that pairs a request entry with its response.
func (l *Logger) NextSeq() int64 {
	if l == nil {
		return 0
	}
	return l.seq.Add(1)
}

func (l *Logger) Write(e Entry) {
	if l == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.enc != nil {
		// A logging failure must not take down the chat session.
		_ = l.enc.Encode(e)
	}
	l.ring = append(l.ring, e)
	if len(l.ring) > l.max {
		l.ring = l.ring[len(l.ring)-l.max:]
	}
}

// Recent returns up to n of the most recent entries, oldest first.
func (l *Logger) Recent(n int) []Entry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if n <= 0 || n > len(l.ring) {
		n = len(l.ring)
	}
	out := make([]Entry, n)
	copy(out, l.ring[len(l.ring)-n:])
	return out
}

func (l *Logger) Count() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ring)
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file, l.enc = nil, nil
	return err
}
