package mcplog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWritesJSONLThatCanBeReadBack(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	l.Write(Entry{Seq: l.NextSeq(), Level: LevelCall, Server: "airline",
		Direction: DirectionRequest, Method: "tools/call", Tool: "search_flights",
		Params: map[string]any{"origin": "GUA"}})
	l.Write(Entry{Seq: 1, Level: LevelCall, Server: "airline",
		Direction: DirectionResponse, Method: "tools/call", Tool: "search_flights",
		DurationMS: 34, Result: "3 flights"})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var got []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line is not valid json: %v", err)
		}
		got = append(got, e)
	}
	if len(got) != 2 {
		t.Fatalf("wrote %d lines, want 2", len(got))
	}
	// A request and its response share a seq, which is what pairs them up.
	if got[0].Seq != got[1].Seq {
		t.Errorf("seqs %d and %d do not pair", got[0].Seq, got[1].Seq)
	}
	if got[0].Tool != "search_flights" || got[1].DurationMS != 34 {
		t.Errorf("fields lost in the round trip: %+v %+v", got[0], got[1])
	}
	if got[0].Time.IsZero() {
		t.Error("timestamp was not filled in")
	}
	if !strings.HasSuffix(l.Path(), ".jsonl") || filepath.Dir(l.Path()) != dir {
		t.Errorf("unexpected path %q", l.Path())
	}
}

func TestSeqIsMonotonic(t *testing.T) {
	l := Discarding()
	a, b, c := l.NextSeq(), l.NextSeq(), l.NextSeq()
	if a != 1 || b != 2 || c != 3 {
		t.Errorf("got %d %d %d, want 1 2 3", a, b, c)
	}
}

func TestRingKeepsOnlyTheMostRecent(t *testing.T) {
	l := Discarding()
	l.max = 3
	for i := 1; i <= 10; i++ {
		l.Write(Entry{Seq: int64(i), Server: "s"})
	}
	got := l.Recent(0)
	if len(got) != 3 {
		t.Fatalf("ring holds %d, want 3", len(got))
	}
	// Oldest first, so the last three written.
	if got[0].Seq != 8 || got[2].Seq != 10 {
		t.Errorf("ring holds seqs %d..%d, want 8..10", got[0].Seq, got[2].Seq)
	}
}

