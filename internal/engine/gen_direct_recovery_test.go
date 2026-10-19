package engine

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var typedRecoveryCases = []genCase{
	{"typed/recovery_capture_environment", `type Doc = { Text *node[string]; N int }
rule main Doc = {n := 0} e:item $$ => $e
rule item Doc = (
    v:(x:"é" #atomic {n := 1} "!") #recover(skip="é" {n := n + 2} "?")
) => Doc{Text: $x, N: n}
`, []string{"é!", "é?", "é", "éx", "?", "", "é??"}},
	{"typed/recovery_zero_progress", `type Atom = { Text string }
rule main Atom = t:("ab" #recover(skip="x"?) $$) #atomic => Atom{Text: text($t)}
`, []string{"ab", "x", "", "a", "y", "xx"}},
	{"typed/recovery_failed_skip_rollback", `type Doc = { Text node[string]; N int }
rule main Doc = (
    {n := 0} x:("ab" #recover(skip={n := 8} "x" "!") / {n == 0} "x" "?") #atomic $$
) => Doc{Text: $x, N: n}
`, []string{"ab", "x!", "x?", "x", "", "xx!"}},
	{"typed/recovery_error_inside", `type Doc = { Text node[string] }
rule main Doc = x:("é" "!") @error(message="inner") #recover(skip="é?") #atomic $$ => Doc{Text: $x}
`, []string{"é!", "é?", "é", "?", "", "é??"}},
	{"typed/recovery_error_outside", `type Doc = { Text node[string] }
rule main Doc = x:("é" "!") #recover(skip="é?") @error(message="outer") #atomic $$ => Doc{Text: $x}
`, []string{"é!", "é?", "é", "?", "", "é??"}},
	{"typed/recovery_nested", `type Doc = { Text node[string] }
rule main Doc = x:("é" "!") #recover(skip="é?" "z") #recover(skip="é??") #atomic $$ => Doc{Text: $x}
`, []string{"é!", "é?z", "é??", "é?", "é", "", "é???"}},
	{"typed/recovery_cut_scope", `type Doc = { Text node[string] }
rule main Doc = x:(("é" #cut "!") #recover(skip="é" #cut "?") "z" / "é?x") #atomic $$ => Doc{Text: $x}
`, []string{"é!z", "é?z", "é?x", "é!", "é?", "é", "", "é?zz"}},
	{"typed/recovery_lookahead", `type Doc = { Text node[string] }
rule main Doc = &("é" #cut "!") #recover(skip="é" #cut "?") x:("é" "!") #recover(skip="é?") #atomic $$ => Doc{Text: $x}
`, []string{"é!", "é?", "é", "", "é?x"}},
	{"typed/recovery_negative_lookahead", `type Doc = { Text node[string] }
rule main Doc = !("é" #cut "!") #recover(skip="é" #cut "?") x:"x" #atomic $$ => Doc{Text: $x}
`, []string{"x", "é!", "é?", "é", "", "x?"}},
	{"typed/recovery_optional_repetition", `type Doc = { Text *node[string] }
rule main Doc = x:("a" #cut "!") #recover(skip="a?")? #atomic ("b" #cut "!") #recover(skip="b?")* $$ => Doc{Text: $x}
`, []string{"", "a!", "a?", "b!", "b?b!", "a?b?b!", "a", "b", "a?b", "a!x"}},
	{"typed/recovery_dead_skip", `type Doc = { N int }
rule main Doc = {n := 0} _ #recover(skip="x" {n := 7}) $$ => Doc{N: n}
`, []string{"", "x", "é"}},
	{"typed/recovery_list_errors", `type Atom = { Text string }
type Group = { Item Atom }
type Expr ~ Atom | Group | error
rule main []Expr = items:item* $$ => map($items, (x) => x)
rule item Expr = (atom / group) #recover(skip=[^;]+ ";")
rule group Group = "(" a:atom ")" => Group{Item: $a}
rule atom Atom = t:([a-z]+ ";") #atomic => Atom{Text: text($t)}
`, []string{"abc;", "1;", "abc;1;def;2;", "(abc;)", "(1;", ";", "", "1"}},
	{"typed/recovery_typed_corpus", strings.NewReplacer(
		"type Node ~ Call | Name | Num | node[_]", "type Group = { Item Num }\ntype Node ~ Call | Name | Num | Group",
		`rule grp = "[" n:num "]" ";"`, `rule grp Group = "[" n:num "]" ";" => Group{Item: $n}`,
	).Replace(typedGrammar), []string{"abc;", "1x;", "abc;1x;def;", "foo(1,bar)!;", "[1];", "", "("}},
	{"typed/recovery_operatorsRule", `type Atom = { Text string }
type Post = { X Expr; Text node[_] | error }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    skip (" " "!") #recover(skip=" " "?")
    operand atom
    level {
        prefix ("-" #cut "z") #recover(skip="-?") => Post{X: $rhs, Text: $op}
        postfix ("[" x:"é" #atomic "]") #recover(skip="[?]") => Post{X: $lhs, Text: $op}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, []string{"a", "-za", "-?a", "a[é]", "a[?]", " !a", " ?a", "-a", "a[", "a[?", "-?", "", "a ?[?]"}},
	{"typed/recovery_lr", `type Doc = { Text node[string] }
rule main Doc = x:expr #atomic $$ => Doc{Text: $x}
rule expr = (expr "+" #cut "a") #recover(skip="a?") / "a"
`, []string{"a", "a+a", "a?", "a?+a", "a+", "a??", "", "b"}},
}

func TestGeneratedTypedRecoveryBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	marker := "// Recovery inlined under the runtime-owned frame."
	for _, c := range typedRecoveryCases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "recoverybody", Start: "main", Types: true, Recognize: true}
		candidate, err := Generate(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(string(candidate), "tparse(trules") || !strings.Contains(string(candidate), marker) {
			t.Fatalf("%s: typed recovery route not exercised", c.name)
		}
		opts.disableTypedRecoveryBodies = true
		reference, err := Generate(g, opts)
		if err != nil || strings.Contains(string(reference), marker) || strings.Contains(string(candidate), "disableTypedRecoveryBodies") {
			t.Fatalf("%s: invalid generation-only control: %v", c.name, err)
		}
		opts.Types = false
		nodeReference, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedRecoveryBodies = false
		node, err := Generate(g, opts)
		if err != nil || !bytes.Equal(nodeReference, node) {
			t.Fatalf("%s: typed recovery control changes Node output: %v", c.name, err)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, typedRecoveryCases, GenOptions{disableTypedRecoveryBodies: true})
	testGeneratedTypesCorpus(t, goBin, typedRecoveryCases, GenOptions{convertTypes: true})
}
