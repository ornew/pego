package gentest

import (
	"fmt"
	"strings"
)

// OutlineKeywords is a grammar of a choice of many keywords, each a sequence of classes of both cases,
// which share their letters: the shapes that the outlining of generated TypeScript acts on.
func OutlineKeywords() (grammar string, inputs []string) {
	words := []string{"select", "selects", "set", "setup", "from", "frozen", "union", "unique", "update", "use",
		"using", "values", "view", "virtual", "when", "where", "while", "window", "with", "without", "write", "xor"}
	var alts []string
	for _, w := range words {
		var cs []string
		for _, r := range w {
			cs = append(cs, fmt.Sprintf("[%c%c]", r, r-'a'+'A'))
		}
		alts = append(alts, strings.Join(cs, " "))
	}
	grammar = "rule main = (kw ws)* $$\nrule kw = " + strings.Join(alts, " / ") + "\nrule ws = \" \"*"
	inputs = []string{"select from where", "SELECT FrOm WHERE ", "selec", "xorx xor", "withou", "unions unique uni", "wrote"}
	return
}
