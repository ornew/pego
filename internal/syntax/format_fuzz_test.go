package syntax

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// listGen writes random comma-separated lists with comments in every place a
// comment can stand. The seed makes a run repeat.
type listGen struct {
	r *rand.Rand
	n int
	// inline limits the comments to block comments between tokens of one line,
	// and the lists to the ones that stay on one line.
	inline bool
}

// slot returns what stands between two tokens of a list: any comment, or none.
func (g *listGen) slot() string {
	g.n++
	if g.inline {
		if g.r.IntN(3) == 0 {
			return fmt.Sprintf(" /* b%d */ ", g.n)
		}
		return ""
	}
	switch g.r.IntN(13) {
	case 0:
		return fmt.Sprintf(" /* b%d */ ", g.n)
	case 1:
		return fmt.Sprintf(" // l%d\n    ", g.n)
	case 2:
		return fmt.Sprintf("\n    /* o%d */\n    ", g.n)
	case 3:
		return fmt.Sprintf("\n    // m%d\n    ", g.n)
	case 4:
		return "\n    "
	case 5:
		return fmt.Sprintf(" /* x%d\n y */ ", g.n)
	case 6:
		return fmt.Sprintf(" /* b%d */\n    ", g.n)
	case 7:
		return fmt.Sprintf(" /* p%d */ /* q%d */ ", g.n, g.n)
	case 8:
		return fmt.Sprintf(" /* p%d */ // q%d\n    ", g.n, g.n)
	case 9:
		return fmt.Sprintf("\n\n    // r%d\n\n    ", g.n)
	case 10:
		return fmt.Sprintf(" // s%d\n    // t%d\n    ", g.n, g.n)
	}
	return ""
}

// edge is slot for the places next to a delimiter or a comma: after an opening
// delimiter or a comma, and before a closing delimiter. A block comment there
// is next to the element it leads or trails.
func (g *listGen) edge() string { return g.slot() }

// typ returns a random type whose tuples and node[...] arguments hold comments.
func (g *listGen) typ(depth int) string {
	if depth <= 0 || g.r.IntN(3) == 0 {
		return []string{"int", "string", "T", "*T"}[g.r.IntN(4)]
	}
	switch g.r.IntN(3) {
	case 0:
		el := make([]string, 1+g.r.IntN(3))
		for i := range el {
			el[i] = g.typ(depth - 1)
		}
		return g.list("(", ")", el, true)
	case 1:
		return "[]" + g.typ(depth-1)
	}
	return "node" + g.list("[", "]", []string{g.typ(depth - 1)}, false)
}

// attr returns an attribute whose arguments hold comments. An empty argument
// is written nil.
func (g *listGen) attr() string {
	el := make([]string, 1+g.r.IntN(3))
	for i := range el {
		el[i] = []string{"1", "\"s\"", "nil"}[g.r.IntN(3)]
	}
	if g.r.IntN(2) == 0 {
		el[len(el)-1] = "k=2"
	}
	return "@custom" + g.list("(", ")", el, false)
}

func (g *listGen) list(open, close string, elems []string, tuple bool) string {
	var b strings.Builder
	b.WriteString(open)
	for i, e := range elems {
		b.WriteString(g.edge())
		b.WriteString(e)
		last := i == len(elems)-1
		comma := !last || tuple && len(elems) == 1 || !g.inline && g.r.IntN(3) == 0
		if last && !comma {
			b.WriteString(g.edge())
		} else {
			b.WriteString(g.slot())
		}
		if comma {
			b.WriteString(",")
		}
	}
	if len(elems) == 0 || b.String()[b.Len()-1] == ',' {
		b.WriteString(g.edge())
	}
	b.WriteString(close)
	return b.String()
}

func (g *listGen) term(depth int) string {
	if depth <= 0 || g.r.IntN(3) == 0 {
		return []string{"1", "\"s\"", "x", "2 + 3"}[g.r.IntN(4)]
	}
	kind := g.r.IntN(6)
	n := g.r.IntN(4)
	if kind != 1 && n == 0 {
		n = 1
	}
	elems := make([]string, n)
	for i := range elems {
		elems[i] = g.term(depth - 1)
	}
	switch kind {
	case 0:
		return "f" + g.list("(", ")", elems, false)
	case 1:
		if n == 0 {
			return "()"
		}
		return g.list("(", ")", elems, true)
	case 2:
		return "[]int" + g.list("{", "}", elems, false)
	case 3:
		for i := range elems {
			elems[i] = fmt.Sprintf("F%d: %s", i, elems[i])
		}
		return "S" + g.list("{", "}", elems, false)
	case 4:
		return "map(x, " + g.list("(", ")", []string{"a", "b", "c"}[:len(elems)], false) + " => " + elems[0] + ")"
	}
	return "N" + g.list("{", "}", elems[:1], false)
}

