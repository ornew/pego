package typescript_test

import (
	"math"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/typescript"
)

func TestStringValue(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`'abc'`, "abc"},
		{`"a\tb\n"`, "a\tb\n"},
		{`'it\'s'`, "it's"},
		{`"\x41B\u{43}\u{1F600}"`, "ABC\U0001F600"},
		{`"😀"`, "\U0001F600"},
		{`"\ud83d"`, "�"},
		{`"\ude00x"`, "�x"},
		{`"a\` + "\n" + `b"`, "ab"},
		{`"a\` + "\r\n" + `b"`, "ab"},
		{`"\0\a\q"`, "\x00aq"},
		{`"日本語"`, "日本語"},
		{`""`, ""},
	} {
		f, err := typescript.ParseAST(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		lit := f.Statements[0].(*typescript.ExpressionStatement).Expression.(*typescript.StringLiteral)
		if got := lit.Value(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.src, got, c.want)
		}
	}
}

func TestTemplateValue(t *testing.T) {
	f, err := typescript.ParseAST("`a\\n${1}`; `b\\u0041\r\nc`")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Statements[0].(*typescript.ExpressionStatement).Expression.(*typescript.TemplateExpression); !ok {
		t.Fatalf("%T", f.Statements[0].(*typescript.ExpressionStatement).Expression)
	}
	lit := f.Statements[1].(*typescript.ExpressionStatement).Expression.(*typescript.NoSubstitutionTemplateLiteral)
	if got := lit.Value(); got != "bA\nc" {
		t.Errorf("got %q", got)
	}
}

func TestNumberValue(t *testing.T) {
	for _, c := range []struct {
		src  string
		want float64
	}{
		{"0", 0}, {"42", 42}, {"1_000", 1000}, {".5", 0.5}, {"1.", 1}, {"1e3", 1000}, {"1.5E-2", 0.015},
		{"0x1F", 31}, {"0XfF", 255}, {"0b101", 5}, {"0o17", 15}, {"0xFFFF_FFFF", 4294967295},
		{"1e999", math.Inf(1)}, {"0x" + strings.Repeat("F", 300), math.Inf(1)},
	} {
		f, err := typescript.ParseAST(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		lit := f.Statements[0].(*typescript.ExpressionStatement).Expression.(*typescript.NumericLiteral)
		if got := lit.Value(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.src, got, c.want)
		}
	}
}

func TestBigIntValue(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"0n", "0"}, {"123n", "123"}, {"1_000_000n", "1000000"}, {"0xFFn", "255"}, {"0b11n", "3"},
		{"0o17n", "15"}, {"123456789012345678901234567890n", "123456789012345678901234567890"},
	} {
		f, err := typescript.ParseAST(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		lit := f.Statements[0].(*typescript.ExpressionStatement).Expression.(*typescript.BigIntLiteral)
		if got := lit.Value().String(); got != c.want {
			t.Errorf("%s: got %s, want %s", c.src, got, c.want)
		}
	}
}

func TestIdentifierValue(t *testing.T) {
	f, err := typescript.ParseAST(`abc; \u{62}c; plain; é`)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"abc", "bc", "plain", "é"} {
		id := f.Statements[i].(*typescript.ExpressionStatement).Expression.(*typescript.Identifier)
		if got := id.Value(); got != want {
			t.Errorf("%q: got %q, want %q", id.Text, got, want)
		}
	}
}
