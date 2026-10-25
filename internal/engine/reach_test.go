package engine

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// The grammars have one start rule, main, which the test corpora (they mark every rule as a start
// rule) cannot have.
const (
	// guard and tail are called only from a lookahead, so only their value-free twins are called.
	reachTwinGrammar = `@start
rule main = &guard word+ $$
rule guard = "a" tail
rule tail = "b"?
rule word = [a-z]+ " "?`
	// item is recursive, so the stream body calls it instead of inlining it.
	reachStreamGrammar = `@start
rule main = item* #stream $$
rule item = "(" item* ")" / [a-z]`
)

// reachMixedGrammar has several start rules, a left-recursive rule, a rule called only from a
// lookahead, an Operators Rule reached only through such a rule, #recover with a rule as its skip,
// and a rule whose name starts with an underscore.
const reachMixedGrammar = `@start(default)
rule main = &guard item+ $$
@start
rule alt = lr ";" $$
@start
rule calc = e $$
rule lr = lr "+" num / num
rule num = [0-9]+
rule item = word " "? / stmt
rule word = [a-z]+
rule guard = "a" tail / "c" ops
rule tail = "b"?
rule ops = #operators {
    operand n:num
    level { infix left "+" }
}
rule e = #operators {
    operand n:num
    level { infix left "*" }
}
rule stmt = _kw ("x" #atomic ";") #recover(skip=skipper)
rule _kw = "k"
rule skipper = [^;]* ";"`

var reachStarts = map[string][]string{reachMixedGrammar: {"alt", "calc", ""}}

var reachInputs = map[string][]string{
	reachMixedGrammar:  {"ab cd", "ac", "c1+2 x", "kx;", "kq;kx;", "1+2+3", "2*3*4", "", "a", "k", "ab kx;"},
	reachTwinGrammar:   {"ab cd", "ac", "b", "", "abc d e"},
	reachStreamGrammar: {"(a)b", "((a)(b))c", "(", "", "abc"},
}

