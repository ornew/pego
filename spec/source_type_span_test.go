package spec_test

import (
	"reflect"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/sourceast"
)

func TestCanonicalTypeDeclarationSpans(t *testing.T) {
	p, _ := sourceGrammar(t)
	for _, tc := range []struct {
		name, source string
		spans        []string
	}{
		{"terminal", "// leading\ntype T terminal // trailing\nrule main = \"x\"", []string{"type T terminal"}},
		{"same line separator", "type A terminal; /* next */ type B = []A", []string{"type A terminal", "type B = []A"}},
		{"alias trailing block", "type T = A /* outside */\nrule main = \"x\"", []string{"type T = A"}},
		{"multiline alias", "/* detached */\n\n// leading\ntype T = (\n []A | *B\n)\n// trailing\n", []string{"type T = (\n []A | *B\n)"}},
		{"struct", "type T struct {\n // field\n A Match\n B []Match; // previous\n // unowned\n}\n\n// eof", []string{"type T struct {\n // field\n A Match\n B []Match; // previous\n // unowned\n}"}},
		{"unicode and CRLF", "// 😀\r\ntype T struct { A /* β */ Match }\r\n", []string{"type T struct { A /* β */ Match }"}},
		{"CR separators", "type T terminal\rtype U = *T\r", []string{"type T terminal", "type U = *T"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertCanonicalTypeSpans(t, p, tc.source, tc.spans)
			manual, err := pego.ParseGrammar(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			// Formatting changes offsets but each frontend must still report the
			// same syntax-only span on the newly formatted source.
			assertCanonicalTypeSpans(t, p, grammar.Format(manual), nil)
		})
	}
}

func assertCanonicalTypeSpans(t *testing.T, p *pego.Parser, source string, expected []string) {
	t.Helper()
	manual, err := pego.ParseGrammar(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		n, err := p.Parse(source, pego.WithBackend(backend))
		if err != nil {
			t.Fatalf("%v: %v", backend, err)
		}
		var got []string
		index := 0
		for _, def := range sourceDefs(t, n) {
			if def.Type() != "TypeDef" {
				continue
			}
			if index >= len(manual.Types()) {
				t.Fatal("canonical parser added a type declaration")
			}
			span := sourceField(t, def, "SourceSpan")
			got = append(got, string([]rune(source)[int(span.Start):int(span.End)]))
			want := manual.Types()[index].SourceInfo
			adapted := want
			if err := sourceast.NewPositions(source).SetSpan(&adapted, int(span.Start), int(span.End)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(adapted, want) {
				t.Fatalf("%v: type %d span differs: canonical=%+v manual=%+v", backend, index, adapted, want)
			}
			index++
		}
		if index != len(manual.Types()) || expected != nil && !reflect.DeepEqual(got, expected) {
			t.Fatalf("%v: type spans=%q, expected=%q; manual types=%d", backend, got, expected, len(manual.Types()))
		}
	}
}
