package engine

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

type genCase struct {
	name   string
	src    string
	inputs []string
}

// genCorpus returns the grammars and inputs used to compare backends and generated parsers with the engine.
func genCorpus(t *testing.T) []genCase {
	cases := []genCase{
		{"declared variable initial values", `type R = { N int; S string; B bool; M int }
var n int = 7
var s = "init"
var b bool
var m = -3
rule main R = {s := s + "!"} g:grow "," h:grow "," t:test $$ => R{N: n + $g + $h, S: s, B: b || $t, M: m}
rule grow int = {n := n + 1} "a" => n
rule test bool = "y" {b == false} => b == false
`, []string{"a,a,y", "a,a,x", "a,a,", "a,", "y", ""}},
		// Case-insensitive literals: a suffix and a pragma. Their first characters are both cases, so a
		// guard of such an alternative must test both.
		{"case-insensitive literal alternatives", `rule main = (kw / id)+ !.
rule kw = "select"i / "from"i / "x"
rule id = [a-z]+`, []string{"SELECT", "select", "From fRoM", "sel x", "Xx", "x", "", "1"}},
		{"ignorecase group", `rule main = (("ab" / "cd") #ignorecase "e" / "x") !.`, []string{"ABe", "cdE", "CDe", "ABE", "x", "xe", "", "ab", "cde"}},
		// item is called three times at the same position under different values of d, so a memo
		// entry made for d = 2 would serve the call with d = 3 if d were left out of the key.
		{"memo entries are keyed by the variables the rule reads", `var d int
rule main = a1 / a2 / a3 $$
rule a1 = {d := 1} v:item {$v == "none"}
rule a2 = {d := 2} v:item {$v == "none"}
rule a3 = {d := 3} v:item {$v == "three"} => $v
rule item string = i1 / i2 / i3
rule i1 string = {d == 1} "x" => "one"
rule i2 string = {d == 2} "x" => "two"
rule i3 string = {d == 3} "x" => "three"
`, []string{"x", "y", ""}},
		// A rule is memoized from its second call at a position, so the middle call stores the entry
		// that the last one must not reuse. The last call, made after the bindings are undone, reads
		// the initial values, which must key the memo like the same values bound explicitly.
		{"initial values key the memo like bound ones", `var d int = 1
rule main = a1 / a2 / a3
rule a1 = {d := 1} v:item {$v == "none"}
rule a2 = {d := 2} v:item {$v == "none"}
rule a3 string = v:item $$ => $v
rule item string = i1 / i2
rule i1 string = {d == 1} "x" => "one"
rule i2 string = {d == 2} "x" => "two"
`, []string{"x", "y", ""}},
		{"bindings of a cast value", `var x int
rule main = d:dyn {x := $d.(int)} "z" => x
rule dyn _ = di / ds
rule di = "i" => 1
rule ds = "s" => "str"
`, []string{"iz", "sz", "i", ""}},
		// A failed cast is an evaluation error, so the binding fails and the choice goes on.
		{"a failed cast in a binding backtracks", `var x int
rule main = (d:dyn {x := $d.(int)} "z" / "s" "y") / "s" "w" => x
rule dyn _ = di / ds
rule di = "i" => 1
rule ds = "s" => "str"
`, []string{"iz", "sz", "sy", "sw", "i", ""}},
		{"a cast binding in an Operators Rule operand", `var x int
rule main = e $$
rule e = #operators {
    operand d:dyn {x := $d.(int)}
    level { infix left "+" }
}
rule dyn _ = di / ds
rule di = "i" => 1
rule ds = "s" => "str"
`, []string{"i", "i+i", "s", "i+s", "s+i", ""}},
		{"declared variables are part of the memo key", `var d int
rule main = {d := 1} item "|" {d := 2} item "|" item $$
rule item = {d == 1} "x" / {d == 2} "y" / {d == 0} "z"
`, []string{"x|y|z", "x|y|x", "z|y|z", "x|x|z", ""}},
		{"Operators Rule explicit default action", `rule main = e $$
rule e = #operators {
    operand "α"
    level { infix left "+" }
} => ...`, []string{"α", "α+α", "α+α+α", "α+", "+α", ""}},
		{"direct value-free rollback", `rule main = "a" ("é" "!" / "é" "?") $$ / "o" ("é" "!")? "é"? $$ / "r" ("é" "!") #repeat(1, 3) "é"? $$ / "z" (x:_) #repeat(2, ) $$ / "p" &("é" "!") "é" !"?" "!" $$ / "l" "é" @error(message="letter") #atomic "!" $$ / "e" "ab" @error(message="pair") $$
`, []string{"aé!", "aé?", "aé@", "aé", "o", "oé!", "oé", "oé?", "ré!é!é", "ré!é", "ré", "z", "zé", "zéé", "zééé", "pé!", "pé?", "pé", "lé!", "lé?", "l?", "eab", "ea?", "e", ""}},
		{"direct value-free fallback calls", `var n int
rule main = "m" &(cached "!") cached "!" $$ / "c" #cut plain "!" $$ / "v" {n := 1} plain "?" $$ / "g" {n := 2} plain $$ / "h" recovering $$ / "j" prefixed $$ / "k" left $$
rule cached = "é" / "ab"
rule plain = "é" / guarded
rule guarded = {n == 2} "a"
rule recovering = ("é" ";") #recover(skip="?")
rule prefixed = #operators {
    operand "é"
    level { prefix "-" }
}
rule left = left "+" "é" / "é"
`, []string{"mé!", "mab!", "mé?", "mab", "cé!", "cé?", "ca!", "vé?", "va?", "ga", "gé", "g?", "hé;", "h?", "h", "j-é", "j--é", "j-", "ké+é", "ké+", ""}},
		{"direct value-free decoding and anchors", `rule main = "u" a:("é" / "�" / "�é" / "😀") #atomic $$ / ^^ "na" $ "\n" ^ "é" $$ / "s" [a-z] #repeat(1, 3) $$ / "t" ("x" / _) #repeat(2, ) $$
`, []string{"ué", "u�", "u�é", "u😀", "u?", "na\né", "na\n?", "sabc", "sab", "sabcd", "s", "t", "tx", "txx", "txxx"}},
		{"wide expectation scopes", wideExpectationsGrammar(), []string{"sé00!", "sé@", "mé@", "mé00!", "mé00?", "mé00@", "ré@;é!;", "ré79", "eé@", "eé79", ""}},
		{"stream element recovery", `rule main = "ab" #recover(skip=[^a]+) #recover(skip="!")* #stream $$`,
			[]string{"", "ab", "abab", "abX!ab", "X!", "a"}},
		{"nullable minimum repetitions", `rule main = ("z" zero / "o" one / "t" two / "u" unbounded / "a" atomic / "d" discarded / "p" projected / "f" fallback) $$
rule zero = ("é" / _) #repeat(0) #stream
rule one = ("é" / _) #repeat(0, 1) #stream
rule two = ("é" / _) #repeat(2) #stream
rule unbounded = ("é" / _) #repeat(2, ) #stream
rule atomic = ("é" / _) #repeat(2, ) #atomic
rule discarded = -("é" / _) #repeat(2, )
rule projected = xs:(f:("é" / _)) #repeat(2, ) => map($xs, (x) => x.$f)
rule fallback = ("é" / _) #repeat(3) / "é"`,
			[]string{"z", "zé", "o", "oé", "oéé", "t", "té", "téé", "tééé", "u", "ué", "uéé", "uééé", "a", "aé", "aéé", "d", "dé", "déé", "p", "pé", "péé", "f", "fé", "féé", "fééé", ""}},
		{"stream capture recognition", `type N = { T []node[string] }
rule main = items:(x:"é" #atomic {text($x) == "é"}) #repeat(0, 2) #stream $$ => N{T: map($items, (i) => i.$x)}`,
			[]string{"", "é", "éé", "ééé", "a", "éa"}},
		{"literal text predicate dispatch", `type R = { X *node[string] }
rule main = w:[^!]+ #atomic {text($w) == "a" || "b" == text($w) || text($w) == "c" || text($w) == "é" || text($w) == "😀" || text($w) == "λ" || text($w) == "日本語" || text($w) == "long-literal"} $$ => R{X: $w}`,
			[]string{"a", "b", "c", "é", "😀", "λ", "日本語", "long-literal", "absent", "A", ""}},
		{"literal text predicate controls", `type R = { X *node[string] }
rule main = n:[0-9]+ #atomic {len($n) == 1 || len($n) == 2} {text($n) != "0" && text($n) != "00"} $$ => R{X: $n}`,
			[]string{"1", "2", "0", "00", "123", "x", ""}},
		{"literal text mixed control", `type R = { X *node[string] }
rule main = w:[a-z]+ #atomic {text($w) == "a" || len($w) == 1} $$ => R{X: $w}`,
			[]string{"a", "b", "ab", ""}},
		{"literal text different captures control", `type R = { X *node[string] }
rule main = x:[a-z] #atomic y:[a-z] #atomic {text($x) == "a" || text($y) == "b"} $$ => R{X: $x}`,
			[]string{"ab", "ac", "cb", "ca", "a", ""}},
		{"literal text negation control", `type R = { X *node[string] }
rule main = w:[a-z]+ #atomic {text($w) == "a" || !(text($w) == "b")} $$ => R{X: $w}`,
			[]string{"a", "b", "c", ""}},
		{"literal text missing capture", `type R = { X *node[string] }
rule main = (w:[a-z] #atomic)? {text($w) == "" || text($w) == "a" || text($w) == "b" || text($w) == "a"} $$ => R{X: $w}`,
			[]string{"", "a", "b", "c"}},
		{"literal text variable alias control", `type R = { X node[string] }
var alias string
rule main = w:[a-z]+ #atomic {alias := text($w)} {alias == "a" || alias == "b"} $$ => R{X: $w}`,
			[]string{"a", "b", "c", ""}},
		{"stream empty capture recognition", `rule main = (x:_) #repeat(1) #stream $$`, []string{"", "a", "é"}},
		{"unicode scalar boundaries", `type N = { T node[string] }
rule main = t:("f" [\u{0}-\u{10ffff}] / "e" [\u{d7ff}-\u{e000}] / "n" [^\u{d7ff}-\u{e000}] / "l" ("\u{10ffff}" / "�" / "\u{ffff}")) #atomic $$ => N{T: $t}`,
			[]string{"f\x00", "fa", "f\ud7ff", "f\ue000", "f\ufffd", "f\uffff", "f\U0010ffff", "e\ud7ff", "e\ue000", "e\ue001", "na", "n\ue000", "l\U0010ffff", "l\ufffd", "l\uffff", "la", "f", "f😀x"}},
		{"long inferred chain", inferredChain(64, false, ""), []string{"a", "", "aa", "b"}},
		{"arith", arith, []string{"1+2*3", "-2^2", "3!!", "(1+2)*3", "1==2==3", "1+", ""}},
		{"ast", ast, []string{"a ? b : c ? d : e", "f(a = 1, b), c", "-a(1)[2]", "a->b-c", "f()", "f(,)"}},
		{"restricted OperatorsRule prefixes", restrictedPrefix, []string{"~é", "~~x", "x*y", "é!", "x^y^z", "~é+y", "x*~y+z", "x^~y+z", "x+y", "x|y", "é?", "~é|y", "~é?", "~", ""}},
		{"restricted OperatorsRule prefix memo", strings.Replace(restrictedPrefix,
			`rule main = x:level(e, mul) $$ => $x`, `rule main = &(level(e, mul) "|") x:e $$ => $x`, 1), []string{"~é+y|z", "~é+y", "x|y", "é|z"}},
		{"incremental", incrementalGrammar, []string{"x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n", "x = (1\n", "@\n"}},
		{"records", records, []string{"#records\na=1\nbc=22\n", "#records\nx\n"}},
		{"indent", `var indent int
rule main = {indent := 0} block $$
rule block = item+
rule item = s:spaces {len($s) == indent} name:[a-z]+ #atomic "\n" children?
rule children = &s:spaces {len($s) > indent} {indent := len($s)} block
rule spaces = " "* #atomic`, []string{"a\n  b\n  c\n    d\ne\n", "a\n   b\n c\n"}},
		{"recover", `rule main = stmt* $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`, []string{"ab;1x;cd;9;", "1x;ab"}},
		{"error attr", `rule main = "let" " " name @error(message="expected a name") ";"
rule name = [a-z]+ #atomic [0-9]*`, []string{"let 1;", "let a", "let ab1;"}},
		{"diagnostic attr", `rule main = "n" number $$ / "e" ("a" @error(message="explicit") @diagnostic(name="letter")) $$ / "s" ("a"+ @diagnostic(name="letter") "b") $$ / "c" ("a" @diagnostic(name="letter") / "b") $$ / "r" ("a" @diagnostic(name="letter") #recover(skip=";")) $$ / "q" ("a" #recover(skip=";") @diagnostic(name="letter")) $$
rule number = ([0-9]+ "." [0-9]+) @diagnostic(name="number")`, []string{"n12x", "n12.", "n12.3", "ex", "ea", "saac", "saab", "cx", "ca", "cb", "r;", "q;", "r", "q", ""}},
		{"left recursion", `rule main = a
rule a = b "x" / "y"
rule b = a "z" / c
rule c = a "w" / "v"`, []string{"y", "vx", "yzxwx", "yz"}},
		{"fold and lists", `type Op = { Left Node; Op node[string]; Right Node }
type Node ~ Op | node[string]
type Args = { Items []node[string]; N int; S string; B bool }
rule main = e:(expr #atomic) ";" a:args => Args{Items: concat($a.Items, $a.Items), N: len($a.Items) * 2 - 1, S: text($e) + "!", B: !($a.N == 0) || false}
rule expr Node = l:num rest:(op:("+" / "-") #atomic r:num)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.$op, Right: i.$r})
rule args Args = first:num rest:(-"," x:num)* => Args{Items: concat(list($first), map($rest, (r) => r.$x)), N: len($rest)}
rule num = [0-9]+ #atomic`, []string{"1+2-3;4,5,6", "1;2", "1-;2"}},
		{"projected and indexed repetition", `type L = { Items []node[string]; Rest node[_]; N int }
rule main = first:x rest:(-"," r:x)* => L{Items: concat(list($first), map($rest, (e) => e.$r)), Rest: $rest, N: len($rest)}
rule x = [a-z]+ #atomic`, []string{"a,b,c", "a", "a,"}},
		{"runtime error", `type A = { N int }
rule main = x:"a"* #atomic => A{N: 10 / len($x)}`, []string{"aa", ""}},
		// Integers are 64-bit and wrap around; the operands depend on the input.
		{"integer boundaries", integerBoundaryGrammar, []string{"a", "aa", ""}},
		{"cut and lookahead", `rule main = ("a" #cut "b" / "a" #cut "c") rest
rule rest = &x:"!" "!"? !"?" .*`, []string{"ab!x", "ac", "ab?"}},
		{"memo keyed by variables", `var n int
rule main = {n := 1} r "x" / {n := 2} r .*
rule r = {n == 1} "a" / {n == 2} "b"`, []string{"ax", "b", "ay", "bx"}},
		// Value and predicate actions read callers' variables, shadow them,
		// roll back failed definitions and keep definitions from repetition
		// elements; the failing peek first calls the memoized plain twice
		// with another depth at the same position. An undefined variable fails
		// a predicate but aborts the parse in a value action.
		{"action environments", actionEnvironmentGrammar, []string{"a", "a%b", "(a)", "((ab?))", "#x:a", "#x:((b))", "#x:(#y:b!)", "((b))", "a!!", "a!", "(((a)))", "~a", "a(#t:b)c?", "[a,bc]", "[]", "(#q:[x])y", "a)", ""}},
		{"empty prefix and postfix", `rule main = e $$
rule e = #operators {
    operand "x"
    level { prefix "-"? }
    level { postfix "!"? }
}`, []string{"x", "-x!", "--x", ""}},
		// An empty operand and an empty infix operator: an application that consumes nothing ends
		// the expression.
		{"empty operand and infix", `rule main = e $$
rule e = #operators {
    operand "a"?
    level { infix left _ }
}`, []string{"a", "", "aa"}},
		{"anchors and classes", `rule main = (^ [a-z]+ #atomic $ "\n"?)* $$ [^\n]? _`, []string{"ab\ncd\n", "ab\n1", "日本", "é\n\xff"}},
		{"scans", `rule main = a:[a-z]* -[0-9] #repeat(2, 3) -. #repeat(1, 2) -[^,]+ "," rest:.* #atomic`, []string{"ab12xy,z", "123,", "1", "abc12éq,é"}},
	}
	cases = append(cases, prefixPartCutCorpus())
	cases = append(cases, typedDirectCutCases...)
	cases = append(cases, actionVariableCases...)
	cases = append(cases, actionLifecycleCases...)
	cases = append(cases, envBindingCases...)
	cases = append(cases, aliasCases...)
	cases = append(cases, wideRepetitionCases()...)
	cases = append(cases, reachabilityGeneratedCases...)
	cases = append(cases, nodeDirectCases...)
	cases = append(cases, typedLRBodyCases...)
	cases = append(cases, typedOperatorsRuleBodyCases...)
	cases = append(cases, operatorsRuleOperandActionCases...)
	cases = append(cases, typedFramedCutCases(t)...)
	cases = append(cases, typedFrameLayoutCases...)
	cases = append(cases, typedRecoveryCases...)
	cases = append(cases, typedLocalLayoutCases...)
	cases = append(cases, attributeShapeCases...)
	// Grammars of typed values with their inputs, from reviews of the typed runtime: each was a
	// case where it differed from converting the result of Parse.
	typed, _ := filepath.Glob("testdata/typed/*.pego")
	for _, gp := range typed {
		src, err := os.ReadFile(gp)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(strings.TrimSuffix(gp, ".pego") + ".json")
		if err != nil {
			t.Fatal(err)
		}
		c := genCase{name: gp, src: string(src)}
		if err := json.Unmarshal(data, &c.inputs); err != nil {
			t.Fatalf("%s: %v", gp, err)
		}
		cases = append(cases, c)
	}
	// Grammars and inputs from examples/.
	grammars, _ := filepath.Glob("../../examples/*/*.pego")
	for _, gp := range grammars {
		src, err := os.ReadFile(gp)
		if err != nil {
			t.Fatal(err)
		}
		inputs, _ := filepath.Glob(filepath.Join(filepath.Dir(gp), "testdata", "*.txt"))
		c := genCase{name: gp, src: string(src)}
		for _, in := range inputs {
			data, _ := os.ReadFile(in)
			c.inputs = append(c.inputs, string(data))
		}
		cases = append(cases, c)
	}
	return append(append(append(constructorCases(), tupleLiteralCases...), inlineActionGenCases()...), append(spliceGenCases(), cases...)...)
}

