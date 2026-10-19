package syntax

import (
	"slices"
	"strings"
	"testing"
)

func TestFix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`rule a = "a" -- "b" / "c"`, `rule a = "a" #cut "b" / "c"`},
		{`rule a = "a" --b`, `rule a = "a" #cut b`},
		{`rule a = "a" --("b")`, `rule a = "a" #cut ("b")`},
		{`rule a = "a"--"b"`, `rule a = "a" #cut "b"`},
		{`rule a = ---b`, `rule a = #cut -b`},
		{`rule a = "a" --$$`, `rule a = "a" #cut $$`},
		{"rule a = \"a\" -- \"b\" // \xff", "rule a = \"a\" #cut \"b\" // \xff"},
		{"rule a = \"a\" // c\xff\nrule b = \"\xfe\" -- \"b\"", "rule a = \"a\" // c\xff\nrule b = \"\xfe\" #cut \"b\""},
		{"rule a = \"a\" // \xff\n", "rule a = \"a\" // \xff\n"},
		{"rule a = (\"a\" --\n\t\"b\") // keep -- here\n", "rule a = (\"a\" #cut\n\t\"b\") // keep -- here\n"},
		{`rule a = "--" [-] - -b`, `rule a = "--" [-] - -b`},
		{`rule a = "é" -- "b"`, `rule a = "é" #cut "b"`},
		{"rule a = \"x\"\r\nrule b = \"a\" -- \"b\"", "rule a = \"x\"\r\nrule b = \"a\" #cut \"b\""},
		{"type P struct { A int; B string }", "type P = { A int; B string }"},
		{"type P struct{}", "type P = {}"},
		{"type P struct {\n    A int // a\n}", "type P = {\n    A int // a\n}"},
		{"type U = P | Q\ntype V = (\n    P\n    | Q\n)\ntype P = {}\ntype Q terminal", "type U ~ P | Q\ntype V ~ (\n    P\n    | Q\n)\ntype P = {}\ntype Q = node[string]"},
		// Terminal types become named node types holding the text of the match, or structs
		// holding it where a rule's captures make the text differ from its value.
		{"type N terminal // a number\nrule n N = [0-9]+", "type N = node[string] // a number\nrule n N = ([0-9]+) #atomic"},
		{"type N terminal\nrule n N = (\"-\"? [0-9]+)", "type N = node[string]\nrule n N = (\"-\"? [0-9]+) #atomic"},
		{"type N terminal\nrule n N = [0-9]+ #atomic\nrule m N = ([0-9]+ #atomic)", "type N = node[string]\nrule n N = [0-9]+ #atomic\nrule m N = [0-9]+ #atomic"},
		{"type N terminal\nrule n N = \"a\" / \"b\" => ...", "type N = node[string]\nrule n N = (\"a\" / \"b\") #atomic"},
		{"type N terminal\ntype A ~ N\nrule n A = t:\"a\" t1:\"b\"", "type N = { Text string }\ntype A ~ N\nrule n A = t2:(t:\"a\" t1:\"b\") => N{Text: text($t2)}"},
		{"type N terminal\nrule n N = x:[0-9]+ => N{text($x)}\nrule m terminal = \"m\"", "type N = { Text string }\nrule n N = x:[0-9]+ => N{Text: text($x)}\nrule m node[string] = \"m\""},
		{"type N terminal\nrule n N = [0-9]+\nrule m = x:n y:(n / \"y\") {len($x) > 1} => list(len($y))", "type N = node[string]\nrule n N = ([0-9]+) #atomic\nrule m = x:n y:(n / \"y\") {len($x) > 1} => list(len($y))"},
		{"type N terminal\nrule n N = [0-9]+\nrule m = x:(n / n) {text($x) != \"0\"} y:(n / \"y\") => list(text($x), text($y))", "type N = node[string]\nrule n N = ([0-9]+) #atomic\nrule m = x:(n / n) {text($x) != \"0\"} y:(n / \"y\") => list(text($x), text($y))"},
		// The member Value of a terminal read its text.
		{"type N terminal\nrule n N = [0-9]+\nrule m = x:n y:(n / n) z:(n / \"z\") => list($x.Value, $y.Value, $z.Value)", "type N = node[string]\nrule n N = ([0-9]+) #atomic\nrule m = x:n y:(n / n) z:(n / \"z\") => list(text($x), text($y), $z.Value)"},
		{"type N = { Text string }\ntype A ~ N\ntype P = { Text string; B int }\nrule n A = t:[0-9]+ => N{Text: text($t)}\nrule p P = \"p\" => P{Text: \"p\", B: 1}\nrule m = x:n y:p {$x.Value != \"\"} => list($x.Value, $y.Value, map($x, (v) => v.Value))", "type N = { Text string }\ntype A ~ N\ntype P = { Text string; B int }\nrule n A = t:[0-9]+ => N{Text: text($t)}\nrule p P = \"p\" => P{Text: \"p\", B: 1}\nrule m = x:n y:p {$x.Text != \"\"} => list($x.Text, $y.Value, map($x, (v) => v.Value))"},
		{"type P = { A Match; B []Seq; C *List; D Operator; E Error }\nrule a = x:\"a\" => $x.(Match)", "type P = { A node[string]; B []node[_]; C *node[_]; D node[_]; E error }\nrule a = x:\"a\" => $x.(node[string])"},
		{"type P = { M *Missing }\nrule a = \"a\"", "type P = { M *missing }\nrule a = \"a\""},
		{"type L =[]P\ntype P = {}", "type L ~ []P\ntype P = {}"},
		{"type P = { A int }\ntype U ~ P", "type P = { A int }\ntype U ~ P"},
		{"rule struct_x = \"struct\" // type A struct", "rule struct_x = \"struct\" // type A struct"},
		{"type K terminal\ntype P = {\n  type K\n  struct K\n}", "type K = node[string]\ntype P = {\n  type K\n  struct K\n}"},
		{"type K terminal\ntype P struct { type K; struct K }", "type K = node[string]\ntype P = { type K; struct K }"},
		{`rule a = "a"`, `rule a = "a"`},
	} {
		got, err := Fix(tc.in)
		if err != nil {
			t.Errorf("Fix(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Fix(%q)\ngot  %q\nwant %q", tc.in, got, tc.want)
		}
		if _, err := Parse(got); err != nil {
			t.Errorf("Fix(%q) = %q does not parse: %v", tc.in, got, err)
		}
	}
}

