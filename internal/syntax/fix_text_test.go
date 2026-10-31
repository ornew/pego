package syntax_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/starttest"
	"github.com/ornew/zeroparse/internal/syntax"
)

// Fix rewrites the structs { Text string } that rules make from the text they match to named
// node types, and the reads of their field Text to text. The result type checks, parses inputs
// to the same text and is a fixed point of Fix.
func TestFixTextStructs(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{
			"type T = { Text string }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t)}\nrule main = x:r \",\" y:r => list($x.Text, $y.Text + \"!\", len($y.Text))",
			"type T = node[string]\nrule r T = ([a-z]+) #atomic\nrule main = x:r \",\" y:r => list(text($x), text($y) + \"!\", len($y))",
		},
		// A rule without a type is declared with the type; parentheses around the body or the
		// #atomic expression go.
		{
			"type T = { Text string }\nrule r = t:(\"a\" #atomic) => T{Text: text($t)}\nrule s=(t:((\"b\") #atomic)) => T{Text: (text($t))}\nrule main = x:(r / s) => $x.Text",
			"type T = node[string]\nrule r T = \"a\" #atomic\nrule s T =(\"b\") #atomic\nrule main = x:(r / s) => text($x)",
		},
		// A rule declared with an alias keeps it; layout and comments stay.
		{
			"// the text\ntype T = {\n    Text string // the field\n}\ntype A ~ T\nrule r A = /* body */ t:([a-z]+) #atomic /* c */ => T{Text: text($t)} // a word\nrule main = r",
			"// the text\ntype T = node[string]\ntype A ~ T\nrule r A = /* body */ ([a-z]+) #atomic /* c */ // a word\nrule main = r",
		},
		// Where another struct has a field Text, only the reads of captures of the rules are
		// rewritten; a read in a lambda is too, where none has.
		{
			"type T = { Text string }\ntype P = { Text string; N int }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t)}\nrule p = \"p\" => P{Text: \"p\", N: 1}\nrule main = x:r y:p => list($x.Text, $y.Text)",
			"type T = node[string]\ntype P = { Text string; N int }\nrule r T = ([a-z]+) #atomic\nrule p = \"p\" => P{Text: \"p\", N: 1}\nrule main = x:r y:p => list(text($x), $y.Text)",
		},
		{
			"type T = { Text string }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t)}\nrule main = xs:r+ => map($xs, (v) => v.Text)",
			"type T = node[string]\nrule r T = ([a-z]+) #atomic\nrule main = xs:r+ => map($xs, (v) => text(v))",
		},
	} {
		got, err := syntax.Fix(tc.in)
		if err != nil {
			t.Errorf("Fix(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Fix(%q)\ngot  %q\nwant %q", tc.in, got, tc.want)
			continue
		}
		if again, err := syntax.Fix(got); err != nil || again != got {
			t.Errorf("Fix(%q) = %q, %v: not a fixed point", got, again, err)
		}
		g, err := syntax.Parse(got)
		if err != nil {
			t.Errorf("%q does not parse: %v", got, err)
			continue
		}
		if _, err := engine.Compile(starttest.MarkAll(g), engine.Options{}); err != nil {
			t.Errorf("%q does not compile: %v", got, err)
		}
	}
}

// Fix keeps a struct { Text string } that a constructor makes other than from the text a rule
// matches, and structs with other fields.
func TestFixTextStructsKept(t *testing.T) {
	for _, src := range []string{
		// Another constructor makes T.
		"type T = { Text string }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t)}\nrule s = \"s\" => T{Text: \"s\"}",
		// The action is not text($t) of the capture, or the body is not atomic.
		"type T = { Text string }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t) + \"!\"}",
		"type T = { Text string }\nrule r T = t:([a-z]+) #atomic u:\"u\" => T{Text: text($u)}",
		"type T = { Text string }\nrule r T = t:([a-z]+) => T{Text: text($t)}",
		// The rule is declared with another type.
		"type T = { Text string }\ntype U = { Text string }\nrule r U = t:([a-z]+) #atomic => T{Text: text($t)}",
		// The struct has other fields, or its field is not a string.
		"type T = { Text string; N int }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t)}",
		"type T = { Text node[string] }\nrule r T = t:([a-z]+) #atomic => T{Text: $t}",
		// No rule makes T.
		"type T = { Text string }\nrule r = \"r\"",
	} {
		if got, err := syntax.Fix(src); err != nil || got != src {
			t.Errorf("Fix(%q) = %q, %v", src, got, err)
		}
	}
}

// The results of a grammar and of the grammar Fix rewrites have the same text.
func TestFixTextStructsResults(t *testing.T) {
	src := "type T = { Text string }\nrule r T = t:([a-z]+) #atomic => T{Text: text($t)}\nrule main = x:r \",\" y:r => list($x.Text, $y.Text)"
	fixed, err := syntax.Fix(src)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(src string) *engine.Program {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		p, err := engine.Compile(starttest.MarkAll(g), engine.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	before, after := compile(src), compile(fixed)
	for _, input := range []string{"ab,cd", "a,", ""} {
		if got, want := run(after, input), run(before, input); got != want {
			t.Errorf("%q: %s, want %s", input, got, want)
		}
	}
}

// The offsets FixNodeTypes returns for node[_] follow the rewrites of text structs.
func TestFixTextStructsNodeOffsets(t *testing.T) {
	src := "type T = { Text string }\ntype P = { A node; B T }\nrule r = t:([a-z]+) #atomic => T{Text: text($t)}\nrule main = x:r y:\"y\" => P{A: $y, B: $x}"
	got, offsets, err := syntax.FixNodeTypes(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "type T = node[string]\ntype P = { A node[_]; B T }\nrule r T = ([a-z]+) #atomic\nrule main = x:r y:\"y\" => P{A: $y, B: $x}"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if len(offsets) != 1 || !strings.HasPrefix(got[offsets[0]:], "node[_];") {
		t.Errorf("offsets %v in %q", offsets, got)
	}
}
