package typescript_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/typescript"
)

type cookedLiteralCase struct{ name, src, want string }

func cookedLiteralCases() []cookedLiteralCase {
	var cases []cookedLiteralCase
	add := func(name, body, want string) {
		for _, delimiter := range []string{"\"", "'", "`"} {
			cases = append(cases, cookedLiteralCase{name + "/" + delimiter, delimiter + body + delimiter, want})
		}
	}
	add("empty", "", "")
	add("plain", "日本語😀", "日本語😀")
	add("adjacent pair", `\uD83D\uDE00`, "😀")
	add("real character", `\uD83Dx\uDE00`, "�x�")
	add("escaped LF", `\uD83D\n\uDE00`, "�\n�")
	add("escaped CR", `a\rb`, "a\rb")
	add("escaped CRLF", `a\r\nb`, "a\r\nb")
	add("pending high", `\uD83D`, "�")
	add("low alone", `\uDE00`, "�")
	for _, nl := range []struct{ name, text string }{
		{"LF", "\n"}, {"CR", "\r"}, {"CRLF", "\r\n"}, {"LS", "\u2028"}, {"PS", "\u2029"},
	} {
		continuation := "\\" + nl.text
		add(nl.name+" continuation", "a"+continuation+"b", "ab")
		add(nl.name+" pair", `\uD83D`+continuation+`\uDE00`, "😀")
		add(nl.name+" multiple continuations", `\uD83D`+continuation+continuation+`\uDE00`, "😀")
		add(nl.name+" intervening character", `\uD83D`+continuation+`x\uDE00`, "�x�")
		add(nl.name+" intervening escape", `\uD83D`+continuation+`\n\uDE00`, "�\n�")
		add(nl.name+" pending at EOF", `\uD83D`+continuation, "�")
		add(nl.name+" low alone", continuation+`\uDE00`, "�")
		add(nl.name+" another high", `\uD83D`+continuation+`\uD83D\uDE00`, "�😀")
		want := "\n"
		if nl.name == "LS" || nl.name == "PS" {
			want = nl.text
		}
		cases = append(cases, cookedLiteralCase{nl.name + "/raw template", "`a" + nl.text + "b`", "a" + want + "b"})
		cases = append(cases, cookedLiteralCase{nl.name + "/raw template between surrogates", "`" + `\uD83D` + nl.text + `\uDE00` + "`", "�" + want + "�"})
	}
	return cases
}

func cookedValue(t *testing.T, src string, unit typescript.Unit) string {
	t.Helper()
	f, err := typescript.ParseAST(src, unit)
	if err != nil {
		t.Fatalf("ParseAST(%q): %v", src, err)
	}
	switch lit := f.Statements[0].(*typescript.ExpressionStatement).Expression.(type) {
	case *typescript.StringLiteral:
		return lit.Value()
	case *typescript.NoSubstitutionTemplateLiteral:
		return lit.Value()
	default:
		t.Fatalf("not a literal: %T", lit)
		return ""
	}
}

func TestCookedLiteralValues(t *testing.T) {
	for _, c := range cookedLiteralCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, unit := range []typescript.Unit{typescript.CodePoints, typescript.Bytes} {
				if got := cookedValue(t, c.src, unit); got != c.want {
					t.Errorf("Value(%q, %v) = %q; want %q", c.src, unit, got, c.want)
				}
			}
		})
	}
}

// JSON replaces lone UTF-16 surrogates with U+FFFD when Go decodes the reference
// strings, matching the documented Go-string representation of Value().
func TestTSCCookedLiteralValues(t *testing.T) {
	p := startTSC(t)
	for _, c := range cookedLiteralCases() {
		t.Run(c.name, func(t *testing.T) {
			r, err := p.parse("literal.ts", c.src, true)
			if err != nil {
				t.Fatal(err)
			}
			if r.Version != "5.9.3" {
				t.Fatalf("TypeScript %q; want exactly 5.9.3", r.Version)
			}
			if len(r.Diags) != 0 || len(r.Values) != 1 {
				t.Fatalf("reference result: %+v", r)
			}
			if r.Values[0] != c.want {
				t.Fatalf("reference Value = %q; fixture expects %q", r.Values[0], c.want)
			}
			if got := cookedValue(t, c.src, typescript.CodePoints); got != r.Values[0] {
				t.Errorf("Value(%q) = %q; TypeScript 5.9.3 gives %q", c.src, got, r.Values[0])
			}
		})
	}
}

// Parsing is excluded. CR and surrogate-continuation cases intentionally change
// the baseline's incorrect output; the other cases are equivalent controls.
func BenchmarkCookedLiteralValue(b *testing.B) {
	for _, tc := range []struct{ name, body string }{
		{"plain", strings.Repeat("abc", 256)},
		{"LF", strings.Repeat("a\nb", 256)},
		{"CRLF", strings.Repeat("a\r\nb", 256)},
		{"CR", strings.Repeat("a\rb", 256)},
		{"escaped", strings.Repeat(`a\n\u0041`, 64)},
		{"surrogate continuation", strings.Repeat(`\uD83D`+"\\\n"+`\uDE00`, 64)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			literal := &typescript.NoSubstitutionTemplateLiteral{Text: "`" + tc.body + "`"}
			b.SetBytes(int64(len(tc.body)))
			b.ReportAllocs()
			for b.Loop() {
				_ = literal.Value()
			}
		})
	}
}