func resultJSON(n *Node, err error) string {
	out := map[string]any{"node": n}
	if err != nil {
		out["err"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// TestGeneratedParsersMatchEngine checks that generated parsers return the same results as the engine:
// the same trees, positions included, and the same errors, and that Recognize returns the errors of
// the engine's recognition.
func TestGeneratedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := genCorpus(t)
	// Exactly at and just beyond the default nesting limit (DefaultMaxDepth rule calls). Only
	// here: the iterative VM, which other tests run on the corpus, has a higher default.
	cases = append(cases, genCase{"nesting limit", `rule main = n
rule n = "(" n ")" / "x"`, []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	// A direct leaf still counts as a rule call, including when a generic
	// recursive rule invokes it or a rule-table entry reaches its body.
	cases = append(cases, genCase{"direct leaf nesting limit", `rule main = n
rule n = "(" n ")" / leaf
rule leaf = "x"`, []string{strings.Repeat("(", DefaultMaxDepth-3) + "x" + strings.Repeat(")", DefaultMaxDepth-3),
		strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2)}})
	// Chains of prefix and right-associative operators count against the limit too (a OperatorsRule
	// expression parses them by recursion).
	cases = append(cases, genCase{"operatorsRule nesting limit", `rule main = x
rule x = #operators {
    operand "x"
    level { infix right "^" }
    level { prefix "-" }
}`, []string{strings.Repeat("-", 3*DefaultMaxDepth) + "x", strings.Repeat("x^", 3*DefaultMaxDepth) + "x", strings.Repeat("-", DefaultMaxDepth/4) + "x"}})
	t.Run("values", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{}) })
	t.Run("source info", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{SourceInfo: true}) })
}

