package language_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/grammar"
)

func sourceInfoAt(line, col, endLine, endCol int) grammar.SourceInfo {
	return grammar.SourceInfo{Pos: grammar.Pos{Line: line, Col: col}, End: grammar.Pos{Line: endLine, Col: endCol}}
}

// nodeBases are sources with every kind of expression and type. A comment is
// inserted at every offset of each, in several shapes, and both frontends must
// agree wherever both accept the result.
var nodeBases = []string{
	"rule a = \"x\" b* c+ d? / e f\n",
	"rule a = x:b -c &d !e (f g) / h\n",
	"rule a T = \"x\" => N{A: 1}\n",
	"rule a = b #atomic #stream c #missing d #recover(skip=x) e #repeat(2) f #repeat(1,3) g #repeat(,2) h\n",
	"rule a = b @foo @bar(1, \"x\", k=y, z=nil) c @a(true, -3) d #missing @e\n",
	"rule a = level(x, y) _ _|_ ^^ ^ $$ $ . #cut [a-z] [^x]\n",
	"rule a = (b c => N{}) (b => ...) (d)\n",
	"rule a = {x == 1 && y} {z := 1; w} => ...\n",
	"rule a = b => (x) => x + 1 * 2 - -3 / 4 % 5 || !c\n",
	"rule a = b => $x.Foo.$y.(T) + f(1, 2) + []int{1, 2}[0]\n",
	"rule a = b => N{A: 1, B: M{C: 2}}\n",
	"rule a = b => (1, 2) + (1,) + () + (3) + N{1, 2}\n",
	"rule a = b => x < y == z\n",
	"type T = { A []int; B *string; C (int, string); D {E int}; F A | B & C; G node[X]; H _|_ }\n",
	"type U ~ A | B | C\ntype V = node[T]\ntype W ~ (A)\n",
	"type T = {/*a*/ // b\n A int\n}\nrule e = #operators { // c\n operand \"1\"\n level { // d\n infix left \"+\" }\n}\n",
	"rule a = #operators {\n skip \" \"\n operand \"1\" => N{}\n level l { prefix \"-\" => N{}\n infix left \"+\" b; postfix \"!\" }\n}\n",
	"rule a []T = x\n",
	"rule a = b => {x; y := 2\n z}\n",
	"rule a = b => N{A: x, B: y,}\n",
	"package p // t\nrule a = x; // v\n// w\nrule b = (y) ; // z\n",
	"type T = {\n A int; // a\n B int;\n}\ntype U ~ A\n",
	"rule a = (\n x // t\n / y\n)* z\n",
	"rule a = b => (i) => N{A: i} + map(list($x), (j) => j) + $x.(*[]int) + x[0][1]\n",
	"rule e = #operators {\n operand \"1\"\n level a {\n prefix \"-\" ;\n infix left \"+\" ; infix right \"^\"\n}\n} => ...\n",
	"rule a = x:y* @error(message=\"x\") @a() @b(nil) @c(1,)\n",
	"rule a = \"😀\" 😀? // ß\n",
	"rule a = b => N{\n A: 1, // a\n B: 2 // b\n} // n\n",
	"rule a = b => {\n x // a\n y; // b\n z\n}\n",
	"rule a T[U] = b\ntype X = {A (B, C)}\n",
	"var x int\nvar y = 1 // c\nvar z bool // d\nvar w string = \"s\" // e\n",
	"var x int /* a */ = /* b */ 1\nvar y /* c */ bool\n// f\nvar z = true;\nvar w = -1 // g\n",
	"rule a = \"x\"i \"y\"i? (\"z\")i b\nrule i = \"x\"i #missing\n",
	"rule a = (\"x\" / b \"y\") #ignorecase #atomic\nrule b = \"x\" #missing #ignorecase\n",
	"rule a = \"x\" #ignorecase \"y\" @foo #ignorecase c #missing\n",
	"rule e = #operators {\n operand \"1\"\n level l { infix left \"+\" }\n} #ignorecase\nrule f = #operators { operand \"2\" } /* c */ #ignorecase => ...\n",
}

// TestCanonicalVarTrailingComment checks that the comment after a variable declaration without a
// value stays outside the declaration in both frontends.
func TestCanonicalVarTrailingComment(t *testing.T) {
	p, _ := sourceGrammar(t)
	for _, src := range []string{"var z bool // c\n", "var z // c\n", "var z = 1 // c\n", "var z\n", "var z bool /* c */\nvar y = 1\n"} {
		assertNodeParity(t, p, src, src)
	}
}

