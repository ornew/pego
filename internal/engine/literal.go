package engine

import "unicode/utf8"

// literalValue derives matching text from the decoded rune sequence. Public AST
// strings and saved string tables retain their original bytes; action constants
// are not matching expressions. Keep valid literals on the constant-text path.
func literalValue(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}