// nodeViews renders a parse result through the Node API other than JSON: the fields, length and
// text of every node Walk visits, and the String of the result. nodeViewsSource is the same
// function for a generated package, whose name replaces PKG.
func nodeViews(n *Node) string {
	if n == nil {
		return "nil"
	}
	// scalar prints a scalar value and only the presence of any other: printing a node or a list
	// of nodes inside every visit would print its subtree again.
	scalar := func(x any) string {
		switch x.(type) {
		case nil, string, int, bool:
			return fmt.Sprint(x)
		}
		return "value"
	}
	// A node shared by several edges, such as an operand that is also a capture of its
	// operator, is printed once: walking it again on every edge can take exponential time.
	seen := map[*Node]bool{}
	var b strings.Builder
	err := Walk(n, func(v WalkVisit) (WalkAction, error) {
		fmt.Fprintf(&b, "%d %d %d %s %d ", v.Depth, v.Edge.Kind, v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex)
		if v.Node == nil {
			fmt.Fprintf(&b, "%s;", scalar(v.Value))
			return WalkContinue, nil
		}
		if seen[v.Node] {
			b.WriteString("seen;")
			return WalkSkip, nil
		}
		seen[v.Node] = true
		for name, x := range v.Node.Fields() {
			fmt.Fprintf(&b, "%s=%s=%t,", name, scalar(x), v.Node.Field(name) != nil)
		}
		if v.Node.Len() == 0 {
			// The text of a node whose operands are fields spans its subtree: print the
			// length of a long one, or a deep tree prints its input once per level.
			if s := v.Node.Text(); len(s) <= 64 {
				b.WriteString(s)
			} else {
				fmt.Fprintf(&b, "text(%d)", len(s))
			}
		}
		b.WriteString(";")
		return WalkContinue, nil
	})
	if err != nil {
		b.WriteString(err.Error())
	}
	return b.String() + n.String()
}

