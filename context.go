package nand

import (
	"fmt"
	"strings"
)

const (
	defaultMaxToolOutputBytes = 16 * 1024
	defaultKeepRecentMessages = 4
	compactedPrefix           = "[compacted tool output:"
)

func truncateOutput(output string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output, false
	}
	return fmt.Sprintf("%s\n... [truncated: showing %d of %d bytes]", output[:maxBytes], maxBytes, len(output)), true
}

func compactMessages(messages []Message, maxChars, keepRecent int) ([]Message, int) {
	if maxChars <= 0 || estimateMessageChars(messages) <= maxChars {
		return messages, 0
	}
	if keepRecent < 0 {
		keepRecent = 0
	}
	cutoff := len(messages) - keepRecent
	if cutoff <= 0 {
		return messages, 0
	}

	out := make([]Message, len(messages))
	copy(out, messages)

	compacted := 0
	for i := 0; i < cutoff; i++ {
		if out[i].Role != RoleTool || isAlreadyCompacted(out[i].Content) {
			continue
		}
		out[i].Content = fmt.Sprintf("%s %d bytes omitted]", compactedPrefix, len(out[i].Content))
		compacted++
		if estimateMessageChars(out) <= maxChars {
			break
		}
	}

	if compacted == 0 {
		return messages, 0
	}
	return out, compacted
}

func estimateMessageChars(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
		for _, tc := range msg.ToolCalls {
			total += len(tc.Name) + len(tc.Arguments)
		}
	}
	return total
}

func isAlreadyCompacted(content string) bool {
	return strings.HasPrefix(content, compactedPrefix)
}
