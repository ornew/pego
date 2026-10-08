package cue

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// TestCheck: inputs that the grammar accepts and Check (so ParseFile) rejects, as the reference parser does; the
// message is that of the reference parser.
func TestCheck(t *testing.T) {
	for _, c := range []struct{ in, msg string }{
		{"a: 1\na: 2\nX=a: 3\nX=b: 4\n", `alias "X" redeclared in same scope`},
		{"foo: bar: 1\nlet x = 1\nlet x = 2\n", `alias "x" redeclared in same scope`},
		{"import \"a b\"\n", `invalid import path: "a b"`},
		{"x: {a: 1}\n[a, b]: int\n", "square bracket must have exactly one element"},
		{"[]: int\n", "square bracket must have exactly one element"},
		{"x: \"\"\"\n  a\\(b)\n c\n  \"\"\"\n", "non-matching whitespace for multiline string"},
		{"a~X: 1\n", "postfix alias syntax requires @experiment(aliasv2)"},
	} {
		_, aerr := ParseAST(c.in)
		_, ferr := ParseFile(c.in)
		if aerr != nil {
			t.Errorf("%q: ParseAST: %v", c.in, aerr)
		}
		var se *SemanticError
		if !errors.As(ferr, &se) || !strings.Contains(se.Msg, c.msg) {
			t.Errorf("%q: ParseFile: %v, want %q", c.in, ferr, c.msg)
		} else if se.Line < 1 || se.Col < 1 {
			t.Errorf("%q: no position: %+v", c.in, se)
		}
		if Valid(c.in) {
			t.Errorf("%q: Valid", c.in)
		}
	}
}

// TestExperiments: what each experiment enables, in the file that declares it.
func TestExperiments(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"x: 1\n", true},
		{"@experiment(try)\nx: 1\n", true},
		{"a~X: 1\n", false},
		{"@experiment(aliasv2)\na~X: 1\n", true},
		{"@experiment(aliasv2)\n\na~(K, V): 1\n", true},
		{"@experiment(try,aliasv2)\na~X: 1\n", true},
		{"@experiment(explicitopen)\nx: 1\n", true},
		{"@experiment(structcmp)\nx: 1\n", true},
		{"@experiment(unknown)\nx: 1\n", false},
		{"x: 1 @experiment(try)\n", true},
	} {
		if got := Valid(c.in); got != c.ok {
			t.Errorf("%q: Valid = %v, want %v", c.in, got, c.ok)
		}
	}
}