// undefinedGenerated returns the functions that the generated Go source calls by a generated name
// (a letter or two and a number) without declaring them.
func undefinedGenerated(t *testing.T, src []byte) []string {
	t.Helper()
	f, err := goparser.ParseFile(token.NewFileSet(), "parser.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^[a-z]{1,2}[0-9]+$`)
	var undefined []string
	for _, id := range f.Unresolved {
		if re.MatchString(id.Name) {
			undefined = append(undefined, id.Name)
		}
	}
	return undefined
}

func countBodies(src []byte) int { return strings.Count(string(src), "].Body = func(") }

func generateReach(t *testing.T, src string, opts GenOptions) []byte {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	opts.Package, opts.Start = "p", "main"
	code, err := generateCode(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// TestUnreachableBodiesAreLeftOut checks that the bodies of rules that nothing can enter are not
// printed, that the printed code calls nothing it does not define, and that the output without
// the pruning has more bodies.
func TestUnreachableBodiesAreLeftOut(t *testing.T) { eachMode(t, testUnreachableBodiesAreLeftOut) }

func testUnreachableBodiesAreLeftOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		opts GenOptions
	}{
		{"twin node", reachTwinGrammar, GenOptions{}},
		{"twin recognizer", reachTwinGrammar, GenOptions{Recognize: true}},
		{"twin document", reachTwinGrammar, GenOptions{Document: true, Recognize: true}},
		{"twin work controls", reachTwinGrammar, GenOptions{WorkControls: true, Recognize: true}},
		{"stream", reachStreamGrammar, GenOptions{Stream: true}},
		{"mixed", reachMixedGrammar, GenOptions{Recognize: true}},
		{"mixed document", reachMixedGrammar, GenOptions{Recognize: true, Document: true}},
		{"mixed observability", reachMixedGrammar, GenOptions{Recognize: true, Observability: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pruned := generateReach(t, tc.src, tc.opts)
			kept := tc.opts
			kept.keepUnreachable = true
			full := generateReach(t, tc.src, kept)
			if got := undefinedGenerated(t, pruned); len(got) > 0 {
				t.Errorf("the pruned output calls undefined functions %v", got)
			}
			if tc.src != reachStreamGrammar && countBodies(pruned) >= countBodies(full) {
				t.Errorf("pruned output has %d rule bodies, the full output %d", countBodies(pruned), countBodies(full))
			}
			if len(pruned) > len(full) {
				t.Errorf("pruned output is larger: %d > %d bytes", len(pruned), len(full))
			}
		})
	}
}

// reachDriver prints what the parser p (the pruned output) and k (the full output) return for
// the inputs, one line each; a panic of the call is printed too.
const reachDriver = `package main

import (
	"fmt"
	"strings"

	k "reachtest/k"
	p "reachtest/p"
)

func call(f func() string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = "PANIC"
		}
	}()
	return f()
}

func show(n interface{ String() string }, err error) string {
	if n == nil {
		return fmt.Sprint("nil ", err)
	}
	return n.String() + " " + fmt.Sprint(err)
}

func main() {
	for _, in := range INPUTS {
		fmt.Println("p parse", call(func() string { n, err := p.Parse(in); return show(n, err) }))
		fmt.Println("k parse", call(func() string { n, err := k.Parse(in); return show(n, err) }))
RECOGNIZE
RULES
STREAM
	}
	_ = strings.Join
}
`

// runReach builds the pruned (p) and the full (k) parser of src and returns their outputs for the
// inputs of the grammar.
func runReach(t *testing.T, src string, opts GenOptions, hook func(*liveRules)) (pruned, full []string) {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("reachtest"))
	opts.Start = "main"
	opts.Package = "p"
	reachHook = hook
	code, err := generateCode(g, opts)
	reachHook = nil
	if err != nil {
		t.Fatal(err)
	}
	write("p/parser.go", string(code))
	opts.Package, opts.keepUnreachable = "k", true
	if code, err = generateCode(g, opts); err != nil {
		t.Fatal(err)
	}
	write("k/parser.go", string(code))
	var inputs []string
	for _, in := range reachInputs[src] {
		inputs = append(inputs, fmt.Sprintf("%q", in))
	}
	recognize, stream := "", ""
	if opts.Recognize {
		recognize = `		fmt.Println("p recognize", call(func() string { return fmt.Sprint(p.Recognize(in)) }))
		fmt.Println("k recognize", call(func() string { return fmt.Sprint(k.Recognize(in)) }))`
	}
	if opts.Stream {
		stream = `		for _, pk := range []string{"p", "k"} {
			pk := pk
			fmt.Println(pk, "stream", call(func() string {
				var out []string
				emit := func(n *p.Node) error { out = append(out, fmt.Sprint(n)); return nil }
				var err error
				if pk == "p" {
					err = p.ParseStream(strings.NewReader(in), emit)
				} else {
					err = k.ParseStream(strings.NewReader(in), func(n *k.Node) error { out = append(out, fmt.Sprint(n)); return nil })
				}
				return strings.Join(out, ";") + " " + fmt.Sprint(err)
			}))
		}`
	}
	var rules strings.Builder
	for _, name := range reachStarts[src] {
		fmt.Fprintf(&rules, `		fmt.Println("p rule %[1]s", call(func() string { n, err := p.ParseRule(%[1]q, in); return show(n, err) }))
		fmt.Println("k rule %[1]s", call(func() string { n, err := k.ParseRule(%[1]q, in); return show(n, err) }))
`, name)
	}
	main := strings.NewReplacer("INPUTS", "[]string{"+strings.Join(inputs, ", ")+"}", "RECOGNIZE", recognize, "RULES", rules.String(), "STREAM", stream).Replace(reachDriver)
	write("main.go", main)
	cmd := exec.Command(goBin, "run", "-trimpath", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case strings.HasPrefix(line, "p "):
			pruned = append(pruned, strings.TrimPrefix(line, "p "))
		case strings.HasPrefix(line, "k "):
			full = append(full, strings.TrimPrefix(line, "k "))
		}
	}
	return pruned, full
}

func TestUnreachableBodiesRunLikeTheFullOutput(t *testing.T) {
	eachMode(t, testUnreachableBodiesRunLikeTheFullOutput)
}

func testUnreachableBodiesRunLikeTheFullOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, tc := range []struct {
		name string
		src  string
		opts GenOptions
	}{
		{"twin", reachTwinGrammar, GenOptions{Recognize: true}},
		{"twin document", reachTwinGrammar, GenOptions{Recognize: true, Document: true}},
		{"stream", reachStreamGrammar, GenOptions{Stream: true, Recognize: true}},
		{"mixed", reachMixedGrammar, GenOptions{Recognize: true}},
		{"mixed document", reachMixedGrammar, GenOptions{Recognize: true, Document: true}},
		{"mixed work controls", reachMixedGrammar, GenOptions{Recognize: true, WorkControls: true}},
		{"mixed observability", reachMixedGrammar, GenOptions{Recognize: true, Observability: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pruned, full := runReach(t, tc.src, tc.opts, nil)
			if len(full) == 0 || strings.Join(pruned, "\n") != strings.Join(full, "\n") {
				t.Errorf("pruned output differs from the full one:\npruned %q\nfull   %q", pruned, full)
			}
			for _, line := range full {
				if strings.Contains(line, "PANIC") {
					t.Errorf("the full output panics: %s", line)
				}
			}
			ok := 0
			for _, line := range full {
				if strings.HasSuffix(line, " <nil>") {
					ok++
				}
			}
			if ok == 0 {
				t.Errorf("no input of the grammar parses, so the comparison shows nothing: %q", full)
			}
		})
	}
}

