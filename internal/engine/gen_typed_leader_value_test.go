package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// typedLeaderValueCases are left-recursion leaders without an action of their own, whose values
// are not nodes: the results of their alternatives' actions or of the rules they call. The typed
// runtime finishes a leader's value itself, so the value must survive that, wherever the leader
// is captured, and a node built over it must keep the positions the engine gives it. The typed
// parsers' values are compared with the engine's like those of every other corpus case.
var typedLeaderValueCases = []genCase{
	{"typed leader reads its own value", `rule main string = e:e $$ => $e
rule e string = (l:e "," o:o => $l + $o) / (o:o => $o)
rule o string = x:[a-z] => text($x)`, []string{"a", "a,b", "a,b,c", "a,", ""}},
	{"typed leader over a rule call", `rule main string = "<" e:e ">" $$ => "<" + $e + ">"
rule e string = (l:e "+" o:o => $l + $o) / o
rule o string = x:[a-z] => text($x)`, []string{"<a>", "<a+b>", "<a+b+c>", "<+>", "<a"}},
	{"typed leader in a plain rule", `rule main string = "[" s:p "]" $$ => $s
rule p string = (l:p "." x:[a-z] => $l + "." + text($x)) / (x:[a-z] => text($x))`, []string{"[a]", "[a.b]", "[a.b.c]", "[a.]", "[]"}},
	{"typed leader in an Operators Rule operand", `rule main string = o:ops $$ => $o
rule ops string = #operators {
    operand "[" s:p "]" => "[" + $s + "]"
    operand x:[a-z] => text($x)
    level { infix left "+" => $lhs + "+" + $rhs }
}
rule p string = (l:p "." x:[a-z] => $l + "." + text($x)) / (x:[a-z] => text($x))`, []string{"[a]", "[a.b]", "c+[a.b]", "[a.b]+c", "[a", "+"}},
	{"typed leader in a negative lookahead predicate", `rule main string = e:e $$ => $e
rule e string = (l:e !("q" {$l != ""}) c:[a-z] => $l + text($c)) / (d:[0-9] => text($d))`, []string{"1a", "1ab", "1q", "1aq", "q", ""}},
	{"typed leader spanned by an operator node", `rule main = v:e $$ => ($v.(node[_]).Start, $v.(node[_]).End, text($v.(node[_])))
rule e = #operators {
    operand p
    level { infix left "+" }
}
rule p string = (l:p "." x:[a-z] => $l + text($x)) / (x:[a-z] => text($x))`, []string{"a+b", "a.b+c", "c+a.b", "a.b.c+d.e+f"}},
	{"typed leader in a tuple node", `rule main = v:q $$ => ($v.Start, $v.End, $v.Value[0], text($v.Value[1]))
rule q = p "!"
rule p string = (l:p "." x:[a-z] => $l + text($x)) / (x:[a-z] => text($x))`, []string{"a!", "a.b!", "a.b.c!", "!", "a."}},
}

// TestGeneratedTypedLeaderValues compares the values of typed parsers with the engine's for
// leaders whose values are not nodes, in both position units and on the reference route of
// Node conversion. TestGeneratedTypes covers the cases again within its corpus.
func TestGeneratedTypedLeaderValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	testGeneratedTypesCorpus(t, goBin, typedLeaderValueCases, GenOptions{convertTypes: true}, len(typedLeaderValueCases))
}