// TestAttributeStrings: a string in an attribute closes with exactly the hashes that opened it; the rest is a
// token of the attribute (cue/scanner, found by the mutants of the corpus).
func TestAttributeStrings(t *testing.T) {
	for _, in := range []string{
		"x: 1 @a(#\"x\"##)\n",
		"x: 1 @a(#\"\"\"\n s\n\"\"\"##y)\n",
		"x: 1 @a(##\"x\"###, \"y\")\n",
	} {
		f, err := ParseFile(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		a := f.Decls[0].(*Field).Attrs
		if len(a) != 1 || a[0].End != len([]rune(strings.TrimSuffix(in, "\n"))) {
			t.Errorf("%q: attribute %+v", in, a)
		}
	}
	// A line goes on after an interpolation that has line breaks; an empty line needs no indentation.
	for _, in := range []string{
		"x: \"\"\"\n  a\\(b\n )\n  c\n  \"\"\"\n",
		"x: \"\"\"\n  a\\(b)\n\n  c\n  \"\"\"\n",
	} {
		if !Valid(in) {
			t.Errorf("%q is not valid", in)
		}
	}
	if Valid("x: 1 @a(#\"x\"") {
		t.Errorf("an unterminated string in an attribute is valid")
	}
}

func TestInspect(t *testing.T) {
	f, err := ParseFile("package p\nimport \"strings\"\na: b + c.d[e]\nf: {for k, v in a if k != \"z\" {(k): v}}\n")
	if err != nil {
		t.Fatal(err)
	}
	var idents []string
	depth, max := 0, 0
	Inspect(f, func(n any) bool {
		if n == nil {
			depth--
			return true
		}
		depth++
		max = max2(max, depth)
		if id, ok := n.(*Ident); ok {
			idents = append(idents, id.Text)
		}
		return true
	})
	want := []string{"p", "a", "b", "c", "d", "e", "f", "k", "v", "a", "k", "k", "v"}
	if !reflect.DeepEqual(idents, want) {
		t.Errorf("identifiers %v, want %v", idents, want)
	}
	if depth != 0 {
		t.Errorf("unbalanced calls: %d", depth)
	}
	if max < 5 {
		t.Errorf("depth %d", max)
	}
	// f returning false prunes the subtree.
	n := 0
	Inspect(f, func(x any) bool {
		if x != nil {
			n++
		}
		_, isField := x.(*Field)
		return !isField
	})
	if n != 8 { // the file; the package and its name; the import, its spec and its path; the two fields
		t.Errorf("visited %d nodes", n)
	}
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestSpans(t *testing.T) {
	src := "a: \"é\" // é\nb: [1, 2]\n"
	f, err := ParseFile(src)
	if err != nil {
		t.Fatal(err)
	}
	b := f.Decls[1].(*Field)
	if got := string([]rune(src)[b.Start:b.End]); got != "b: [1, 2]" {
		t.Errorf("code point span %q", got)
	}
	g, err := ParseFile(src, Bytes)
	if err != nil {
		t.Fatal(err)
	}
	b = g.Decls[1].(*Field)
	if got := src[b.Start:b.End]; got != "b: [1, 2]" {
		t.Errorf("byte span %q", got)
	}
	if SpanOf(b.Value) != b.Value.(*ListLit).Span {
		t.Errorf("SpanOf")
	}
	cs := Comments(src, Bytes)
	if len(cs) != 1 || cs[0].Text != "// é" || src[cs[0].Start:cs[0].End] != "// é" {
		t.Errorf("comments %+v", cs)
	}
}

func TestComments(t *testing.T) {
	src := "// a\npackage p // b\n\nx: \"// not\" // c\ny: #\"\"\"\n // not\n \"\"\"# // d\nz: 1 @a(\"// not\", // e\n)\n/// f\n"
	var got []string
	for _, c := range Comments(src) {
		got = append(got, c.Text)
		if r := []rune(src); string(r[c.Start:c.End]) != c.Text {
			t.Errorf("span of %q: %q", c.Text, string(r[c.Start:c.End]))
		}
	}
	// CUE has no block comments, and the text of strings and attributes is not a comment (nor is one inside an
	// attribute: the scanner of the reference skips it).
	want := []string{"// a", "// b", "// c", "// d", "/// f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
}

func TestIdent(t *testing.T) {
	for _, c := range []struct {
		text        string
		def, hidden bool
	}{
		{"a", false, false},
		{"_", false, false},
		{"_a", false, true},
		{"#D", true, false},
		{"_#D", true, true},
		{"$x", false, false},
	} {
		id := &Ident{Text: c.text}
		if id.IsDefinition() != c.def || id.IsHidden() != c.hidden {
			t.Errorf("%s: definition %v, hidden %v", c.text, id.IsDefinition(), id.IsHidden())
		}
	}
}

func TestAttributeSplit(t *testing.T) {
	for _, c := range []struct{ in, name, body string }{
		{"@go(Name)", "go", "Name"},
		{"@json(name,omitempty)", "json", "name,omitempty"},
		{"@a()", "a", ""},
		{"@a(b(c), \"d)\")", "a", `b(c), "d)"`},
	} {
		name, body := (&Attribute{Text: c.in}).Split()
		if name != c.name || body != c.body {
			t.Errorf("%s: %q, %q", c.in, name, body)
		}
	}
}

func TestUnquote(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`"a"`, "a"},
		{`""`, ""},
		{`"\n\t\\\""`, "\n\t\\\""},
		{`"\u00e9\U0001F600\u0041"`, "é\U0001F600A"},
		{`'\xff'`, "\xff"},
		{`#"a\nb"#`, `a\nb`},
		{`#"a\#nb"#`, "a\nb"},
		{`##"a\#nb\##tc"##`, "a\\#nb\tc"},
		{"\"\"\"\n\ta\n\t b\n\t\"\"\"", "a\n b"},
		{"#\"\"\"\n  a\\n\n  \"\"\"#", "a\\n"},
		{"'''\n\tab\n\t'''", "ab"},
	} {
		f, err := ParseFile("x: " + c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		got, err := f.Decls[0].(*Field).Value.(*String).Unquote()
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v, want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{`"\q"`, `"\u12"`, `"\ud800"`, `'\u00e9\400'`, "\"\"\"\n\ta\n b\n\t\"\"\""} {
		f, err := ParseAST("x: " + in)
		if err != nil {
			continue // a syntax error is an error too
		}
		if s, ok := f.Decls[0].(*Field).Value.(*String); ok {
			if got, err := s.Unquote(); err == nil {
				t.Errorf("%s: %q, want an error", in, got)
			}
		}
	}
}

func TestNumbers(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"12_345", "12345"},
		{"0x1F", "31"},
		{"0b101", "5"},
		{"0o17", "15"},
		{"1K", "1000"},
		{"1Ki", "1024"},
		{"2M", "2000000"},
		{"1Gi", "1073741824"},
		{"1.5K", "1500"},
		{"0.5Ki", "512"},
		{"1P", "1000000000000000"},
	} {
		f, err := ParseFile("x: " + c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		n, ok := f.Decls[0].(*Field).Value.(*Int)
		if !ok {
			t.Errorf("%s: %T", c.in, f.Decls[0].(*Field).Value)
			continue
		}
		v, err := n.Value()
		if err != nil || v.String() != c.want {
			t.Errorf("%s: %v, %v, want %s", c.in, v, err, c.want)
		}
	}
	f, _ := ParseFile("x: 1.3Ki")
	if _, err := f.Decls[0].(*Field).Value.(*Int).Value(); err == nil {
		t.Errorf("1.3Ki: want an error: 1331.2 is not an integer")
	}
	f, _ = ParseFile("x: 9223372036854775808")
	if _, err := f.Decls[0].(*Field).Value.(*Int).Int64(); err == nil {
		t.Errorf("2^63: want an error from Int64")
	}
	for _, c := range []struct {
		in   string
		want float64
	}{{"1.5", 1.5}, {".5", .5}, {"1e3", 1000}, {"1_0.2_5", 10.25}, {"1E-2", .01}, {"2.", 2}} {
		f, err := ParseFile("x: " + c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		fl, ok := f.Decls[0].(*Field).Value.(*Float)
		if !ok {
			t.Errorf("%s: %T", c.in, f.Decls[0].(*Field).Value)
			continue
		}
		if v, err := fl.Float64(); err != nil || v != c.want {
			t.Errorf("%s: %v, %v", c.in, v, err)
		}
		if r, err := fl.Rat(); err != nil {
			t.Errorf("%s: %v", c.in, err)
		} else if v, _ := r.Float64(); v != c.want {
			t.Errorf("%s: rat %v", c.in, r)
		}
	}
}

func TestInvalidUTF8(t *testing.T) {
	_, err := ParseFile("x: \"a\xffb\"\n")
	var se *SemanticError
	if !errors.As(err, &se) || !strings.Contains(se.Msg, "UTF-8") || se.Line != 1 {
		t.Errorf("error %v", err)
	}
	if _, err := ParseAST("x: 1\xff\n"); err == nil {
		t.Errorf("ParseAST accepted invalid UTF-8 outside a string")
	}
}

func TestSyntaxErrors(t *testing.T) {
	_, err := ParseAST("a: 1\nb: [1, 2\nc: 3\n")
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("error %T %v", err, err)
	}
	if se.Line < 2 {
		t.Errorf("line %d: %v", se.Line, err)
	}
	for _, in := range []string{"a: 1 b: 2", "a: (", "a: <-b", "a: 'x", "a: 1..2", "a: \"\\(\"", "{", "a: }", "x: [1 2]"} {
		if Valid(in) {
			t.Errorf("%q is valid", in)
		}
		if err := Recognize(in); err == nil {
			t.Errorf("%q recognized", in)
		}
	}
}

func TestDepth(t *testing.T) {
	for _, c := range []struct{ open, mid, close string }{
		{"[", "1", "]"},
		{"{a: ", "1", "}"},
		{"(", "1", ")"},
		{"-", "1", ""},
		{"a & (", "b", ")"},
	} {
		n := 2000
		in := "x: " + strings.Repeat(c.open, n) + c.mid + strings.Repeat(c.close, n) + "\n"
		if !Valid(in) {
			t.Errorf("%q nested %d deep is not valid", c.open, n)
		}
	}
}

// TestUnicodeTables: the letters and digits above ASCII that identifiers can contain are those of unicode.IsLetter
// and unicode.IsDigit (cue/scanner), as of the version of the unicode package that the grammar was made with
// (internal/unitab prints the tables). The test checks the edges of every range of the tables of package unicode,
// and a point inside; CUE_UNICODE_ALL=1 checks every code point (about 25 s). If it fails after an upgrade of Go,
// regenerate the tables.
func TestUnicodeTables(t *testing.T) {
	var runes []rune
	if os.Getenv("CUE_UNICODE_ALL") != "" {
		for r := rune(0x80); r <= unicode.MaxRune; r++ {
			runes = append(runes, r)
		}
	} else {
		for _, tab := range []*unicode.RangeTable{unicode.Letter, unicode.Digit} {
			for _, r := range tab.R16 {
				runes = append(runes, rune(r.Lo)-1, rune(r.Lo), rune(r.Lo+r.Stride), rune(r.Lo)+rune(r.Hi-r.Lo)/2, rune(r.Hi), rune(r.Hi)+1)
			}
			for _, r := range tab.R32 {
				runes = append(runes, rune(r.Lo)-1, rune(r.Lo), rune(r.Lo+r.Stride), rune(r.Lo)+rune(r.Hi-r.Lo)/2, rune(r.Hi), rune(r.Hi)+1)
			}
		}
	}
	var bad []rune
	for _, r := range runes {
		if r < 0x80 || r > unicode.MaxRune || r >= 0xd800 && r < 0xe000 || r == 0xfeff { // U+FEFF is a byte order mark
			continue
		}
		start := Recognize(string(r)) == nil
		inside := Recognize("a"+string(r)) == nil
		if start != unicode.IsLetter(r) || inside != (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if bad = append(bad, r); len(bad) > 10 {
				break
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("identifier characters differ from package unicode (%s) at %U", unicode.Version, bad)
	}
}
