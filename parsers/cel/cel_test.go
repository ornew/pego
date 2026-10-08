package cel_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/cel"
)

// textprotoExprs returns the value of each expr field in a file of the protobuf text format. A string value may be
// several string literals on several lines, which the format concatenates.
func textprotoExprs(t testing.TB, data string) []string {
	t.Helper()
	var out []string
	rest := data
	for {
		i := strings.Index(rest, "expr:")
		if i < 0 {
			return out
		}
		// A field name: at the start of a line, after spaces.
		line := strings.LastIndex(rest[:i], "\n") + 1
		if strings.TrimSpace(rest[line:i]) != "" {
			rest = rest[i+len("expr:"):]
			continue
		}
		rest = rest[i+len("expr:"):]
		var val strings.Builder
		for {
			rest = strings.TrimLeft(rest, " \t\r\n")
			for strings.HasPrefix(rest, "#") { // a comment between the literals
				_, rest, _ = strings.Cut(rest, "\n")
				rest = strings.TrimLeft(rest, " \t\r\n")
			}
			if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
				break
			}
			q := rest[0]
			end := 1
			for rest[end] != q {
				if rest[end] == '\\' {
					end++
				}
				end++
			}
			val.WriteString(textprotoString(t, rest[1:end]))
			rest = rest[end+1:]
		}
		out = append(out, val.String())
	}
}