const nodeViewsSource = `func() string {
	n, _ := n.(*PKG.Node)
	if n == nil {
		return "nil"
	}
	// scalar prints a scalar value and only the presence of any other: printing a node or a list
	// of nodes inside every visit would print its subtree again.
	scalar := func(x any) string {
		switch x.(type) {
		case nil, string, int, bool:
			return fmt.Sprint(x)
		}
		return "value"
	}
	seen := map[*PKG.Node]bool{}
	var b strings.Builder
	err := PKG.Walk(n, func(v PKG.WalkVisit) (PKG.WalkAction, error) {
		fmt.Fprintf(&b, "%d %d %d %s %d ", v.Depth, v.Edge.Kind, v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex)
		if v.Node == nil {
			fmt.Fprintf(&b, "%s;", scalar(v.Value))
			return PKG.WalkContinue, nil
		}
		if seen[v.Node] {
			b.WriteString("seen;")
			return PKG.WalkSkip, nil
		}
		seen[v.Node] = true
		for name, x := range v.Node.Fields() {
			fmt.Fprintf(&b, "%s=%s=%t,", name, scalar(x), v.Node.Field(name) != nil)
		}
		if v.Node.Len() == 0 {
			// The text of a node whose operands are fields spans its subtree: print the
			// length of a long one, or a deep tree prints its input once per level.
			if s := v.Node.Text(); len(s) <= 64 {
				b.WriteString(s)
			} else {
				fmt.Fprintf(&b, "text(%d)", len(s))
			}
		}
		b.WriteString(";")
		return PKG.WalkContinue, nil
	})
	if err != nil {
		b.WriteString(err.Error())
	}
	return b.String() + n.String()
}()`