func TestRecentClampsToWhatExists(t *testing.T) {
	l := Discarding()
	l.Write(Entry{Seq: 1})
	if n := len(l.Recent(50)); n != 1 {
		t.Errorf("asked for 50, got %d, want 1", n)
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	// The host holds this by value and must not have to nil check every call.
	l.Write(Entry{Server: "x"})
	if l.NextSeq() != 0 || l.Recent(5) != nil || l.Count() != 0 || l.Path() != "" {
		t.Error("nil logger should be inert")
	}
	if err := l.Close(); err != nil {
		t.Error(err)
	}
}

func TestProtocolWriterParsesFrames(t *testing.T) {
	l := Discarding()
	w := l.ProtocolWriter("airline")

	_, _ = w.Write([]byte(`write: {"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"))
	_, _ = w.Write([]byte(`read: {"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n"))

	got := l.Recent(0)
	if len(got) != 2 {
		t.Fatalf("recorded %d frames, want 2", len(got))
	}
	if got[0].Direction != DirectionOutgoing || got[0].Method != "tools/list" {
		t.Errorf("outgoing frame: %+v", got[0])
	}
	if got[0].Level != LevelProtocol || got[0].Server != "airline" {
		t.Errorf("frame not attributed correctly: %+v", got[0])
	}
	if got[1].Direction != DirectionIncoming || got[1].Result == nil {
		t.Errorf("incoming frame: %+v", got[1])
	}
}

func TestProtocolWriterHandlesSplitAndBatchedLines(t *testing.T) {
	l := Discarding()
	w := l.ProtocolWriter("s")

	// A frame arriving in pieces must not be recorded until it is complete.
	_, _ = w.Write([]byte(`write: {"jsonrpc":"2.0",`))
	if l.Count() != 0 {
		t.Fatal("a partial line was recorded early")
	}
	_, _ = w.Write([]byte(`"id":7,"method":"initialize"}` + "\n"))
	if l.Count() != 1 {
		t.Fatalf("completed line not recorded, count %d", l.Count())
	}
	if got := l.Recent(1)[0]; got.Method != "initialize" {
		t.Errorf("reassembled frame is wrong: %+v", got)
	}

	// Several frames in one write must all be recorded.
	_, _ = w.Write([]byte("read: {\"id\":1}\nread: {\"id\":2}\nread: {\"id\":3}\n"))
	if l.Count() != 4 {
		t.Errorf("count %d, want 4 after a batched write", l.Count())
	}
}

func TestProtocolWriterRecordsTransportErrors(t *testing.T) {
	l := Discarding()
	w := l.ProtocolWriter("s")
	_, _ = w.Write([]byte("read error: connection reset\n"))

	got := l.Recent(1)
	if len(got) != 1 || !got[0].IsError || !strings.Contains(got[0].Error, "connection reset") {
		t.Errorf("transport error not recorded: %+v", got)
	}
}

func TestProtocolWriterRecordsJSONRPCErrors(t *testing.T) {
	l := Discarding()
	w := l.ProtocolWriter("s")
	_, _ = w.Write([]byte(`read: {"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"no such method"}}` + "\n"))

	got := l.Recent(1)[0]
	if !got.IsError || !strings.Contains(got.Error, "no such method") {
		t.Errorf("rpc error not captured: %+v", got)
	}
}

func TestFormatIsOneLinePerEntry(t *testing.T) {
	e := Entry{Seq: 12, Level: LevelCall, Server: "airline", Direction: DirectionRequest,
		Method: "tools/call", Tool: "search_flights", Params: map[string]any{"origin": "GUA"}}
	line := Format(e)
	if strings.Contains(line, "\n") {
		t.Fatal("format must produce a single line")
	}
	for _, want := range []string{"#12", "airline", "request", "tools/call", "search_flights", "GUA"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q is missing %q", line, want)
		}
	}
}

func TestFormatSummarisesResponses(t *testing.T) {
	line := Format(Entry{Seq: 3, Direction: DirectionResponse, Method: "tools/call",
		DurationMS: 34, Truncated: true, Result: "lots of text"})
	if !strings.Contains(line, "34ms") || !strings.Contains(line, "truncated") {
		t.Errorf("line %q should report duration and truncation", line)
	}

	line = Format(Entry{Direction: DirectionResponse, Method: "tools/call", IsError: true, DurationMS: 5})
	if !strings.Contains(line, "tool error") {
		t.Errorf("line %q should flag a tool error", line)
	}
}

func TestFormatClipsLongPayloads(t *testing.T) {
	line := Format(Entry{Seq: 1, Direction: DirectionRequest, Method: "tools/call",
		Params: map[string]any{"blob": strings.Repeat("x", 4000)}})
	if len(line) > 200 {
		t.Errorf("line is %d chars, should be clipped", len(line))
	}
	if !strings.Contains(line, "...") {
		t.Error("clipped payload should be marked with an ellipsis")
	}
}

func TestProtocolWriterNamesResponsesAfterTheirRequest(t *testing.T) {
	l := Discarding()
	w := l.ProtocolWriter("s")

	// We ask, the server answers. The answer carries only an id.
	_, _ = w.Write([]byte(`write: {"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"))
	_, _ = w.Write([]byte(`read: {"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n"))

	// The server asks us, we answer. Same id, opposite direction.
	_, _ = w.Write([]byte(`read: {"jsonrpc":"2.0","id":1,"method":"roots/list"}` + "\n"))
	_, _ = w.Write([]byte(`write: {"jsonrpc":"2.0","id":1,"result":{"roots":[]}}` + "\n"))

	got := l.Recent(0)
	if len(got) != 4 {
		t.Fatalf("recorded %d frames, want 4", len(got))
	}
	if got[1].Method != "tools/list" {
		t.Errorf("response method %q, want tools/list", got[1].Method)
	}
	if got[3].Method != "roots/list" {
		t.Errorf("our reply to the server is %q, want roots/list", got[3].Method)
	}
}

func TestProtocolWriterKeepsNotificationMethods(t *testing.T) {
	l := Discarding()
	w := l.ProtocolWriter("s")
	_, _ = w.Write([]byte(`write: {"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
	if got := l.Recent(1)[0]; got.Method != "notifications/initialized" {
		t.Errorf("notification method %q", got.Method)
	}
}
