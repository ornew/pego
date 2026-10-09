package yaml_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/yaml"
)

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := yaml.Parse(string(data))
		if n != nil {
			got = n.String()
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

// rootScalar parses a document whose root is a scalar.
func rootScalar(t *testing.T, src string) *yaml.Scalar {
	t.Helper()
	s, err := yaml.ParseAST(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	if len(s.Documents) != 1 {
		t.Fatalf("%q: %d documents", src, len(s.Documents))
	}
	sc, ok := s.Documents[0].Root.(*yaml.Scalar)
	if !ok {
		t.Fatalf("%q: root is %T", src, s.Documents[0].Root)
	}
	return sc
}

func TestScalarValue(t *testing.T) {
	for _, tc := range []struct {
		src   string
		style int
		want  string
	}{
		// Plain scalars: line folding (7.3.3, 6.5).
		{"plain", yaml.Plain, "plain"},
		{"a b  c", yaml.Plain, "a b  c"},
		{"a\n b\n\n c\n\n\n d", yaml.Plain, "a b\nc\n\nd"},
		{"a  \t\n  \t b", yaml.Plain, "a b"},
		{"a\r\n b\r\n\r\n c", yaml.Plain, "a b\nc"},
		{"a#b c:d", yaml.Plain, "a#b c:d"},
		{"-1", yaml.Plain, "-1"},
		{"!!str", yaml.Plain, ""},
		// Single-quoted scalars (7.3.2).
		{"'it''s'", yaml.SingleQuoted, "it's"},
		{"''", yaml.SingleQuoted, ""},
		{"' a \\n '", yaml.SingleQuoted, " a \\n "},
		{"' a  \n  b \n\n c '", yaml.SingleQuoted, " a b\nc "},
		{"'''\n'''", yaml.SingleQuoted, "' '"},
		// Double-quoted scalars: escapes (5.7) and folding (7.3.1).
		{`"a\tb\nc\\d\"e\/f"`, yaml.DoubleQuoted, "a\tb\nc\\d\"e/f"},
		{`"\0\a\b\v\f\r\e\ \N\_\L\P"`, yaml.DoubleQuoted, "\x00\a\b\v\f\r\x1b \u0085\u00a0\u2028\u2029"},
		{`"\x41\u263A\U0001F600"`, yaml.DoubleQuoted, "A☺😀"},
		{`"\ud800"`, yaml.DoubleQuoted, "\uFFFD"},
		{"\"a\\\tb\"", yaml.DoubleQuoted, "a\tb"},
		{"\" a \n b \n\n c \"", yaml.DoubleQuoted, " a b\nc "},
		{"\"a \\\n   b\"", yaml.DoubleQuoted, "a b"},
		{"\"a\\\n\n  b\"", yaml.DoubleQuoted, "a\nb"},
		{"\"a\\t\n b\"", yaml.DoubleQuoted, "a\t b"},
		{"\"a \\\n \\ b\"", yaml.DoubleQuoted, "a  b"},
		// Literal block scalars (8.1.2) with chomping (8.1.1.2).
		{"|\n a\n  b\n\n c\n\n", yaml.Literal, "a\n b\n\nc\n"},
		{"|-\n a\n b\n\n", yaml.Literal, "a\nb"},
		{"|+\n a\n b\n\n", yaml.Literal, "a\nb\n\n"},
		{"|\n a", yaml.Literal, "a\n"},
		{"|\n\n\n a\n", yaml.Literal, "\n\na\n"},
		{"|\n", yaml.Literal, ""},
		{"|+\n\n\n", yaml.Literal, "\n\n"},
		// The indentation indicator adds to the indentation of the node, which is -1 at the top level
		// (l-bare-document), so that the content of |2 there is indented by 1.
		{"|2\n   a\n  b\n", yaml.Literal, "  a\n b\n"},
		{"|-1 # comment\n  a\n", yaml.Literal, "  a"},
		{"|\r\n a\r\n b\r\n", yaml.Literal, "a\nb\n"},
		{"|\n a\n# trailing comment\n", yaml.Literal, "a\n"},
		// Folded block scalars (8.1.3).
		{">\n a\n b\n\n c\n", yaml.Folded, "a b\nc\n"},
		{">\n a\n   more\n b\n", yaml.Folded, "a\n  more\nb\n"},
		{">\n a\n\n   more\n\n b\n", yaml.Folded, "a\n\n  more\n\nb\n"},
		{">-\n a\n b\n\n", yaml.Folded, "a b"},
		{">\n \ta\n b\n", yaml.Folded, "\ta\nb\n"},
	} {
		s := rootScalar(t, tc.src)
		if s.Style != tc.style {
			t.Errorf("%q: style %d, want %d", tc.src, s.Style, tc.style)
		}
		if got := s.Value(); got != tc.want {
			t.Errorf("%q: Value() = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestBlockScalarIndent(t *testing.T) {
	for _, tc := range []struct {
		src    string
		indent int
	}{
		{"|\n  a\n", 2},
		{"key: |\n    a\n", 4},
		{"key: |2\n    a\n", 2},
		{"- |1\n  a\n", 1},
		{"--- >\n a\n", 1},
	} {
		s, err := yaml.ParseAST(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		var sc *yaml.Scalar
		switch r := s.Documents[0].Root.(type) {
		case *yaml.Scalar:
			sc = r
		case *yaml.Mapping:
			sc = r.Pairs[0].Value.(*yaml.Scalar)
		case *yaml.Sequence:
			sc = r.Items[0].(*yaml.Scalar)
		}
		if sc.Indent != tc.indent {
			t.Errorf("%q: Indent %d, want %d", tc.src, sc.Indent, tc.indent)
		}
	}
}

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want any
	}{
		{"", nil},
		{"# only a comment\n", nil},
		{"~", nil},
		{"null", nil},
		{"Null", nil},
		{"NULL", nil},
		{"nULL", "nULL"},
		{"true", true},
		{"True", true},
		{"FALSE", false},
		{"yes", "yes"},
		{"0", int64(0)},
		{"-12", int64(-12)},
		{"+12", int64(12)},
		{"0o17", int64(15)},
		{"0x1F", int64(31)},
		{"0x", "0x"},
		{"0b101", "0b101"},
		{"1_000", "1_000"},
		{"9223372036854775807", int64(math.MaxInt64)},
		{"1.5", 1.5},
		{"-.5", -0.5},
		{"1.", 1.0},
		{"1e3", 1000.0},
		{"6.02E+23", 6.02e23},
		{".inf", math.Inf(1)},
		{"-.Inf", math.Inf(-1)},
		{"+.INF", math.Inf(1)},
		{"'1'", "1"},
		{"\"true\"", "true"},
		{"|\n 1\n", "1\n"},
		{"!!str 1", "1"},
		{"! 1", "1"},
		{"!!int '1'", int64(1)},
		{"!!float 1", 1.0},
		{"!!bool true", true},
		{"!!null ''", nil},
		{"!local 1", "1"},
		{"[1, a, [], {}]", []any{int64(1), "a", []any{}, map[string]any{}}},
		{"a: 1\nb:\n  - x\n  - y\n", map[string]any{"a": int64(1), "b": []any{"x", "y"}}},
		{"? a\n: b\n", map[string]any{"a": "b"}},
		{"1: a\ntrue: b\n~: c\n", map[any]any{int64(1): "a", true: "b", nil: "c"}},
		{"a: &x [1]\nb: *x\n", map[string]any{"a": []any{int64(1)}, "b": []any{int64(1)}}},
		{"&a a: *a", map[string]any{"a": "a"}},
		{"--- !!map\na: b\n", map[string]any{"a": "b"}},
	} {
		got, err := yaml.Load(tc.src)
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: Load() = %#v, want %#v", tc.src, got, tc.want)
		}
	}
	nan, err := yaml.Load(".NaN")
	if f, ok := nan.(float64); err != nil || !ok || !math.IsNaN(f) {
		t.Errorf(".NaN: %#v, %v", nan, err)
	}
}

func TestLoadShares(t *testing.T) {
	v, err := yaml.Load("a: &x {k: 1}\nb: *x\n")
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	m["a"].(map[string]any)["k"] = 2
	if m["b"].(map[string]any)["k"] != 2 {
		t.Error("an alias does not share the value of its anchor")
	}
}

func TestLoadAll(t *testing.T) {
	got, err := yaml.LoadAll("a\n--- b\n...\n%YAML 1.2\n---\n- c\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"a", "b", []any{"c"}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadAll = %#v, want %#v", got, want)
	}
	if _, err := yaml.Load("a\n---\nb\n"); err == nil {
		t.Error("Load accepted two documents")
	}
}

func TestLoadErrors(t *testing.T) {
	for _, src := range []string{
		"a: 1\na: 2\n",                    // duplicate key
		"1: a\n+1: b\n",                   // duplicate key after resolution
		"? [a]\n: b\n",                    // collection as a key
		"a: *b",                           // undefined alias
		"&a [*a]",                         // alias inside its node
		"!!int a",                         // value does not match the tag
		"!!bool 1",                        // value does not match the tag
		"!!map a",                         // scalar with !!map
		"!!seq {a: b}",                    // mapping with !!seq
		"!!str [a]",                       // sequence with a scalar tag
		"99999999999999999999",            // int out of range
		"!e!a b",                          // undeclared handle
		"%YAML 1.2\n%YAML 1.2\n--- a\n",   // two YAML directives
		"%TAG !a! x\n%TAG !a! y\n--- a\n", // two TAG directives for a handle
		"%YAML 2.0\n--- a\n",              // major version
		"a: \xff",                         // invalid UTF-8
		"[a",                              // syntax error
	} {
		if _, err := yaml.LoadAll(src); err == nil {
			t.Errorf("%q: LoadAll accepted", src)
		}
	}
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		src string
		ok  bool
	}{
		{"a: *b", false},
		{"&b a: *b", true},
		{"--- &b a\n--- *b\n", false}, // anchors are per document
		{"!e!a b", false},
		{"%TAG !e! tag:example.com,2000:\n--- !e!a b\n", true},
		{"%TAG !e! tag:example.com,2000:\n--- !e!a b\n--- !e!a b\n", false},
		{"%YAML 1.1\n--- a\n", true},
		{"%YAML 1.3\n--- a\n", true},
		{"%YAML 2.0\n--- a\n", false},
		{"%FOO bar\n--- a\n", true},
	} {
		s, err := yaml.ParseAST(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		err = s.Check()
		if (err == nil) != tc.ok {
			t.Errorf("%q: Check() = %v", tc.src, err)
		}
		if yaml.Valid(tc.src) != tc.ok {
			t.Errorf("%q: Valid() = %v", tc.src, !tc.ok)
		}
		var se *yaml.SemanticError
		if err != nil && !errors.As(err, &se) {
			t.Errorf("%q: %T is not a *SemanticError", tc.src, err)
		}
	}
}

func TestSemanticErrorPosition(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"a: 1\nb: *x\n", "yaml: 2:4: undefined alias *x"},
		{"é: 1\né: 2\n", `yaml: 2:1: duplicate key "é"`},
		{"- !e!x a\n", "yaml: 1:3: undeclared tag handle !e!"},
	} {
		_, err := yaml.LoadAll(tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q: %v, want %s", tc.src, err, tc.want)
		}
	}
}