// textprotoString decodes the escapes of the body of a string literal of the protobuf text format.
func textprotoString(t testing.TB, s string) string {
	t.Helper()
	if !strings.Contains(s, `\`) {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b = append(b, s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'a':
			b = append(b, '\a')
		case 'b':
			b = append(b, '\b')
		case 'f':
			b = append(b, '\f')
		case 'v':
			b = append(b, '\v')
		case '\\', '\'', '"', '?':
			b = append(b, c)
		case 'x', 'X':
			j := i + 1
			for j < len(s) && j < i+3 && strings.IndexByte("0123456789abcdefABCDEF", s[j]) >= 0 {
				j++
			}
			n, err := strconv.ParseUint(s[i+1:j], 16, 8)
			if err != nil {
				t.Fatalf("bad escape in %q", s)
			}
			b = append(b, byte(n))
			i = j - 1
		case 'u', 'U':
			n := 4
			if c == 'U' {
				n = 8
			}
			v, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			if err != nil {
				t.Fatalf("bad escape in %q", s)
			}
			b = utf8.AppendRune(b, rune(v))
			i += n
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			n, _ := strconv.ParseUint(s[i:j], 8, 16)
			b = append(b, byte(n))
			i = j - 1
		default:
			t.Fatalf("bad escape \\%c in %q", c, s)
		}
	}
	return string(b)
}

// TestConformanceSuite parses the expression of every test of the simple conformance suite of cel-spec
// (testdata/cel-spec), all of which are valid, and checks the typed values, the nodes and the recognizer agree.
func TestConformanceSuite(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "cel-spec", "*.textproto"))
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	total, distinct := 0, map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		exprs := textprotoExprs(t, string(data))
		n := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "expr:") {
				n++
			}
		}
		if n != len(exprs) {
			t.Errorf("%s: %d expr fields, read %d", f, n, len(exprs))
		}
		for _, src := range exprs {
			total++
			distinct[src] = true
			if _, err := cel.ParseExpr(src); err != nil {
				t.Errorf("%s: %q: %v", filepath.Base(f), src, err)
			}
			if err := cel.Recognize(src); err != nil {
				t.Errorf("%s: %q: Recognize: %v", filepath.Base(f), src, err)
			}
			if _, err := cel.Parse(src); err != nil {
				t.Errorf("%s: %q: Parse: %v", filepath.Base(f), src, err)
			}
		}
	}
	t.Logf("%d tests, %d different expressions", total, len(distinct))
	if total != 2527 || len(distinct) != 2263 {
		t.Errorf("read %d tests with %d different expressions, want 2527 and 2263", total, len(distinct))
	}
}

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
		n, err := cel.Parse(string(data))
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

// refSources returns the expressions of every file of testdata/ref except limits.tsv.
func refSources(t testing.TB) []string {
	files, _ := filepath.Glob(filepath.Join("testdata", "ref", "*.tsv"))
	var out []string
	for _, f := range files {
		if name := filepath.Base(f); name != "limits.tsv" {
			for _, c := range readRef(t, name) {
				out = append(out, c.src)
			}
		}
	}
	return out
}

// TestAgreement checks that ParseAST, Parse and Recognize accept the same inputs, in both position units.
func TestAgreement(t *testing.T) {
	for _, src := range refSources(t) {
		_, aerr := cel.ParseAST(src)
		_, nerr := cel.Parse(src)
		rerr := cel.Recognize(src)
		_, berr := cel.ParseAST(src, cel.Bytes)
		if (aerr == nil) != (nerr == nil) || (aerr == nil) != (rerr == nil) || (aerr == nil) != (berr == nil) {
			t.Fatalf("%q: ParseAST: %v, Parse: %v, Recognize: %v, ParseAST in bytes: %v", src, aerr, nerr, rerr, berr)
		}
		if aerr != nil && aerr.Error() != rerr.Error() {
			t.Errorf("%q: errors differ: %v, %v", src, aerr, rerr)
		}
	}
}

// checkSpans checks the invariants of the spans of an expression: they lie in the input, a terminal's span is its
// text, and the spans of the sub-expressions of a node lie in the node's, in order and without overlapping.
func checkSpans(t testing.TB, src string, e cel.Expr, bytes bool) {
	t.Helper()
	text := []rune(src)
	slice := func(sp cel.Span) string {
		if bytes {
			return src[sp.Start:sp.End]
		}
		return string(text[sp.Start:sp.End])
	}
	size := len(text)
	if bytes {
		size = len(src)
	}
	var walk func(e cel.Expr, parent cel.Span, after int) int
	walk = func(e cel.Expr, parent cel.Span, after int) int {
		sp := cel.SpanOf(e)
		if sp.Start < parent.Start || sp.End > parent.End || sp.Start < after || sp.End < sp.Start || sp.End > size {
			t.Fatalf("%q: %T has span %v, outside %v or before %d", src, e, sp, parent, after)
		}
		switch e := e.(type) {
		case *cel.Ident:
			if slice(sp) != e.Text {
				t.Fatalf("%q: %T span %v is %q, text %q", src, e, sp, slice(sp), e.Text)
			}
		case *cel.IntLit:
			if slice(sp) != e.Text {
				t.Fatalf("%q: %T span %v is %q, text %q", src, e, sp, slice(sp), e.Text)
			}
		case *cel.StringLit:
			if slice(sp) != e.Text {
				t.Fatalf("%q: %T span %v is %q, text %q", src, e, sp, slice(sp), e.Text)
			}
		}
		end := sp.Start
		cel.Children(e, func(c cel.Expr) { end = walk(c, sp, end) })
		return sp.End
	}
	walk(e, cel.Span{Start: 0, End: size}, 0)
}

// TestSpans checks the spans of every expression of the reference corpora, in both position units.
func TestSpans(t *testing.T) {
	for _, src := range refSources(t) {
		if e, err := cel.ParseAST(src); err == nil {
			checkSpans(t, src, e, false)
		}
		if e, err := cel.ParseAST(src, cel.Bytes); err == nil {
			checkSpans(t, src, e, true)
		}
	}
}

// TestValues checks the decoding of literals against the examples of the specification (langdef.md) and edge cases.
func TestValues(t *testing.T) {
	str := func(src string) string {
		t.Helper()
		e, err := cel.ParseExpr(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return e.(*cel.StringLit).Value()
	}
	for _, tc := range []struct{ src, want string }{
		{`""`, ""},
		{`'""'`, `""`},
		{`'''x''x'''`, `x''x`},
		{`"\""`, `"`},
		{`"\\"`, `\`},
		{`r"\\"`, `\\`},
		{`"\303\277"`, "Ã¿"},
		{`"\377"`, "ÿ"},
		{`"\xFF"`, "ÿ"},
		{`"☺\U0001F600"`, "☺\U0001f600"},
		{`"\a\b\f\n\r\t\v\?\"\'` + "\\`" + `"`, "\a\b\f\n\r\t\v?\"'`"},
		{"'''a\r\nb\rc'''", "a\nb\nc"},
		{"r'''a\r\nb'''", "a\nb"},
		{`R"\n"`, `\n`},
	} {
		if got := str(tc.src); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
	bytesOf := func(src string) string {
		t.Helper()
		e, err := cel.ParseExpr(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return string(e.(*cel.BytesLit).Value())
	}
	for _, tc := range []struct{ src, want string }{
		{`b"abc"`, "abc"},
		{`b"ÿ"`, "\xc3\xbf"},
		{`b"\303\277"`, "\xc3\xbf"},
		{`b"\377"`, "\xff"},
		{`b"\xff"`, "\xff"},
		{`B'''"Kim\t"'''`, "\"Kim\t\""},
		{`br"\xff"`, `\xff`},
	} {
		if got := bytesOf(tc.src); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
	bs := `\`
	for _, src := range []string{`"\s"`, `"` + bs + `u2FE0` + bs + `ud800"`, `"` + bs + `uD83D` + bs + `uDE03"`, `"\UD83DDE03"`, `"\U00110000"`, `"\400"`, `"\x4"`,
		`b"` + bs + `u00ff"`, `b"\U000000ff"`, `"\` + `"`, `'''\x'''`} {
		if _, err := cel.ParseExpr(src); err == nil {
			t.Errorf("%s was accepted", src)
		}
	}
	// A code point that is valid but unassigned.
	if got := str(`"⿠"`); got != "⿠" {
		t.Errorf(`"⿠" = %q`, got)
	}

	ints := func(src string) (int64, error) {
		e, err := cel.ParseExpr(src)
		if err != nil {
			return 0, err
		}
		return e.(*cel.IntLit).Value()
	}
	for _, tc := range []struct {
		src  string
		want int64
	}{{"0", 0}, {"-0", 0}, {"42", 42}, {"0x1F", 31}, {"-0x1F", -31}, {"- 5", -5}, {"-//c\n5", -5}, {"007", 7},
		{"9223372036854775807", 1<<63 - 1}, {"-9223372036854775808", -1 << 63}, {"-0x8000000000000000", -1 << 63}} {
		if got, err := ints(tc.src); err != nil || got != tc.want {
			t.Errorf("%s = %d, %v; want %d", tc.src, got, err, tc.want)
		}
	}
	for _, src := range []string{"9223372036854775808", "-9223372036854775809", "0x8000000000000000", "0x10000000000000000"} {
		if _, err := ints(src); err == nil {
			t.Errorf("%s was accepted", src)
		}
	}
	uints := func(src string) (uint64, error) {
		e, err := cel.ParseExpr(src)
		if err != nil {
			return 0, err
		}
		return e.(*cel.UintLit).Value()
	}
	if got, err := uints("18446744073709551615u"); err != nil || got != 1<<64-1 {
		t.Errorf("max uint = %d, %v", got, err)
	}
	for _, src := range []string{"18446744073709551616u", "0x10000000000000000U"} {
		if _, err := uints(src); err == nil {
			t.Errorf("%s was accepted", src)
		}
	}
	doubles := func(src string) (float64, error) {
		e, err := cel.ParseExpr(src)
		if err != nil {
			return 0, err
		}
		return e.(*cel.DoubleLit).Value()
	}
	for _, tc := range []struct {
		src  string
		want float64
	}{{"1.5", 1.5}, {".5", .5}, {"-.5", -.5}, {"- 1e3", -1000}, {"1E-2", .01}, {"1e-400", 0}, {"1.7976931348623157e308", 1.7976931348623157e308}} {
		if got, err := doubles(tc.src); err != nil || got != tc.want {
			t.Errorf("%s = %v, %v; want %v", tc.src, got, err, tc.want)
		}
	}
	for _, src := range []string{"1e309", "-1e309"} {
		if _, err := doubles(src); err == nil {
			t.Errorf("%s was accepted", src)
		}
	}
}

// TestNames checks the accessors of names.
func TestNames(t *testing.T) {
	e, err := cel.ParseExpr("a . `b c` . d")
	if err != nil {
		t.Fatal(err)
	}
	d := e.(*cel.Select)
	bc := d.X.(*cel.Select)
	if d.Field.Value() != "d" || bc.Field.Value() != "b c" {
		t.Errorf("fields %q, %q", bc.Field.Value(), d.Field.Value())
	}
	e, err = cel.ParseExpr(". // c\n a")
	if err != nil {
		t.Fatal(err)
	}
	if id := e.(*cel.Ident); id.Name() != "a" || !id.Rooted() {
		t.Errorf("ident %q rooted %v", id.Name(), id.Rooted())
	}
	e, err = cel.ParseExpr(".a . b.C {f: 1}")
	if err != nil {
		t.Fatal(err)
	}
	if id := e.(*cel.MessageLit).Type; id.Name() != "a.b.C" || !id.Rooted() {
		t.Errorf("type %q rooted %v", id.Name(), id.Rooted())
	}
}

func ExampleParseExpr() {
	e, err := cel.ParseExpr(`user.age >= 18 && "admin" in user.roles`)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%T\n", e)
	// Output: *cel.Binary
}