func testGeneratedParsersCorpus(t *testing.T, goBin string, cases []genCase, opts GenOptions) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("gentest"))
	var imports, parsers, recognizers, viewers strings.Builder
	var inputs [][]string
	var want []string
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts.Package, opts.Start, opts.Recognize = fmt.Sprintf("g%d", i), "main", true
		code, err := generateCode(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; n, err := g%d.Parse(s, g%[3]d.WithUnit(u)); return n, err },\n", i, i, i)
		fmt.Fprintf(&recognizers, "\tfunc(s string, b bool) error { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.Recognize(s, g%[3]d.WithUnit(u)) },\n", i, i, i)
		fmt.Fprintf(&viewers, "\tfunc(n any) string { return %s },\n", strings.ReplaceAll(nodeViewsSource, "PKG", fmt.Sprintf("g%d", i)))
		inputs = append(inputs, c.inputs)
		// The generated code projects (project.go), and records source information as asked.
		prog := compileBare(t, c.src, Options{noProjections: true, SourceInfo: opts.SourceInfo})
		for _, in := range c.inputs {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				n, err := prog.ParseWith("main", in, ParseOptions{Unit: unit})
				out := resultJSON(n, err) + " views: " + strconv.Quote(nodeViews(n))
				_, err = prog.ParseWith("main", in, ParseOptions{Unit: unit, Recognize: true})
				want = append(want, out+" recognize: "+strconv.Quote(fmt.Sprint(err)))
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

var recognizers = []func(string, bool) error{
`+recognizers.String()+`}

var viewers = []func(any) string{
`+viewers.String()+`}

func main() {
	var inputs [][][]byte
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i, in := range inputs {
		for _, b := range in {
			s := string(b)
			for _, bytes := range []bool{false, true} {
				n, err := parsers[i](s, bytes)
				out := map[string]any{"node": n}
				if err != nil {
					out["err"] = err.Error()
				}
				b, _ := json.Marshal(out)
				fmt.Println(string(b) + " views: " + strconv.Quote(viewers[i](n)) + " recognize: " + strconv.Quote(fmt.Sprint(recognizers[i](s, bytes))))
			}
		}
	}
}
`)
	data, _ := json.Marshal(inputBytes(inputs))
	write("inputs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	k := 0
	for _, c := range cases {
		for _, in := range c.inputs {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				if got[k] != want[k] {
					t.Errorf("%s (%v): input %q\n generated %s\n engine    %s", c.name, unit, in, got[k], want[k])
				}
				k++
			}
		}
	}
}

func TestGenerateErrors(t *testing.T) {
	g, _ := syntax.Parse("@start\nrule main = \"a\" b\nrule b = \"b\"")
	if _, err := generateCode(g, GenOptions{Package: "p", Start: "nope"}); err == nil || err.Error() != "rule nope is not defined" {
		t.Errorf("got %v", err)
	}
	if _, err := generateCode(g, GenOptions{Package: "p", Start: "b"}); err == nil || err.Error() != "rule b is not a start rule; parsing can begin only at a rule marked @start" {
		t.Errorf("not a start rule: got %v", err)
	}
	if _, err := generateCode(g, GenOptions{Start: "main"}); err == nil {
		t.Error("expected an error for a missing package name")
	}
	for _, pkg := range []string{"foo-bar", "type", "1p", "_"} {
		if _, err := generateCode(g, GenOptions{Package: pkg, Start: "main"}); err == nil || err.Error() != fmt.Sprintf("invalid package name %q", pkg) {
			t.Errorf("package %q: got %v", pkg, err)
		}
	}
	bad, _ := parseTest(`rule main = x`)
	if _, err := generateCode(bad, GenOptions{Package: "p", Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}

// mutants returns n random edits of each input (a character inserted, deleted or replaced, or two
// characters swapped, up to three times), with characters from the inputs, reproducibly for a seed.
func mutants(inputs []string, n int, seed uint64) []string {
	rng := rand.New(rand.NewPCG(seed, 1))
	var pool []rune
	for _, in := range inputs {
		pool = append(pool, []rune(in)...)
	}
	if len(pool) == 0 {
		return nil
	}
	var out []string
	for _, in := range inputs {
		for range n {
			rs := []rune(in)
			for range 1 + rng.IntN(3) {
				i := rng.IntN(len(rs) + 1)
				switch c := pool[rng.IntN(len(pool))]; rng.IntN(4) {
				case 0:
					rs = append(rs[:i], append([]rune{c}, rs[i:]...)...)
				case 1:
					if i < len(rs) {
						rs = append(rs[:i], rs[i+1:]...)
					}
				case 2:
					if i < len(rs) {
						rs[i] = c
					}
				default:
					if i+1 < len(rs) {
						rs[i], rs[i+1] = rs[i+1], rs[i]
					}
				}
			}
			out = append(out, string(rs))
		}
	}
	return out
}

// typedGrammar exercises typed values: unions with CST members, lists of lists, optional
// matches (s always matches, maybe empty; o may be nil), int and bool fields, a user type named like a runtime type (Node) and Error nodes
// left by #recover.
const typedGrammar = `type Name = { Text string }
type Num = { Text string }
type Node ~ Call | Name | Num | node[_] | error
type Call = { Fn Name; Args []Node; Span node[string]; N int; Opt *node[string]; Rows [][]Num; Ok bool }
rule main []Node = xs:item* $$ => concat(map($xs, (x) => x))
rule item Node = (call / name / num / grp) #recover(skip=[^;]+ ";")
rule call Call = f:name "(" a:args? ")" s:"!"? #atomic o:"?"? ";" => Call{Fn: $f, Args: concat($a), Span: $s, N: len($a), Opt: $o, Rows: list(list()), Ok: true}
rule args = first:item rest:(-"," x:item)* => concat(list($first), map($rest, (r) => r.$x))
rule grp = "[" n:num "]" ";"
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}
rule num Num = t:([0-9]+) #atomic => Num{Text: text($t)}`

