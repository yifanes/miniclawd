package core

import (
	"strings"
	"unicode/utf8"
)

// FloorCharBoundary returns the largest byte index <= index that is a valid
// UTF-8 character boundary in s.
func FloorCharBoundary(s string, index int) int {
	if index >= len(s) {
		return len(s)
	}
	for index > 0 && !utf8.RuneStart(s[index]) {
		index--
	}
	return index
}

// SplitText splits text into chunks of at most maxLen bytes, preferring to
// break at newline boundaries.
func SplitText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		boundary := FloorCharBoundary(remaining, maxLen)
		// Try to break at a newline within the chunk.
		if idx := strings.LastIndex(remaining[:boundary], "\n"); idx > 0 {
			boundary = idx
		}

		chunks = append(chunks, remaining[:boundary])
		remaining = remaining[boundary:]
		// Skip leading newline after split.
		if len(remaining) > 0 && remaining[0] == '\n' {
			remaining = remaining[1:]
		}
	}

	return chunks
}