func TestEvents(t *testing.T) {
	got, err := yaml.Events("%TAG !e! tag:e.com,2000:\n--- !e!m\n- &a 'x'\n- *a\n- {k: v}\n- |\n  w\n...\n")
	if err != nil {
		t.Fatal(err)
	}
	want := `+STR
+DOC ---
+SEQ <tag:e.com,2000:m>
=VAL &a 'x
=ALI *a
+MAP {}
=VAL :k
=VAL :v
-MAP
=VAL |w\n
-SEQ
-DOC ...
-STR
`
	if got != want {
		t.Errorf("Events:\n%s\nwant:\n%s", got, want)
	}
}

func TestInvalidUTF8(t *testing.T) {
	src := "a: \xff\xfe"
	if yaml.Valid(src) {
		t.Error("Valid accepted invalid UTF-8")
	}
	if _, err := yaml.Events(src); err == nil || !strings.Contains(err.Error(), "byte 3") {
		t.Errorf("Events: %v", err)
	}
	// The parser reads invalid bytes as U+FFFD, a printable character.
	if err := yaml.Recognize(src); err != nil {
		t.Errorf("Recognize: %v", err)
	}
}

// TestDeepNesting checks that nesting up to the depth limit parses and that deeper nesting is an error,
// not a stack overflow.
func TestDeepNesting(t *testing.T) {
	for _, tc := range []struct {
		depth int
		ok    bool
	}{{1000, true}, {10000, true}, {100000, false}, {1000000, false}} {
		src := strings.Repeat("[", tc.depth) + strings.Repeat("]", tc.depth)
		if _, err := yaml.ParseAST(src); (err == nil) != tc.ok {
			t.Errorf("depth %d: ParseAST: %v", tc.depth, err)
		}
		if err := yaml.Recognize(src); (err == nil) != tc.ok {
			t.Errorf("depth %d: Recognize: %v", tc.depth, err)
		}
	}
	if _, err := yaml.ParseAST(strings.Repeat("{a: ", 5000) + strings.Repeat("}", 5000)); err != nil {
		t.Errorf("5000 nested flow mappings: %v", err)
	}
	var b strings.Builder
	for i := range 2000 {
		b.WriteString(strings.Repeat(" ", i))
		b.WriteString("a:\n")
	}
	if _, err := yaml.Load(b.String()); err != nil {
		t.Errorf("2000 nested block mappings: %v", err)
	}
	// Long runs of lines are repetitions, not recursion.
	for _, src := range []string{
		"a: |\n" + strings.Repeat("\n", 200000) + "b: 1\n",
		"a: |\n" + strings.Repeat("  \n", 200000) + "  x\n",
		"a: b" + strings.Repeat("\n  c", 200000) + "\n",
		"\"" + strings.Repeat("a\n", 200000) + "\"",
		strings.Repeat("- a\n", 200000),
	} {
		if _, err := yaml.ParseAST(src); err != nil {
			t.Errorf("%.20q...: %v", src, err)
		}
	}
}

