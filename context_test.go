package nand

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTruncateOutput(t *testing.T) {
	out, truncated := truncateOutput("hello", 10)
	if truncated || out != "hello" {
		t.Fatalf("got (%q, %v), want (%q, false)", out, truncated, "hello")
	}

	out, truncated = truncateOutput("hello", 5)
	if truncated || out != "hello" {
		t.Fatalf("exact boundary got (%q, %v), want (%q, false)", out, truncated, "hello")
	}

	out, truncated = truncateOutput("hello", 0)
	if truncated || out != "hello" {
		t.Fatalf("disabled got (%q, %v), want (%q, false)", out, truncated, "hello")
	}

	out, truncated = truncateOutput("abcdefghij", 4)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if !strings.HasPrefix(out, "abcd\n") || !strings.Contains(out, "showing 4 of 10 bytes") {
		t.Errorf("unexpected truncated output: %q", out)
	}
}

func TestCompactMessagesPreservesRecentAndMetadata(t *testing.T) {
	big := strings.Repeat("x", 200)
	opaque := map[string]json.RawMessage{"extra_content": json.RawMessage(`{"sig":"1"}`)}
	original := []Message{
		{Role: RoleUser, Content: "start"},
		{Role: RoleAssistant, ProviderData: opaque, ToolCalls: []ToolCall{{ID: "c1", Name: "read"}}},
		{Role: RoleTool, ToolCallID: "c1", Content: big},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c2", Name: "read"}}},
		{Role: RoleTool, ToolCallID: "c2", Content: big},
		{Role: RoleAssistant, Content: "recent"},
	}

	got, count := compactMessages(original, 1000, 2)
	if count != 0 || got[2].Content != big {
		t.Fatalf("expected no compaction under budget, got count=%d", count)
	}

	got, count = compactMessages(original, 260, 2)
	if count != 1 {
		t.Fatalf("got %d compacted messages, want 1", count)
	}
	if !strings.HasPrefix(got[2].Content, compactedPrefix) {
		t.Errorf("older tool message not compacted: %q", got[2].Content)
	}
	if got[2].ToolCallID != "c1" {
		t.Errorf("lost ToolCallID: %q", got[2].ToolCallID)
	}
	if got[4].Content != big {
		t.Errorf("recent tool message should remain intact: %q", got[4].Content)
	}
	if original[2].Content != big {
		t.Errorf("original slice was mutated")
	}

	_, secondCount := compactMessages(got, 260, 2)
	if secondCount != 0 {
		t.Errorf("already compacted messages should not be re-compacted, got %d", secondCount)
	}
}
