package chat

import (
	"strings"

	"r-project1/internal/host"
)

// mutatingTokens are the name segments that mark a tool as changing state.
// Matching is on whole segments, so get_settings does not trip on "set".
var mutatingTokens = map[string]bool{
	"write": true, "edit": true, "create": true, "delete": true, "remove": true,
	"move": true, "rename": true, "copy": true, "cancel": true, "apply": true,
	"commit": true, "push": true, "revert": true, "reset": true, "checkout": true,
	"add": true, "set": true, "put": true, "patch": true, "update": true,
	"insert": true, "drop": true, "send": true, "exec": true, "run": true,
	"kill": true, "install": true, "publish": true,
}

// toolMayMutate decides whether a call needs the user to confirm it.
//
// The name heuristic is the primary signal. MCP tool annotations are consulted
// only to require more confirmation, never less: the spec is explicit that a
// client must not make tool use decisions from annotations sent by a server it
// does not trust, so a server cannot mark a destructive tool read only and slip
// past the prompt.
func toolMayMutate(e *host.Entry) bool {
	if e == nil {
		return true
	}
	if nameSuggestsMutation(e.ToolName) {
		return true
	}
	if a := e.Tool.Annotations; a != nil {
		if !a.ReadOnlyHint && a.DestructiveHint != nil && *a.DestructiveHint {
			return true
		}
	}
	return false
}

func nameSuggestsMutation(name string) bool {
	for _, token := range tokenize(name) {
		if mutatingTokens[token] {
			return true
		}
	}
	return false
}

func tokenize(name string) []string {
	fields := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '/' || r == ' '
	})
	return fields
}