func TestFixLexError(t *testing.T) {
	if _, err := Fix(`rule a = "unterminated`); err == nil {
		t.Fatal("Fix accepted source that does not lex")
	}
}

func TestOldCutRejected(t *testing.T) {
	_, err := Parse(`rule a = "a" -- "b"`)
	if err == nil || !strings.Contains(err.Error(), "#cut") || !strings.Contains(err.Error(), "pego fix") {
		t.Fatalf("got %v, want an error naming #cut and pego fix", err)
	}
}

func TestCutSpelling(t *testing.T) {
	for _, src := range []string{
		`rule a = "a" # cut "b"`,
		`rule a = "a" #cutx "b"`,
		`rule a = "a" #cut("b")`,
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", src)
		}
	}
}

func TestAnyTypes(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want []int
	}{
		{"type B = { V any; L []any; O *any; M _ }", []int{13, 22, 30}},
		{"rule c any = x:\"y\" => $x.(any)", []int{7, 26}},
		{"type any2 = { V any2 }\nrule a = \"any\" // any", nil},
		{"rule a = (", nil},
	} {
		got := AnyTypes(tc.src)
		if !slices.Equal(got, tc.want) {
			t.Errorf("AnyTypes(%q) = %v, want %v", tc.src, got, tc.want)
		}
		for _, o := range got {
			if tc.src[o:o+3] != "any" {
				t.Errorf("AnyTypes(%q): offset %d is not any", tc.src, o)
			}
		}
	}
}
