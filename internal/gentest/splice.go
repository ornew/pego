package gentest

import (
	"regexp"
	"strings"
)

var SpliceQuoted = regexp.MustCompile(`"([^"\\]*)"`)

// SpliceWantLeaves returns the strings the typed value of a grammar must hold given the tokens of
// the node tree. Most keep every token, in order; the grammar with an action holds the text of the
// part it captured, which is the input without the "!" that follows it.
func SpliceWantLeaves(name, input string, tokens []string) []string {
	if name == "splice_recursive_capture_action" {
		return []string{strings.TrimSuffix(input, "!")}
	}
	return tokens
}