var insertions = []string{
	"/* c */",
	" /* c */ ",
	" // c\n",
	"\n// c\n",
	"/*a*//*b*/",
	"\n\n// d\n\n// l\n",
	"\n/* t */\n",
	" // t\n// l\n",
}

// TestCanonicalNodeParityNegativeControl shows the comparison sees owners: the
// outermost node at a token takes its comments, and a changed comment, or a
// comment moved to an inner node, is reported as a difference.
func TestCanonicalNodeParityNegativeControl(t *testing.T) {
	p, _ := sourceGrammar(t)
	src := "rule a = (\n  // lead\n  b c\n  / d // trail\n  / e\n) // g\n"
	n, err := p.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	got := canonicalNodes(t, src, n)
	if c := got[nodeKey("Choice", sourceInfoAt(3, 3, 5, 6))]; c.LeadingComments != " lead\n" {
		t.Fatalf("the choice leads its operands: %+v", c)
	}
	if s := got[nodeKey("Seq", sourceInfoAt(3, 3, 3, 6))]; s.LeadingComments != "" {
		t.Fatalf("an inner node keeps no group of an outer one: %+v", s)
	}
	if r := got[nodeKey("Ref", sourceInfoAt(4, 5, 4, 6))]; !slices.Equal(r.TrailingComments, []string{" trail\n"}) {
		t.Fatalf("the operand before a comment owns it: %+v", r)
	}
	manual, err := zp.ParseGrammar(strings.Replace(src, "// trail", "// other", 1))
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got, manualNodes(t, manual)) {
		t.Fatal("changed comment was not detected")
	}
	manual, err = zp.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, manualNodes(t, manual)) {
		t.Fatal("control source differs")
	}
}

// inClass marks the offsets of a source that lie inside a character class,
// where text is class content and no comment can start. A "[" opens a class
// unless it follows a name, ")" or "]" directly, or starts "[]".
func inClass(rs []rune) []bool {
	in := make([]bool, len(rs)+1)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '[' || i+1 < len(rs) && rs[i+1] == ']' {
			continue
		}
		if i > 0 {
			prev := rs[i-1]
			if prev == '_' || prev == ')' || prev == ']' || prev == '}' || prev >= '0' && prev <= '9' ||
				prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' {
				continue
			}
		}
		j := i + 1
		for j < len(rs) && rs[j] != ']' {
			if rs[j] == '\\' {
				j++
			}
			j++
		}
		for k := i + 1; k <= j && k < len(in); k++ {
			in[k] = true
		}
		i = j
	}
	return in
}

// crlf rewrites the line ends of a source.
func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// TestCanonicalNodeParityPairs inserts two comments, at pseudo-random offsets.
func TestCanonicalNodeParityPairs(t *testing.T) {
	p, _ := sourceGrammar(t)
	rng := rand.New(rand.NewSource(1))
	failed := 0
	for bi, base := range nodeBases {
		for _, b := range []string{base, crlf(base)} {
			rs := []rune(b)
			class := inClass(rs)
			for i := 0; i < 600; i++ {
				a, c := rng.Intn(len(rs)+1), rng.Intn(len(rs)+1)
				if a > c {
					a, c = c, a
				}
				if class[a] || class[c] {
					continue
				}
				x, y := insertions[rng.Intn(len(insertions))], insertions[rng.Intn(len(insertions))]
				src := string(rs[:a]) + x + string(rs[a:c]) + y + string(rs[c:])
				if !compareNodes(t, p, fmt.Sprintf("base %d pair (%q)", bi, src), src) {
					failed++
					if failed >= 8 {
						t.Fatal("too many differences")
					}
				}
			}
		}
	}
}

func TestCanonicalNodeParityInsertions(t *testing.T) {
	compared = 0
	defer func() {
		t.Logf("compared %d sources", compared)
		if compared < 4000 {
			t.Errorf("only %d sources were compared", compared)
		}
	}()
	p, _ := sourceGrammar(t)
	failed := 0
	for bi, base := range nodeBases {
		rs := []rune(base)
		class := inClass(rs)
		for off := 0; off <= len(rs); off++ {
			if class[off] {
				continue
			}
			for ii, ins := range insertions {
				src := string(rs[:off]) + ins + string(rs[off:])
				if off%2 == 1 {
					src = crlf(src)
				}
				if !compareNodes(t, p, fmt.Sprintf("base %d offset %d insertion %d (%q)", bi, off, ii, src), src) {
					failed++
					if failed >= 8 {
						t.Fatal("too many differences")
					}
				}
			}
		}
	}
}
