package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stringFieldCases read struct fields in actions and predicates. direct reports whether the
// typed runtime may build the values: a string field that every constructor of every struct with
// that field sets reads the same from a typed value as from a node.
func stringFieldCases() []struct {
	genCase
	direct bool
} {
	return []struct {
		genCase
		direct bool
	}{
		{genCase{"string field in predicate", `type Name = { Text string }
type Elem = { Open Name; Close Name }
rule main = e:elem $$ => $e
rule elem = "<" o:name ">" "</" c:name ">" {$c.Text == $o.Text} => Elem{Open: $o, Close: $c}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, []string{"<a></a>", "<ab></ab>", "<a></b>", "<a>", ""}}, true},
		{genCase{"string field in action", `type Name = { Text string }
type Pair = { Left Name; Same bool }
rule main = l:name "=" r:name $$ => Pair{Left: $l, Same: $l.Text == $r.Text}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, []string{"a=a", "a=b", "ab=ab", "=", ""}}, true},
		{genCase{"string field of a recovered value", `type Name = { Text string }
type Out = { V string; N int; B bool }
rule main = l:name "=" r:(name #recover(skip=[^;]*)) ";" $$ => Out{B: $l.Text == $r.Text}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, []string{"a=a;", "a=b;", "a=1;", "a=;", ""}}, true},
		{genCase{"string field in inline action", `type Out = { V string; N int; B bool }
type Name = { Text string }
var k string
rule main = n:name {k := $n.Text} "!" {k != "b"} $$ => Out{V: k}
rule name = t:[a-z]+ #atomic => Name{Text: text($t)}`, []string{"a!", "ab!", "a", ""}}, true},
		{genCase{"string field shared by structs", `type Out = { V string; N int; B bool }
type A = { Text string }
type B = { Text string; N int }
rule main = x:(a / b) $$ => Out{V: $x.Text}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule b = t:[0-9]+ #atomic => B{Text: text($t), N: 1}`, []string{"ab", "12", "", "a1"}}, true},
		{genCase{"comparisons of scalar fields", `type Out = { V string; N int; B bool }
type Name = { Text string; Len int }
rule main = sorted / pair
rule pair = l:name r:name {$l.Text != $r.Text || $l.Len >= 0} $$ => Out{B: $l.Text < $r.Text, N: $r.Len, V: $r.Text}
rule sorted = "!" l:name r:name {$l.Text <= $r.Text} {"m" > $r.Text} {$r.Text >= ("a")} {$l.Len == 1} $$ => Out{V: $r.Text}
rule name = t:[a-z]+ #atomic -" "? => Name{Text: text($t), Len: 1}`, []string{"a b", "b a", "a a", "ab ab", "z y", "a", "!a b", "!b a", "!a z", "!a a"}}, true},
		{genCase{"string field not always set", `type Out = { V string; N int; B bool }
type A = { Text string }
rule main = x:(a / e) $$ => Out{B: $x.Text == ""}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule e = "!" => A{}`, []string{"ab", "!", ""}}, false},
		{genCase{"int field not always set", `type Out = { V string; N int; B bool }
type A = { Len int }
rule main = x:(a / e) $$ => Out{B: $x.Len == 0}
rule a = [a-z]+ => A{Len: 1}
rule e = "!" => A{}`, []string{"ab", "!", ""}}, false},
		{genCase{"string field shared with another type", `type Out = { V string; N int; B bool }
type A = { Text string }
type B = { Text int }
rule main = x:(a / b) $$ => Out{B: $x.Text == 1}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule b = [0-9]+ => B{Text: 1}`, []string{"ab", "12", ""}}, false},
		{genCase{"text struct comparisons", textCompareGrammar, textCompareInputs}, true},
		{genCase{"list field", `type Out = { V string; N int; B bool }
type A = { Items node[_] }
rule main = x:a $$ => Out{N: len($x.Items)}
rule a = xs:([a-z] #atomic)+ => A{Items: $xs}`, []string{"ab", ""}}, false},
		{genCase{"string field built by an alias", `type Out = { V string; N int; B bool }
type A = { Text string }
type B ~ A
rule main = x:(a / b) $$ => Out{V: $x.Text}
rule a = t:[a-z]+ #atomic => A{Text: text($t)}
rule b = t:[0-9]+ #atomic => B{Text: text($t)}`, []string{"ab", "12", ""}}, false},
	}
}

func TestGeneratedStringFieldReads(t *testing.T) { eachMode(t, testGeneratedStringFieldReads) }

func testGeneratedStringFieldReads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var cases []genCase
	direct := 0
	for _, c := range stringFieldCases() {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := generateCode(g, GenOptions{Package: "p", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := strings.Contains(string(code), "TParse(trules"); got != c.direct {
			t.Errorf("%s: typed runtime %t, want %t", c.name, got, c.direct)
		}
		if c.direct {
			direct++
		}
		cases = append(cases, c.genCase)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, cases, GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true}, direct)
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, GenOptions{})
	}
}
