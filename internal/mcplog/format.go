package mcplog

import (
	"encoding/json"
	"fmt"
	"strings"
)

const detailWidth = 96

// Format renders one entry as a single line for the /log command.
func Format(e Entry) string {
	seq := "-  "
	if e.Seq > 0 {
		seq = fmt.Sprintf("%-3d", e.Seq)
	}
	subject := e.Method
	if e.Tool != "" {
		subject = e.Method + " " + e.Tool
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s #%s %-12s %-8s %-28s",
		e.Time.Local().Format("15:04:05"), seq, e.Server, e.Direction, subject)

	if d := detail(e); d != "" {
		b.WriteString(" " + d)
	}
	return strings.TrimRight(b.String(), " ")
}

func detail(e Entry) string {
	switch {
	case e.Error != "":
		return "error: " + clip(strings.TrimSpace(e.Error), detailWidth)
	case e.Direction == DirectionResponse:
		s := fmt.Sprintf("ok in %dms", e.DurationMS)
		if e.IsError {
			s = fmt.Sprintf("tool error in %dms", e.DurationMS)
		}
		if e.Truncated {
			s += ", truncated"
		}
		if e.Result != nil {
			s += ", " + clip(jsonString(e.Result), detailWidth)
		}
		return s
	case e.Params != nil:
		return clip(jsonString(e.Params), detailWidth)
	case e.Result != nil:
		return clip(jsonString(e.Result), detailWidth)
	default:
		return ""
	}
}

func jsonString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(encoded)
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