// TestGeneratedTypes checks ParseAST of parsers generated with Types: it compiles for every
// grammar of the corpus and returns the errors of Parse, and it converts the trees of
// typedGrammar into the expected values.
func TestGeneratedTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	testGeneratedTypesCorpus(t, goBin, generatedTypesCases(t), GenOptions{convertTypes: true})
}

// generatedTypesCases returns the grammars of TestGeneratedTypes: the corpus, a nesting limit and
// the typed grammar, with mutated inputs for the grammars written for direct rules.
func generatedTypesCases(t *testing.T) []genCase {
	// Exactly at and just beyond the nesting limit, through direct rules (as in
	// TestGeneratedParsersMatchEngine).
	cases := append(genCorpus(t), genCase{"nesting limit", `type R = { T node[string] }
rule main R = t:n #atomic => R{T: $t}
rule n = "(" n ")" / "x"`, []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	cases = append(cases, typedLeaderValueCases...)
	cases = append(cases, genCase{"typed", typedGrammar, []string{"f(1,x)!?;[3];zz", "f(1,(;g();", "f(a);"}})
	// The grammars written for direct rules also get mutated inputs, which reach other partial
	// matches and failures than the written ones.
	for i := range cases {
		if strings.Contains(cases[i].name, "typed/direct_") {
			cases[i].inputs = append(cases[i].inputs, mutants(cases[i].inputs, 8, uint64(i))...)
		}
	}
	return cases
}

func testGeneratedTypesCorpus(t *testing.T, goBin string, cases []genCase, reference GenOptions, requiredTyped ...int) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("gentest"))
	var imports, parsers, converters strings.Builder
	var inputs [][]string
	var wantErr []string
	// wantValue holds the canonical form of the engine's value of each input, and accepted tells
	// which inputs the engine accepts; the canonical typed values of accepted inputs must equal it.
	var wantValue []any
	var accepted []bool
	typedRuntime := 0
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code := generatePackage(t, dir, fmt.Sprintf("g%d", i), g, GenOptions{Start: "main", Types: true, SourceInfo: true})
		if strings.Contains(code, "TParse(trules") {
			typedRuntime++
		}
		if c.name == "OperatorsRule prefix-part cuts" && !strings.Contains(code, "TParse(trules") {
			t.Error("OperatorsRule prefix-part cuts: typed runtime is not exercised")
		}
		// The same through the reference route: Node conversion for the full
		// corpus, or a disabled optimization for focused differential tests.
		refOpts := reference
		refOpts.Start, refOpts.Types = "main", true
		refOpts.SourceInfo = true
		generatePackage(t, dir, fmt.Sprintf("c%d", i), g, refOpts)
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n\tc%d \"gentest/c%d\"\n", i, i, i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.ParseAST(s, g%[3]d.WithUnit(u)) },\n", i, i, i)
		fmt.Fprintf(&converters, "\tfunc(s string, b bool) (any, error) { u := c%d.CodePoints; if b { u = c%d.Bytes }; return c%d.ParseAST(s, c%[3]d.WithUnit(u)) },\n", i, i, i)
		inputs = append(inputs, c.inputs)
		prog := compile(t, c.src)
		for _, in := range c.inputs {
			n, err := prog.Parse("main", in)
			wantErr = append(wantErr, fmt.Sprint(err))
			accepted = append(accepted, err == nil)
			var value any
			if err == nil {
				b, merr := json.Marshal(n)
				if merr != nil {
					t.Fatalf("%s: input %q: %v", c.name, in, merr)
				}
				var engineJSON any
				if merr := json.Unmarshal(b, &engineJSON); merr != nil {
					t.Fatalf("%s: input %q: %v", c.name, in, merr)
				}
				if value, merr = canonicalEngine(engineJSON); merr != nil {
					t.Fatalf("%s: input %q: engine value: %v", c.name, in, merr)
				}
			}
			wantValue = append(wantValue, value)
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

var converters = []func(string, bool) (any, error){
`+converters.String()+`}

// typeName is the dynamic type of v without package qualifiers, also inside type arguments.
func typeName(v any) string {
	var out []byte
	word := 0 // where the current qualified name starts in out
	for _, c := range []byte(fmt.Sprintf("%T", v)) {
		switch {
		case c == '.':
			out = out[:word] // drop the package qualifier
		case c == '_' || c == '/' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z':
			out = append(out, c)
		default:
			out = append(out, c)
			word = len(out)
		}
	}
	return string(out)
}

`+canonicalValueSource+`
func main() {
	var inputs [][][]byte
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i, in := range inputs {
		for _, b := range in {
			s := string(b)
			for _, bytes := range []bool{false, true} {
				v, err := parsers[i](s, bytes)
				cv, cerr := converters[i](s, bytes)
				typed, _ := json.Marshal(map[string]any{"value": v, "err": fmt.Sprint(err)})
				conv, _ := json.Marshal(map[string]any{"value": cv, "err": fmt.Sprint(cerr)})
				if string(typed) != string(conv) || typeName(v) != typeName(cv) {
					fmt.Fprintf(os.Stderr, "MISMATCH %d %q (bytes: %v)\n typed %s %s\n conv  %s %s\n", i, s, bytes, typeName(v), typed, typeName(cv), conv)
				}
				if !bytes {
					b, _ := json.Marshal(map[string]any{"value": v, "canonical": canonicalValue(reflect.ValueOf(v)), "type": fmt.Sprintf("%T", v), "err": fmt.Sprint(err)})
					fmt.Println(string(b))
				}
			}
		}
	}
	// Parsers of the typed runtime are pooled: parsing concurrently gives the same results.
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, in := range inputs {
				for _, b := range in {
					s := string(b)
					v, err := parsers[i](s, false)
					cv, cerr := converters[i](s, false)
					a, _ := json.Marshal(map[string]any{"value": v, "err": fmt.Sprint(err)})
					b, _ := json.Marshal(map[string]any{"value": cv, "err": fmt.Sprint(cerr)})
					if string(a) != string(b) {
						fmt.Fprintf(os.Stderr, "CONCURRENT MISMATCH %d %q\n", i, s)
					}
				}
			}
		}()
	}
	wg.Wait()
}
`)
	data, _ := json.Marshal(inputBytes(inputs))
	write("inputs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("the typed runtime differs from the reference route:\n%s", stderr.String())
	}
	minimum := 5
	if len(requiredTyped) != 0 {
		minimum = requiredTyped[0]
	}
	if typedRuntime < minimum {
		t.Errorf("only %d of %d grammars use the typed runtime", typedRuntime, len(cases))
	}
	t.Logf("%d of %d grammars use the typed runtime", typedRuntime, len(cases))
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(wantErr) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(wantErr), out)
	}
	var got []struct {
		Value json.RawMessage
		Type  string
		Err   string
	}
	for _, l := range lines {
		var r struct {
			Value json.RawMessage
			Type  string
			Err   string
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		got = append(got, r)
	}
	canonical := make([]json.RawMessage, len(lines))
	for i, l := range lines {
		var r struct{ Canonical json.RawMessage }
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		canonical[i] = r.Canonical
	}
	k := 0
	for _, c := range cases {
		for _, in := range c.inputs {
			if got[k].Err != wantErr[k] {
				t.Errorf("%s: input %q: error %s, want %s", c.name, in, got[k].Err, wantErr[k])
			}
			if accepted[k] {
				if err := canonicalTypedMatches(canonical[k], wantValue[k]); err != nil {
					t.Errorf("%s: input %q: typed value %s: %v", c.name, in, got[k].Value, err)
				}
			}
			if want, ok := aliasTypedGoldens[c.name]; ok && in == "éé" && string(got[k].Value) != want {
				t.Errorf("%s: typed alias value is %s, want %s", c.name, got[k].Value, want)
			}
			k++
		}
	}
	if cases[len(cases)-1].name != "typed" {
		return
	}
	typed := got[len(got)-3:]
	for i, want := range []string{
		`{"Value":[` +
			`{"Start":0,"End":9,"Fn":{"Start":0,"End":1,"Text":"f"},"Args":[{"Start":2,"End":3,"Text":"1"},{"Start":4,"End":5,"Text":"x"}],"Span_":{"Start":6,"End":7,"Value":"!"},"N":2,"Opt":{"Start":7,"End":8,"Value":"?"},"Rows":[[]],"Ok":true},` +
			`{"Start":9,"End":13,"Value":[{"Start":9,"End":10,"Value":"["},{"Start":10,"End":11,"Text":"3"},{"Start":11,"End":12,"Value":"]"},{"Start":12,"End":13,"Value":";"}]},` +
			`{"Start":13,"End":15,"Text":"zz"}],"Type":"[]types.Node","Err":"<nil>"}`,
		`{"Value":[` +
			`{"Start":0,"End":1,"Text":"f"},` +
			`{"Start":1,"End":6,"Text":"(1,(;","Message":"1:2: syntax error: expected \"[\", [0-9], [a-z]"},` +
			`{"Start":6,"End":7,"Text":"g"},` +
			`{"Start":7,"End":10,"Text":"();","Message":"1:8: syntax error: expected \"[\", [0-9], [a-z]"}],"Type":"[]types.Node","Err":"1:2: syntax error: expected \"[\", [0-9], [a-z]\n1:8: syntax error: expected \"[\", [0-9], [a-z]"}`,
		`{"Value":[{"Start":0,"End":5,"Fn":{"Start":0,"End":1,"Text":"f"},"Args":[{"Start":2,"End":3,"Text":"a"}],"Span_":{"Start":4,"End":4,"Value":""},"N":1,"Opt":null,"Rows":[[]],"Ok":true}],"Type":"[]types.Node","Err":"<nil>"}`,
	} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.Encode(typed[i])
		want = strings.ReplaceAll(want, "g37", fmt.Sprintf("g%d", len(cases)-1))
		if got := strings.TrimSuffix(b.String(), "\n"); got != want {
			t.Errorf("typed %q:\n got %s\nwant %s", cases[len(cases)-1].inputs[i], got, want)
		}
	}
}

// inputBytes returns inputs as byte slices, which JSON carries exactly (as base64); JSON
// strings would replace invalid UTF-8 with U+FFFD.
func inputBytes(inputs [][]string) [][][]byte {
	out := make([][][]byte, len(inputs))
	for i, in := range inputs {
		for _, s := range in {
			out[i] = append(out[i], []byte(s))
		}
	}
	return out
}
