package gentest

import (
	"fmt"
	"strings"
)

// Large failure sets traverse memo, lookahead, label and recovery scopes;
// generated parsers compare complete public values and diagnostics.
func WideExpectationsGrammar() string {
	var choices []string
	for i := 0; i < 80; i++ {
		choices = append(choices, fmt.Sprintf("%q", fmt.Sprintf("%02d", i)))
	}
	return `type Token = { Text string }
rule main = ("s" &(wide "!") wide "!" / "m" (wide "!" / wide "?") / "r" (wide #recover(skip=[^;]+ ";"))+ / "e" (wide @error(message="expected a word"))) $$
rule wide Token = t:("é" (` + strings.Join(choices, " / ") + `)) #atomic => Token{Text: text($t)}`
}
