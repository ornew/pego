package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// stringFieldCases read struct fields in actions and predicates. direct reports whether the
// typed runtime may build the values: a string field that every constructor of every struct with
// that field sets reads the same from a typed value as from a node.
func stringFieldCases() []struct {
	gentest.Case
	direct bool
} {
	return []struct {
		gentest.Case
		direct bool
	}{
		{gentest.Case{Name: "string field in predicate", Src: `type Name = { Text string }
type Elem = { Open Name; Close Name }
rule main = e:elem $$ => $e
rule elem = "<" o:name ">" "</" c:name ">" {$c.Text == $o.Text} => Elem{Open: $o, Close: $c}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, Inputs: []string{"<a></a>", "<ab></ab>", "<a></b>", "<a>", ""}}, true},
		{gentest.Case{Name: "string field in action", Src: `type Name = { Text string }
type Pair = { Left Name; Same bool }
rule main = l:name "=" r:name $$ => Pair{Left: $l, Same: $l.Text == $r.Text}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, Inputs: []string{"a=a", "a=b", "ab=ab", "=", ""}}, true},
		{gentest.Case{Name: "string field of a recovered value", Src: `type Name = { Text string }
type Out = { V string; N int; B bool }
rule main = l:name "=" r:(name #recover(skip=[^;]*)) ";" $$ => Out{B: $l.Text == $r.Text}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, Inputs: []string{"a=a;", "a=b;", "a=1;", "a=;", ""}}, true},
		{gentest.Case{Name: "string field in inline action", Src: `type Out = { V string; N int; B bool }
type Name = { Text string }
var k string
rule main = n:name {k := $n.Text} "!" {k != "b"} $$ => Out{V: k}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, Inputs: []string{"a!", "ab!", "a", ""}}, true},
		{gentest.Case{Name: "string field shared by structs", Src: `type Out = { V string; N int; B bool }
type A = { Text string }
type B = { Text string; N int }
rule main = x:(a / b) $$ => Out{V: $x.Text}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule b = t:[0-9]+ #atomic => B{Text: text($t), N: 1}`, Inputs: []string{"ab", "12", "", "a1"}}, true},
		{gentest.Case{Name: "comparisons of scalar fields", Src: `type Out = { V string; N int; B bool }
type Name = { Text string; Len int }
rule main = sorted / pair
rule pair = l:name r:name {$l.Text != $r.Text || $l.Len >= 0} $$ => Out{B: $l.Text < $r.Text, N: $r.Len, V: $r.Text}
rule sorted = "!" l:name r:name {$l.Text <= $r.Text} {"m" > $r.Text} {$r.Text >= ("a")} {$l.Len == 1} $$ => Out{V: $r.Text}
rule name = t:[a-z]+ #atomic -" "? => Name{Text: text($t), Len: 1}`, Inputs: []string{"a b", "b a", "a a", "ab ab", "z y", "a", "!a b", "!b a", "!a z", "!a a"}}, true},
		{gentest.Case{Name: "string field not always set", Src: `type Out = { V string; N int; B bool }
type A = { Text string }
rule main = x:(a / e) $$ => Out{B: $x.Text == ""}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule e = "!" => A{}`, Inputs: []string{"ab", "!", ""}}, false},
		{gentest.Case{Name: "int field not always set", Src: `type Out = { V string; N int; B bool }
type A = { Len int }
rule main = x:(a / e) $$ => Out{B: $x.Len == 0}
rule a = [a-z]+ => A{Len: 1}
rule e = "!" => A{}`, Inputs: []string{"ab", "!", ""}}, false},
		{gentest.Case{Name: "string field shared with another type", Src: `type Out = { V string; N int; B bool }
type A = { Text string }
type B = { Text int }
rule main = x:(a / b) $$ => Out{B: $x.Text == 1}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule b = [0-9]+ => B{Text: 1}`, Inputs: []string{"ab", "12", ""}}, false},
		{gentest.Case{Name: "text struct comparisons", Src: gentest.TextCompareGrammar, Inputs: gentest.TextCompareInputs}, true},
		{gentest.Case{Name: "list field", Src: `type Out = { V string; N int; B bool }
type A = { Items node[_] }
rule main = x:a $$ => Out{N: len($x.Items)}
rule a = xs:([a-z] #atomic)+ => A{Items: $xs}`, Inputs: []string{"ab", ""}}, false},
		{gentest.Case{Name: "string field built by an alias", Src: `type Out = { V string; N int; B bool }
type A = { Text string }
type B ~ A
rule main = x:(a / b) $$ => Out{V: $x.Text}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule b = t:[0-9]+ #atomic => B{Text: text($t)}`, Inputs: []string{"ab", "12", ""}}, false},
	}
}

func TestGeneratedStringFieldReads(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedStringFieldReads)
}

func testGeneratedStringFieldReads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var cases []gentest.Case
	direct := 0
	for _, c := range stringFieldCases() {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if got := strings.Contains(string(code), "TParse(trules"); got != c.direct {
			t.Errorf("%s: typed runtime %t, want %t", c.Name, got, c.direct)
		}
		if c.direct {
			direct++
		}
		cases = append(cases, c.Case)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, direct)
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{})
	}
}
