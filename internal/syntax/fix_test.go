package syntax

import (
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
		{"type U = P | Q\ntype V = (\n    P\n    | Q\n)\ntype P = {}\ntype Q terminal", "type U ~ P | Q\ntype V ~ (\n    P\n    | Q\n)\ntype P = {}\ntype Q terminal"},
		{"type L =[]P\ntype P = {}", "type L ~ []P\ntype P = {}"},
		{"type P = { A int }\ntype U ~ P", "type P = { A int }\ntype U ~ P"},
		{"rule struct_x = \"struct\" // type A struct", "rule struct_x = \"struct\" // type A struct"},
		{"type K terminal\ntype P = {\n  type K\n  struct K\n}", "type K terminal\ntype P = {\n  type K\n  struct K\n}"},
		{"type K terminal\ntype P struct { type K; struct K }", "type K terminal\ntype P = { type K; struct K }"},
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