// TestUnreachableRootControls is the negative control of the reachability: the same differential
// run with a root left out of the first pass's result must show the missing Body at run time (the
// output compiles; the rule's slot is nil).
func TestUnreachableRootControls(t *testing.T) { eachMode(t, testUnreachableRootControls) }

func testUnreachableRootControls(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, tc := range []struct {
		name string
		src  string
		opts GenOptions
		hook func(*liveRules)
		line string // the output that must panic
	}{
		{"start rule of Parse", reachTwinGrammar, GenOptions{Recognize: true}, func(l *liveRules) {
			var keep []ruleKey
			for _, k := range l.roots {
				if k.table != "rules" {
					keep = append(keep, k)
				}
			}
			l.roots = keep
			l.reachFrom(keep)
		}, "parse"},
		{"start rule of Recognize", reachTwinGrammar, GenOptions{Recognize: true}, func(l *liveRules) {
			var keep []ruleKey
			for _, k := range l.roots {
				if k.table != "recRules" {
					keep = append(keep, k)
				}
			}
			l.roots = keep
			l.reachFrom(keep)
		}, "recognize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pruned, full := runReach(t, tc.src, tc.opts, func(l *liveRules) {
				before := len(l.reach)
				tc.hook(l)
				if len(l.reach) >= before {
					t.Fatalf("the hook left out nothing: %d rules stay reachable of %d", len(l.reach), before)
				}
			})
			panics := 0
			for _, line := range pruned {
				if strings.HasPrefix(line, tc.line) && strings.Contains(line, "PANIC") {
					panics++
				}
			}
			if panics == 0 {
				t.Errorf("no %s call panics with the root left out:\n%q", tc.line, pruned)
			}
			for _, line := range full {
				if strings.Contains(line, "PANIC") {
					t.Errorf("the full output panics: %s", line)
				}
			}
		})
	}
}

// TestUnreachableCalleeFailsGeneration checks that a generated call of a rule that the first pass
// found unreachable fails generation, and that a reachable rule without a Body does as well.
func TestUnreachableCalleeFailsGeneration(t *testing.T) {
	eachMode(t, testUnreachableCalleeFailsGeneration)
}

func testUnreachableCalleeFailsGeneration(t *testing.T) {
	g, err := syntax.Parse(reachTwinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	// word is called from main: leaving it out of the reachable rules is a contradiction.
	reachHook = func(l *liveRules) {
		for k := range l.reach {
			if k.table == "rules" && k.id != 0 {
				delete(l.reach, k)
			}
		}
	}
	defer func() { reachHook = nil }()
	_, err = generateCode(g, GenOptions{Start: "main", Package: "p"})
	if err == nil || !strings.Contains(err.Error(), "left out") {
		t.Errorf("generation with a callee left out: err = %v, want an internal error", err)
	}

	// The stream body is a copy of the start rule's body, so its calls repeat the start rule's. With
	// the calls of both left out, the stream body is the only caller of item, and generation must
	// notice that it calls a rule that was left out.
	gs, err := syntax.Parse(reachStreamGrammar)
	if err != nil {
		t.Fatal(err)
	}
	reachHook = func(l *liveRules) {
		delete(l.edges, rootKey)
		for _, k := range l.roots {
			delete(l.edges, k)
		}
		l.reachFrom(l.roots)
	}
	_, err = generateCode(gs, GenOptions{Start: "main", Package: "p", Stream: true})
	if err == nil || !strings.Contains(err.Error(), "left out") {
		t.Errorf("generation with the stream body's callee left out: err = %v, want an internal error", err)
	}

	l := &liveRules{reach: map[ruleKey]bool{{"rules", 1, false}: true, {"rules", 3, false}: true, {"rules", 4, true}: true, {"recRules", 2, false}: true}}
	printed := map[ruleKey]bool{{"rules", 1, false}: true}
	miss := l.missingBodies("rules", printed)
	if len(miss) != 1 || miss[0].id != 3 {
		t.Errorf("missingBodies = %v, want rule 3", miss)
	}
	if miss := l.missingBodies("recRules", printed); len(miss) != 1 || miss[0].id != 2 {
		t.Errorf("missingBodies(recRules) = %v, want rule 2", miss)
	}
}
