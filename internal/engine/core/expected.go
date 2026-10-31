package core

import (
	"fmt"
	"slices"
	"strings"
)

// expectedInMessage is the number of expected entries that the message of a syntax error lists.
// A longer list is cut after them and ends with the count of the rest; SyntaxError.Expected keeps
// every entry.
const expectedInMessage = 10

// expectedKind ranks an expected entry by its kind: a named entry (such as "end of input" or a
// label) first, then a literal, which begins with a double quote, then a character class, which
// begins with [.
func expectedKind(s string) int {
	if s != "" {
		switch s[0] {
		case '"':
			return 1
		case '[':
			return 2
		}
	}
	return 0
}

// compareExpected orders expected entries: by kind (expectedKind), then by their bytes.
func compareExpected(a, b string) int {
	if ka, kb := expectedKind(a), expectedKind(b); ka != kb {
		return ka - kb
	}
	return strings.Compare(a, b)
}

// sortExpectedEntries sorts expected entries in the order of compareExpected.
func sortExpectedEntries(values []string) { slices.SortFunc(values, compareExpected) }

// expectedShown returns the entries of expected that a message lists and the number of the rest.
func expectedShown(expected []string) ([]string, int) {
	if len(expected) > expectedInMessage {
		return expected[:expectedInMessage], len(expected) - expectedInMessage
	}
	return expected, 0
}

// expectedTail is the end of a message whose list is cut before rest entries.
func expectedTail(rest int) string { return fmt.Sprintf(", ... and %d more", rest) }

// expectedMessage describes the expected entries of a syntax error without messages.
func expectedMessage(expected []string) string {
	if len(expected) == 0 {
		return "syntax error"
	}
	shown, rest := expectedShown(expected)
	msg := "syntax error: expected " + strings.Join(shown, ", ")
	if rest > 0 {
		msg += expectedTail(rest)
	}
	return msg
}
