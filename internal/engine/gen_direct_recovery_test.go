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
	{"typed/recovery_capture_environment", `
type Doc struct { Text *Match, N int }
def main: Doc = [n=0] e:item $$ -> $e
def item: Doc = v:((x:@"é" [n=1] "!") #recover(skip="é" [n=n+2] "?"))
    -> new Doc{Text:$x, N:n}
`, []string{"é!", "é?", "é", "éx", "?", "", "é??"}},
	{"typed/recovery_zero_progress", `
type Atom terminal
def main: Atom = ("ab" #recover(skip="x"?)) $$
`, []string{"ab", "x", "", "a", "y", "xx"}},
	{"typed/recovery_failed_skip_rollback", `
type Doc struct { Text Match, N int }
def main: Doc = [n=0] x:@(("ab" #recover(skip=[n=8] "x" "!") / [n==0] "x" "?")) $$
    -> new Doc{Text:$x, N:n}
`, []string{"ab", "x!", "x?", "x", "", "xx!"}},
	{"typed/recovery_error_inside", `
type Doc struct { Text Match }
def main: Doc = x:@(("é" "!") #error(message="inner") #recover(skip="é?")) $$ -> new Doc{Text:$x}
`, []string{"é!", "é?", "é", "?", "", "é??"}},
	{"typed/recovery_error_outside", `
type Doc struct { Text Match }
def main: Doc = x:@(("é" "!") #recover(skip="é?") #error(message="outer")) $$ -> new Doc{Text:$x}
`, []string{"é!", "é?", "é", "?", "", "é??"}},
	{"typed/recovery_nested", `
type Doc struct { Text Match }
def main: Doc = x:@((("é" "!") #recover(skip="é?" "z")) #recover(skip="é??")) $$ -> new Doc{Text:$x}
`, []string{"é!", "é?z", "é??", "é?", "é", "", "é???"}},
	{"typed/recovery_cut_scope", `
type Doc struct { Text Match }
def main: Doc = x:@(("é" -- "!") #recover(skip="é" -- "?") "z" / "é?x") $$ -> new Doc{Text:$x}
`, []string{"é!z", "é?z", "é?x", "é!", "é?", "é", "", "é?zz"}},
	{"typed/recovery_lookahead", `
type Doc struct { Text Match }
def main: Doc = &(("é" -- "!") #recover(skip="é" -- "?")) x:@(("é" "!") #recover(skip="é?")) $$ -> new Doc{Text:$x}
`, []string{"é!", "é?", "é", "", "é?x"}},
	{"typed/recovery_negative_lookahead", `
type Doc struct { Text Match }
def main: Doc = !(("é" -- "!") #recover(skip="é" -- "?")) x:@"x" $$ -> new Doc{Text:$x}
`, []string{"x", "é!", "é?", "é", "", "x?"}},
	{"typed/recovery_optional_repetition", `
type Doc struct { Text *Match }
def main: Doc = x:@(("a" -- "!") #recover(skip="a?"))? (("b" -- "!") #recover(skip="b?"))* $$ -> new Doc{Text:$x}
`, []string{"", "a!", "a?", "b!", "b?b!", "a?b?b!", "a", "b", "a?b", "a!x"}},
	{"typed/recovery_dead_skip", `
type Doc struct { N int }
def main: Doc = [n=0] (_ #recover(skip="x" [n=7])) $$ -> new Doc{N:n}
`, []string{"", "x", "é"}},
	{"typed/recovery_list_errors", `
type Atom terminal
type Group struct { Item Atom }
type Expr = Atom | Group
def main: []Expr = items:item* $$ -> map($items,(x)=>x)
def item: Expr = (atom / group) #recover(skip=(?^;)+ ";")
def group: Group = "(" a:atom ")" -> new Group{Item:$a}
def atom: Atom = (?a-z)+ ";"
`, []string{"abc;", "1;", "abc;1;def;2;", "(abc;)", "(1;", ";", "", "1"}},
	{"typed/recovery_typed_corpus", strings.NewReplacer(
		"type Node = Call | Name | Num | Seq", "type Group struct { Item Num }\ntype Node = Call | Name | Num | Group",
		`def grp = "[" n:num "]" ";"`, `def grp: Group = "[" n:num "]" ";" -> new Group{Item:$n}`,
	).Replace(typedGrammar), []string{"abc;", "1x;", "abc;1x;def;", "foo(1,bar)!;", "[1];", "", "("}},
	{"typed/recovery_pratt", `
type Atom terminal
type Post struct { X Expr, Text Match }
type Expr = Atom | Post
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    skip (" " "!") #recover(skip=" " "?")
    operand atom
    level {
        prefix ("-" -- "z") #recover(skip="-?") -> new Post{X:$rhs, Text:$op}
        postfix ("[" x:@"é" "]") #recover(skip="[?]") -> new Post{X:$lhs, Text:$op}
    }
}
def atom: Atom = "a"
`, []string{"a", "-za", "-?a", "a[é]", "a[?]", " !a", " ?a", "-a", "a[", "a[?", "-?", "", "a ?[?]"}},
	{"typed/recovery_lr", `
type Doc struct { Text Match }
def main: Doc = x:@expr $$ -> new Doc{Text:$x}
def expr = ((expr "+" -- "a") #recover(skip="a?")) / "a"
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