// TestFormatRandomLists formats random lists with comments, and checks that the
// result parses to the same grammar with the same comments, each owned by the
// same node, and that formatting it again changes nothing.
func TestFormatRandomLists(t *testing.T) {
	wraps := []struct {
		name string
		wrap func(body string) string
	}{
		{"action", func(s string) string { return "rule r = \"a\" => " + s + "\n" }},
		{"group", func(s string) string { return "rule r = (\"a\" => " + s + ") \"b\"\n" }},
		{"predicate", func(s string) string { return "rule r = \"a\" {" + s + " == 1} \"b\"\n" }},
		{"level", func(s string) string {
			return "rule e = operators {\n    operand \"n\"\n    level { infix left \"+\" => " + s + " }\n}\n"
		}},
		{"repeat", func(s string) string {
			return "rule r = (\"a\" #repeat(2,) \"b\" #repeat(,3) \"c\" #repeat(2) \"d\" #repeat(2, 3) => " + s + ") \"e\"\n"
		}},
		{"attribute", func(s string) string {
			return "rule r = (\"a\" @error(message=\"m\",) \"b\" => " + s + ") \"c\"\n"
		}},
	}
	const seeds = 1000
	for _, w := range wraps {
		t.Run(w.name, func(t *testing.T) {
			failures, parsed := 0, 0
			defer func() {
				if failures == 0 && parsed < seeds*9/10 {
					t.Errorf("only %d of %d random sources parse", parsed, seeds)
				}
			}()
			for seed := uint64(0); seed < seeds && failures < 3; seed++ {
				g := &listGen{r: rand.New(rand.NewPCG(seed, 7)), inline: seed%4 == 0}
				src := w.wrap(g.term(3))
				ast, err := Parse(src)
				if err != nil {
					continue
				}
				parsed++
				if problem := checkRandomFormat(src, ast); problem != "" {
					failures++
					t.Errorf("seed %d\nsource:\n%s\n%s", seed, src, problem)
				}
			}
		})
	}
}

// TestFormatRandomTypes formats random types, and attributes, with comments in
// the same places as the lists of terms: the result types of rules, the types of
// fields and the arguments of attributes.
func TestFormatRandomTypes(t *testing.T) {
	wraps := []struct {
		name string
		wrap func(g *listGen) string
	}{
		{"rule result", func(g *listGen) string { return "rule r " + g.typ(3) + " = \"a\"\n" }},
		{"field", func(g *listGen) string { return "type T = { F " + g.typ(3) + " }\nrule r = \"a\"\n" }},
		{"alias", func(g *listGen) string { return "type T ~ " + g.typ(3) + "\nrule r = \"a\"\n" }},
		{"attribute", func(g *listGen) string { return "rule r = (\"a\" " + g.attr() + " \"b\" => 1) \"c\"\n" }},
	}
	const seeds = 1000
	for _, w := range wraps {
		t.Run(w.name, func(t *testing.T) {
			failures, parsed := 0, 0
			defer func() {
				if failures == 0 && parsed < seeds*7/10 {
					t.Errorf("only %d of %d random sources parse", parsed, seeds)
				}
			}()
			for seed := uint64(0); seed < seeds && failures < 3; seed++ {
				g := &listGen{r: rand.New(rand.NewPCG(seed, 11)), inline: seed%4 == 0}
				src := w.wrap(g)
				ast, err := Parse(src)
				if err != nil {
					continue
				}
				parsed++
				if problem := checkRandomFormat(src, ast); problem != "" {
					failures++
					t.Errorf("seed %d\nsource:\n%s\n%s", seed, src, problem)
				}
			}
		})
	}
}

// checkRandomFormat returns a description of what formatting changes in ast,
// parsed from src.
func checkRandomFormat(src string, ast *grammar.Grammar) string {
	out := grammar.Format(ast)
	again, err := Parse(out)
	if err != nil {
		return fmt.Sprintf("the result does not parse: %v\n%s", err, out)
	}
	if a, b := jsonOf(ast), jsonOf(again); a != b {
		return "the result is a different grammar\n" + out
	}
	if a, b := commentTexts(ast), commentTexts(again); !slices.Equal(a, b) {
		return fmt.Sprintf("the comments changed\n%q\n%q\n%s", a, b, out)
	}
	if second := grammar.Format(again); second != out {
		return "formatting is not idempotent\nfirst:\n" + out + "\nsecond:\n" + second
	}
	if diffs := ownerDiffs(ast, again); len(diffs) > 0 {
		return "the owners changed\n" + strings.Join(diffs, "\n") + "\n" + out
	}
	return ""
}

func jsonOf(g *grammar.Grammar) string {
	c := g.Clone()
	stripSource(reflect.ValueOf(c))
	data, err := grammar.MarshalJSON(c)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func commentTexts(g *grammar.Grammar) []string {
	var out []string
	for _, c := range g.AllComments() {
		out = append(out, c.Text)
	}
	slices.Sort(out)
	return out
}
