package engine

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The lines of an Operators Rule in the Node and recognizer tables are printed from Flow IR, and the
// old printers (directNodeRules, directRecognizer) are not byte twins of that form. The generated text of
// the two routes is therefore compared outside the lines: lineResidue removes from both outputs every
// declaration the matchers of the lines reach, and what is left must be equal up to the numbers in names
// (identNumber, tableVar) and the residue that lineResidue accounts for.

var (
	chunkStart = regexp.MustCompile(`\n(?:func|var|type|const) `)
	chunkName  = regexp.MustCompile(`^(?:func|var) (\w+)`)
	lineRoot   = regexp.MustCompile(`\b(?:M|Skip): (\w+)`)
	// identNumber matches the names whose numbers the two printers assign in their own order: x (locals),
	// L (labels), v (value slots), l_ (closure parameters), e (matcher functions), q and r (the quoted and
	// repetition helpers). Package variables of one-line tables are compared by content (tableVar).
	identNumber = regexp.MustCompile(`\b(x|L|v|l_|e|q|r)\d+\b`)
	// tableVar matches the package variables whose definition is one line: scope tables, shapers and work
	// classes. A reference is compared by the definition it names, not by its number.
	tableVar = regexp.MustCompile(`(?m)^var ((?:scope|sh|workClass)\d+) = (.*)$`)
	tableRef = regexp.MustCompile(`\b(?:scope|sh|workClass)\d+\b`)
	wordRE   = regexp.MustCompile(`\w+`)
)

// goChunks splits generated source into its top-level declarations, in order.
func goChunks(src []byte) []string {
	s := string(src)
	var out []string
	last := 0
	for _, m := range chunkStart.FindAllStringIndex(s, -1) {
		out = append(out, s[last:m[0]+1])
		last = m[0] + 1
	}
	return append(out, s[last:])
}

// normChunk renumbers the names of identNumber and replaces each reference to a one-line table variable
// with its definition. defs maps those variables to their definitions.
func normChunk(c string, defs map[string]string) string {
	c = tableRef.ReplaceAllStringFunc(c, func(n string) string {
		if d, ok := defs[n]; ok {
			return "<" + d + ">"
		}
		return n
	})
	return identNumber.ReplaceAllString(c, "${1}N")
}

// lineChunks is the generated declarations split by their reachability from the matchers of the Operators Rule
// lines (the M and Skip fields of the rule tables). Keys are the chunks with their names normalized by
// normChunk.
type lineChunks struct {
	// outside counts the chunks that the line matchers do not reach.
	outside map[string]int
	// shared counts the chunks that the line matchers reach and that a chunk outside the lines also names:
	// a function that serves both a line and a rule table.
	shared map[string]int
}

func lineClosure(src []byte) lineChunks {
	chunks := goChunks(src)
	defs := map[string]string{}
	for _, m := range tableVar.FindAllStringSubmatch(string(src), -1) {
		defs[m[1]] = m[2]
	}
	byName := map[string]string{}
	for _, c := range chunks {
		if m := chunkName.FindStringSubmatch(c); m != nil {
			byName[m[1]] = c
		}
	}
	reached := map[string]bool{}
	var stack []string
	for _, m := range lineRoot.FindAllStringSubmatch(string(src), -1) {
		stack = append(stack, m[1])
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c, ok := byName[n]
		if !ok || reached[n] {
			continue
		}
		reached[n] = true
		for _, w := range wordRE.FindAllString(c, -1) {
			if _, ok := byName[w]; ok && !reached[w] {
				stack = append(stack, w)
			}
		}
	}
	named := map[string]bool{} // names that a chunk outside the lines mentions
	cl := lineChunks{outside: map[string]int{}, shared: map[string]int{}}
	for _, c := range chunks {
		if m := chunkName.FindStringSubmatch(c); m != nil && reached[m[1]] {
			continue
		}
		for _, w := range wordRE.FindAllString(c, -1) {
			named[w] = true
		}
		cl.outside[normChunk(c, defs)]++
	}
	for _, c := range chunks {
		if m := chunkName.FindStringSubmatch(c); m != nil && reached[m[1]] && named[m[1]] {
			cl.shared[normChunk(c, defs)]++
		}
	}
	return cl
}

