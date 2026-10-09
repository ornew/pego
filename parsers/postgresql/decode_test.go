package postgresql_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/postgresql"
)

func TestIdentValue(t *testing.T) {
	for _, c := range []struct {
		text, want string
		quoted     bool
	}{
		{"Foo", "foo", false},
		{"FOO_Bar$1", "foo_bar$1", false},
		{`"Foo"`, "Foo", true},
		{`"a""b"`, `a"b`, true},
		{`U&"d\0061t\+000061"`, "data", true},
		{`U&"d!0061t!+000061" UESCAPE '!'`, "data", true},
		{`"` + strings.Repeat("x", 70) + `"`, strings.Repeat("x", 63), true},
		{"École", "École", false}, // only ASCII letters are folded
	} {
		id := &postgresql.Ident{Text: c.text}
		got, err := id.Value()
		if err != nil || got != c.want || id.Quoted() != c.quoted {
			t.Errorf("Ident(%s) = %q, %v, quoted %v; want %q, quoted %v", c.text, got, err, id.Quoted(), c.want, c.quoted)
		}
	}
}

func TestSconstValue(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{`'abc'`, "abc"},
		{`'it''s'`, "it's"},
		{"'a'\n'b'", "ab"},
		{"'a' -- c\n'b'", "ab"},
		{`E'a\nb\x41\101é\U0001F600\\'`, "a\nbAAé\U0001F600\\"},
		{`E'😀'`, "\U0001F600"},
		{`U&'d\0061t\+000061'`, "data"},
		{`U&'d!0061' UESCAPE '!'`, "da"},
		{`$$a'b$$`, "a'b"},
		{`$tag$ $$ x $tag$`, " $$ x "},
	} {
		got, err := (&postgresql.Sconst{Text: c.text}).Value()
		if err != nil || got != c.want {
			t.Errorf("Sconst(%s) = %q, %v; want %q", c.text, got, err, c.want)
		}
	}
	for _, bad := range []string{`E'\x00'`, `E'\xff'`, `U&'\d800'`, `U&'\zzzz'`} {
		if got, err := (&postgresql.Sconst{Text: bad}).Value(); err == nil {
			t.Errorf("Sconst(%s) = %q, want an error", bad, got)
		}
	}
}

func TestOtherValues(t *testing.T) {
	if v, err := (&postgresql.Iconst{Text: "0x_ff"}).Int64(); err != nil || v != 255 {
		t.Errorf("Iconst 0x_ff = %d, %v", v, err)
	}
	if v, err := (&postgresql.Iconst{Text: "1_000"}).Int64(); err != nil || v != 1000 {
		t.Errorf("Iconst 1_000 = %d, %v", v, err)
	}
	if v := (&postgresql.Param{Text: "$12"}).Value(); v != 12 {
		t.Errorf("Param $12 = %d", v)
	}
	if v := (&postgresql.OpTok{Text: "!="}).Value(); v != "<>" {
		t.Errorf("OpTok != = %q", v)
	}
	if v := (&postgresql.Bconst{Text: "B'101'"}).Value(); v != "b101" {
		t.Errorf("Bconst = %q", v)
	}
	if v := (&postgresql.Xconst{Text: "X'1F'"}).Value(); v != "x1F" {
		t.Errorf("Xconst = %q", v)
	}
}
