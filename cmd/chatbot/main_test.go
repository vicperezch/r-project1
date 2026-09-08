package main

import "testing"

func TestEnvOr(t *testing.T) {
	t.Setenv("CHATBOT_TEST_KEY", "from-env")
	if got := envOr("CHATBOT_TEST_KEY", "fallback"); got != "from-env" {
		t.Errorf("got %q, want the environment value", got)
	}
	if got := envOr("CHATBOT_TEST_MISSING", "fallback"); got != "fallback" {
		t.Errorf("got %q, want the fallback", got)
	}
	// An empty variable is treated as unset, so an exported but blank value
	// does not silently override the default.
	t.Setenv("CHATBOT_TEST_EMPTY", "")
	if got := envOr("CHATBOT_TEST_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("got %q, want the fallback for an empty variable", got)
	}
}

func TestSystemPromptMentionsTheToolNamingScheme(t *testing.T) {
	// The host namespaces tools as server__tool, so the prompt has to explain
	// that or the model invents names.
	if !contains(systemPrompt, "server__tool") {
		t.Error("the system prompt should explain the tool naming scheme")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
