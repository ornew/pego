package syntax

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
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
	if g.r.IntN(6) == 0 {
		return "@custom" + g.list("(", ")", nil, false)
	}
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
		{"declaration attribute", func(g *listGen) string { return "rule a = \"x\"\n" + g.attr() + " " + g.attr() + "\nrule r = \"a\"\n" }},
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

// TestFormatRandomOpeners formats the constructs that are not lists, with
// comments next to their delimiters: a block comment after an opening delimiter
// leads what follows it, and one before the closing delimiter trails it.
func TestFormatRandomOpeners(t *testing.T) {
	wraps := []struct {
		name string
		wrap func(g *listGen) string
	}{
		{"group", func(g *listGen) string { return "rule r = (" + g.slot() + "\"a\"" + g.slot() + ") \"b\"\n" }},
		{"predicate", func(g *listGen) string { return "rule r = \"a\" {" + g.slot() + "true" + g.slot() + "} \"b\"\n" }},
		{"block", func(g *listGen) string {
			return "rule r = \"a\" {" + g.slot() + "x := 1; x == 1" + g.slot() + "} \"b\"\n"
		}},
		{"struct", func(g *listGen) string {
			return "type T = {" + g.slot() + "F int; G int" + g.slot() + "}\nrule r = \"a\"\n"
		}},
		{"empty struct", func(g *listGen) string { return "type T = {" + g.slot() + "}\nrule r = \"a\"\n" }},
		{"index", func(g *listGen) string {
			return "rule r = \"a\" => x[" + g.slot() + "0]\n"
		}},
		{"type group", func(g *listGen) string { return "rule r (" + g.slot() + "int" + g.slot() + ") = \"a\"\n" }},
		{"declaration attributes", func(g *listGen) string {
			return "rule a = \"x\"\n" + g.slot() + "@start(" + g.slot() + "default" + g.slot() + ")" + g.slot() + "@foo\n" + g.slot() + "rule r = \"a\"\n"
		}},
	}
	const seeds = 1500
	for _, w := range wraps {
		t.Run(w.name, func(t *testing.T) {
			failures, parsed := 0, 0
			for seed := uint64(0); seed < seeds && failures < 3; seed++ {
				// Block comments between the tokens of a line. Comments on lines of their own
				// in these constructs, and next to a semicolon, are not kept in place.
				g := &listGen{r: rand.New(rand.NewPCG(seed, 17)), inline: true}
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
			if failures == 0 && parsed < seeds/2 {
				t.Errorf("only %d of %d random sources parse", parsed, seeds)
			}
		})
	}
}

// repeatForms are the ways to spell the bounds of a #repeat pragma: the minimum
// and the maximum are each there or left out, and so is the comma.
var repeatForms = []struct{ min, comma, max string }{
	{"2", "", ""}, {"2", ",", ""}, {"", ",", "3"}, {"0", ",", "3"}, {"2", ",", "3"},
	{"1", ",", ""}, {"0", ",", ""}, {"2", ",", "2"}, {"", ",", "0"}, {"0", ",", "0"},
}

// TestFormatRandomRepeat formats #repeat pragmas with comments in every gap
// between their tokens. The bounds are written in the canonical spelling, and
// each comment stays before the token it was before, or moves to the next token
// that the canonical spelling has, or else to the end: the comments there have
// no owner.
func TestFormatRandomRepeat(t *testing.T) {
	const seeds = 3000
	failures, parsed := 0, 0
	for seed := uint64(0); seed < seeds && failures < 3; seed++ {
		g := &listGen{r: rand.New(rand.NewPCG(seed, 13))}
		form := repeatForms[g.r.IntN(len(repeatForms))]
		var b strings.Builder
		b.WriteString("(" + g.slot())
		for i, tok := range []string{form.min, form.comma, form.max} {
			if tok == "" {
				continue
			}
			if i > 0 {
				b.WriteString(g.slot())
			}
			b.WriteString(tok)
		}
		b.WriteString(g.slot() + ")")
		src := "rule r = \"a\" #repeat" + b.String() + " \"b\"\n"
		ast, err := Parse(src)
		if err != nil {
			continue
		}
		parsed++
		problem := checkRandomFormat(src, ast)
		if problem == "" {
			again, err := Parse(grammar.Format(ast))
			if err != nil {
				problem = err.Error()
			} else if want, got := repeatSlots(ast, true), repeatSlots(again, false); want != got {
				problem = fmt.Sprintf("the comments moved\nwant %s\ngot  %s", want, got)
			}
		}
		if problem != "" {
			failures++
			t.Errorf("seed %d\nsource:\n%s\n%s", seed, src, problem)
		}
	}
	if failures == 0 && parsed < seeds*9/10 {
		t.Errorf("only %d of %d random sources parse", parsed, seeds)
	}
}

// repeatSlots describes the bounds of the #repeat pragma of the first rule and
// the token that follows each of its comments ("end" for the closing
// parenthesis). With canonical, it maps the slots of the source to the slots of
// the canonical spelling of the bounds: the first token of it that is not before
// the written one.
func repeatSlots(g *grammar.Grammar, canonical bool) string {
	rep := g.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.Repeat)
	names := []string{"min", "comma", "max"}
	var kinds []int
	switch {
	case rep.Min == rep.Max:
		kinds = []int{0}
	case rep.Max < 0:
		kinds = []int{0, 1}
	case rep.Min == 0:
		kinds = []int{1, 2}
	default:
		kinds = []int{0, 1, 2}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d,%d", rep.Min, rep.Max)
	sp := rep.Spelling
	if sp == nil {
		return b.String()
	}
	var written []int
	for k, has := range []bool{sp.HasMin, sp.Comma, sp.HasMax} {
		if has {
			written = append(written, k)
		}
	}
	for j, gap := range sp.Gaps {
		for _, c := range gap.Comments {
			slot := "end"
			if j < len(written) {
				slot = names[written[j]]
				if canonical {
					slot = "end"
					for _, k := range kinds {
						if k >= written[j] {
							slot = names[k]
							break
						}
					}
				}
			}
			fmt.Fprintf(&b, " %s>%s", c.Text, slot)
		}
	}
	return b.String()
}

// checkRandomFormat returns a description of what formatting changes in ast,
// parsed from src.
func checkRandomFormat(src string, ast *grammar.Grammar) string {
	return checkRandomFormatOwners(src, ast, true)
}

// checkRandomFormatOwners is checkRandomFormat that checks the owners of the
// comments only when owners is set.
func checkRandomFormatOwners(src string, ast *grammar.Grammar, owners bool) string {
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
	if diffs := ownerDiffs(ast, again); owners && len(diffs) > 0 {
		return "the owners changed\n" + strings.Join(diffs, "\n") + "\n" + out
	}
	if diffs := argHomeDiffs(ast, again); len(diffs) > 0 {
		return "comments left their arguments\n" + strings.Join(diffs, "\n") + "\n" + out
	}
	return ""
}

// argHomes maps the text of each comment inside the parentheses of an attribute,
// a lambda or a #repeat to the path of that node. No owner claims these
// comments and JSON omits them, so only their place in the tree shows whether
// formatting kept them where they were.
func argHomes(g *grammar.Grammar) map[string]string {
	homes := map[string][]string{}
	var all func(v reflect.Value, path string)
	all = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			if c, ok := v.Interface().(*grammar.Comment); ok {
				homes[c.Text] = append(homes[c.Text], path)
				return
			}
			all(v.Elem(), path)
		case reflect.Interface:
			if !v.IsNil() {
				all(v.Elem(), path)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				all(v.Index(i), path)
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					all(v.Field(i), path)
				}
			}
		}
	}
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem(), path)
			}
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), path+"<"+v.Elem().Type().String()+">")
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), path+"["+strconv.Itoa(i)+"]")
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}
				if f.Name == "ArgsLayout" || f.Name == "Spelling" || f.Name == "ParamsLayout" {
					all(v.Field(i), path+"."+f.Name)
				}
				walk(v.Field(i), path+"."+f.Name)
			}
		}
	}
	walk(reflect.ValueOf(g), "")
	out := map[string]string{}
	for text, paths := range homes {
		slices.Sort(paths)
		out[text] = strings.Join(paths, " & ")
	}
	return out
}

func argHomeDiffs(a, b *grammar.Grammar) []string {
	x, y := argHomes(a), argHomes(b)
	var diffs []string
	for text, home := range x {
		if y[text] != home {
			diffs = append(diffs, fmt.Sprintf("%q: %s -> %s", text, home, y[text]))
		}
	}
	for text, home := range y {
		if _, ok := x[text]; !ok {
			diffs = append(diffs, fmt.Sprintf("%q: none -> %s", text, home))
		}
	}
	slices.Sort(diffs)
	return diffs
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