// emptyScope is the normalized form of a package variable for an empty capture scope.
var emptyScope = regexp.MustCompile(`^var <\[\]string\{\}> = \[\]string\{\}\n*$`)

// lineResidue compares the output flow of the Flow route with the output direct of the old printers
// outside the lines, and returns the chunks only one side has, after removing the ones that are
// accounted for:
//   - a package variable of an empty capture scope that only the old printer declares for a line;
//   - a chunk of the old output outside the lines that the Flow output has as a function shared between a
//     line and a rule table (exact-text sharing makes the line's matcher and the rule's body one function,
//     which the line closure counts as inside the lines). Each shared function of the Flow output accounts
//     for one such chunk.
func lineResidue(flow, direct []byte) (directOnly, flowOnly []string) {
	f, d := lineClosure(flow), lineClosure(direct)
	budget := map[string]int{}
	for k, n := range f.shared {
		budget[k] = n
	}
	for k, n := range d.outside {
		for ; n > f.outside[k]; n-- {
			if emptyScope.MatchString(k) {
				continue
			}
			if budget[k] > 0 {
				budget[k]--
				continue
			}
			directOnly = append(directOnly, k)
		}
	}
	for k, n := range f.outside {
		for ; n > d.outside[k]; n-- {
			flowOnly = append(flowOnly, k)
		}
	}
	sort.Strings(directOnly)
	sort.Strings(flowOnly)
	return
}

func summary(chunks []string) string {
	if len(chunks) == 0 {
		return "none"
	}
	first := chunks[0]
	if i := strings.IndexByte(first, '\n'); i > 0 {
		first = first[:i]
	}
	return first
}

// operatorsLineRecoveryCases are Operators Rules with a line that recovers, which the Flow route prints as it
// prints the other lines; the parsers must match the engine (they are in the generated corpus). rule is the
// Operators Rule with the line.
var operatorsLineRecoveryCases = []struct {
	genCase
	rule string
}{
	{genCase{"Operators Rule operand with recovery", `rule main = v:e $$ => $v
rule e = #operators {
    operand ("i" ";") #recover(skip="?")
    level { infix left "+" }
}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", ""}}, "e"},
	{genCase{"Operators Rule operand with a cut in the recovered body", `rule main = v:e $$ => $v
rule e = #operators {
    operand ("i" #cut ";") #recover(skip="?")
    level { infix left "+" }
}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", "ix", ""}}, "e"},
}

// TestOperatorsLinesWithRecoveryPrintFromFlow checks that a line with recovery is printed from Flow IR in the
// Node and recognizer tables, and that no line of the rule falls back to generator.expr.
func TestOperatorsLinesWithRecoveryPrintFromFlow(t *testing.T) {
	t.Setenv("PEGO_TEST_ORDINARY", "1")
	for _, c := range operatorsLineRecoveryCases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.src)
			if err != nil {
				t.Fatal(err)
			}
			flowed, fell := map[string]int{}, map[string]int{}
			operatorsRouteHook = func(table string, r *rule) { flowed[table+"/"+r.name]++ }
			operatorsFallbackHook = func(table string, r *rule) { fell[table+"/"+r.name]++ }
			defer func() { operatorsRouteHook, operatorsFallbackHook = nil, nil }()
			if _, err := generateCode(g, GenOptions{Start: "main", Package: "gen", Recognize: true}); err != nil {
				t.Fatal(err)
			}
			if len(fell) > 0 {
				t.Errorf("lines of %s fall back to generator.expr: %v", c.rule, fell)
			}
			for _, table := range []string{"rules", "recRules"} {
				if flowed[table+"/"+c.rule] == 0 {
					t.Errorf("no line of %s is printed from Flow IR in the %s table (%v)", c.rule, table, flowed)
				}
			}
		})
	}
}
