package gogen

import (
	"regexp"
	"strings"
)

// Fixed helpers and one-line package variables are declared before it is known whether the code
// that is finally kept uses them. A unit is such a declaration; the units that no kept text names,
// directly or through another kept unit, are left out.
//
// The mention of a name is a lexical one: any identifier of the text, wherever it stands, counts
// (a name in a comment or a string keeps its unit), so the result keeps every unit the Go
// compiler would need and may keep an unused one. The shakable prefixes and the
// fixed ast helpers are never the name of a local variable, parameter or field of the generated
// code, so no check of scopes is needed.

// shakeUnit is a declaration that is left out unless the kept text names it.
type shakeUnit struct {
	Name string // the identifier that the rest of the code uses to name it
	Text string
}

// keptUnits returns which of units the texts name, directly or through another kept unit.
func keptUnits(units []shakeUnit, texts ...string) []bool {
	index := make(map[string]int, len(units))
	for i, u := range units {
		index[u.Name] = i
	}
	kept := make([]bool, len(units))
	var queue []int
	scan := func(text string) {
		for i := 0; i < len(text); {
			c := text[i]
			if !isIdentStart(c) {
				i++
				continue
			}
			j := i + 1
			for j < len(text) && isIdentPart(text[j]) {
				j++
			}
			if k, ok := index[text[i:j]]; ok && !kept[k] {
				kept[k] = true
				queue = append(queue, k)
			}
			i = j
		}
	}
	for _, t := range texts {
		scan(t)
	}
	for len(queue) > 0 {
		k := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		scan(units[k].Text)
	}
	return kept
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// shakableVar matches the one-line package variables that generated code names by a number: the
// capture layouts, literals, shapers, class tables and class steps of rule bodies.
var shakableVar = regexp.MustCompile(`^var ((?:scope|lit|sh|cls|steps|budgetClass)[0-9]+) = `)

// shakeVars returns the source without the shakable variables that neither the rest of it nor the
// variables that stay name. It runs after the passes that edit the text of the functions, which can
// leave a variable unused.
func shakeVars(vars string) string {
	lines := strings.SplitAfter(vars, "\n")
	var units []shakeUnit
	unitOf := make([]int, len(lines)) // the index+1 of the unit of a line, or 0
	var fixed strings.Builder
	for i, l := range lines {
		if m := shakableVar.FindStringSubmatch(l); m != nil {
			units = append(units, shakeUnit{m[1], l})
			unitOf[i] = len(units)
			continue
		}
		fixed.WriteString(l)
	}
	kept := keptUnits(units, fixed.String())
	var out strings.Builder
	out.Grow(len(vars))
	for i, l := range lines {
		if u := unitOf[i]; u == 0 || kept[u-1] {
			out.WriteString(l)
		}
	}
	return out.String()
}