// FuzzParse checks on arbitrary input that ParseAST and Recognize agree, that what Valid accepts has
// events and values, and that nothing panics.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"a: [b, {c: d}]\n", "- |\n x\n- 'y'\n", "--- !!str &a \"z\"\n...\n", "? a\n: *a\n", "a:\n\tb"} {
		f.Add(s)
	}
	for _, s := range []string{"%TAG\n---\n", "%YAML\n--- x\n", "%TAG !e!\n--- x\n", "%YAML 1.foo\n--- x\n", "%TAG notahandle tag:x\n--- x\n", "%YAMLfoo\n--- x\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		s, perr := yaml.ParseAST(src)
		if rerr := yaml.Recognize(src); (perr == nil) != (rerr == nil) {
			t.Fatalf("%q: ParseAST: %v, Recognize: %v", src, perr, rerr)
		}
		_, eerr := yaml.Events(src)
		if yaml.Valid(src) != (eerr == nil) {
			t.Fatalf("%q: Valid and Events disagree: %v", src, eerr)
		}
		if perr == nil {
			if err := s.Check(); (utf8.ValidString(src) && err == nil) != yaml.Valid(src) {
				t.Fatalf("%q: Check and Valid disagree: %v", src, err)
			}
		}
		yaml.Load(src)
		yaml.LoadAll(src)
	})
}
