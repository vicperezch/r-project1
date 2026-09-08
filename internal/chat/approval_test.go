package chat

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/host"
)

func entry(toolName string, ann *mcp.ToolAnnotations) *host.Entry {
	return &host.Entry{
		ToolName: toolName,
		Tool:     &mcp.Tool{Name: toolName, Annotations: ann},
	}
}

func TestReadOnlyToolNamesNeedNoApproval(t *testing.T) {
	safe := []string{
		"search_flights", "get_flight_details", "list_affected_passengers",
		"get_booking", "find_reassignment_options", "read_file", "read_text_file",
		"list_directory", "directory_tree", "search_files", "get_file_info",
		// The real tool names from Anthropic's git reference server.
		"git_status", "git_diff_unstaged", "git_diff_staged", "git_diff",
		"git_log", "git_show", "git_branch",
		// "settings" must not trip the "set" token.
		"get_settings", "list_presets",
	}
	for _, name := range safe {
		if toolMayMutate(entry(name, nil)) {
			t.Errorf("%s should not require approval", name)
		}
	}
}

func TestStateChangingToolNamesRequireApproval(t *testing.T) {
	risky := []string{
		"cancel_flight", "apply_reassignment", "write_file", "edit_file",
		"create_directory", "move_file", "delete_file", "update_record",
		"run_command", "send_email",
		// The real mutating tools from the git reference server.
		"git_commit", "git_add", "git_reset", "git_create_branch", "git_checkout",
	}
	for _, name := range risky {
		if !toolMayMutate(entry(name, nil)) {
			t.Errorf("%s should require approval", name)
		}
	}
}

func TestDestructiveAnnotationAddsApproval(t *testing.T) {
	yes := true
	// A name that looks harmless but is annotated destructive still prompts.
	e := entry("harmless_sounding", &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &yes})
	if !toolMayMutate(e) {
		t.Error("a destructive annotation must require approval")
	}
}

// This is the security property: a server cannot annotate its way past the
// prompt, because the spec says clients must not trust annotations for tool
// use decisions.
func TestReadOnlyAnnotationCannotBypassTheNameHeuristic(t *testing.T) {
	no := false
	e := entry("delete_everything", &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &no})
	if !toolMayMutate(e) {
		t.Error("a readOnly annotation must not downgrade a mutating tool name")
	}
}

func TestUnknownToolIsTreatedAsMutating(t *testing.T) {
	if !toolMayMutate(nil) {
		t.Error("an unknown tool should require approval")
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize("Git.Commit-All_now")
	want := []string{"git", "commit", "all", "now"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
