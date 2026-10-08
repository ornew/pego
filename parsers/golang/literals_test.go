package golang_test

import (
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

func TestLiterals(t *testing.T) {
	file, err := golang.ParseAST("package p; var _ = []any{\"a\\tb\\u00e9\", `r\\n`, 'x', '\\n', '\\xff', '\\u00e9', "+
		"0, 0x1F, 0b101, 0o17, 017, 1_000, 123456789012345678901234567890, 1.5, 0x1p-2, .5e3, 1e400, 2i, 1.5i}", golang.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	elts := file.Decls[0].(*golang.GenDecl).Specs[0].(*golang.ValueSpec).Values[0].(*golang.CompositeLit).Elts
	strs := []string{"a\tbé", "r\\n"}
	for i, want := range strs {
		if got, err := elts[i].(*golang.StringLit).Value(); err != nil || got != want {
			t.Errorf("string %d: %q, %v; want %q", i, got, err, want)
		}
	}
	runes := []rune{'x', '\n', 0xff, 0xe9}
	for i, want := range runes {
		if got, err := elts[2+i].(*golang.CharLit).Value(); err != nil || got != want {
			t.Errorf("char %d: %q, %v; want %q", i, got, err, want)
		}
	}
	ints := []string{"0", "31", "5", "15", "15", "1000", "123456789012345678901234567890"}
	for i, want := range ints {
		v, err := elts[6+i].(*golang.IntLit).Constant()
		if err != nil || v.ExactString() != want {
			t.Errorf("int %d: %v, %v; want %s", i, v, err, want)
		}
	}
	floats := []string{"3/2", "1/4", "500", "1e+400"} // the last as String, since ExactString spells out the digits
	for i, want := range floats {
		v, err := elts[13+i].(*golang.FloatLit).Constant()
		got := v.ExactString()
		if i == 3 {
			got = v.String()
		}
		if err != nil || got != want {
			t.Errorf("float %d: %q, %v; want %q", i, got, err, want)
		}
	}
	for i, want := range []string{"(0 + 2i)", "(0 + 3/2i)"} {
		v, err := elts[17+i].(*golang.ImagLit).Constant()
		if err != nil || v.ExactString() != want {
			t.Errorf("imag %d: %v, %v; want %s", i, v, err, want)
		}
	}
	// A literal that is not valid is an error, not a panic.
	if _, err := (&golang.IntLit{Text: "0x"}).Constant(); err == nil {
		t.Error("IntLit 0x: no error")
	}
	if _, err := (&golang.StringLit{Text: `"\q"`}).Value(); err == nil {
		t.Error(`StringLit "\q": no error`)
	}
}
