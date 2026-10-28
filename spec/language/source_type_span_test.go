package language_test

import (
	"reflect"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/spec/language/internal/sourceast"
)

func TestCanonicalTypeDeclarationSpans(t *testing.T) {
	p, _ := sourceGrammar(t)
	for _, tc := range []struct {
		name, source string
		spans        []string
	}{
		{"terminal", "// leading\ntype T = { Text string } // trailing\nrule main = \"x\"", []string{"type T = { Text string }"}},
		{"same line separator", "type A = { Text string }; /* next */ type B ~ []A", []string{"type A = { Text string }", "type B ~ []A"}},
		{"alias trailing block", "type T ~ A /* outside */\nrule main = \"x\"", []string{"type T ~ A"}},
		{"multiline alias", "/* detached */\n\n// leading\ntype T ~ (\n []A | *B\n)\n// trailing\n", []string{"type T ~ (\n []A | *B\n)"}},
		{"struct", "type T = {\n // field\n A node[string]\n B []node[string]; // previous\n // unowned\n}\n\n// eof", []string{"type T = {\n // field\n A node[string]\n B []node[string]; // previous\n // unowned\n}"}},
		{"unicode and CRLF", "// 😀\r\ntype T = { A /* β */ node[string] }\r\n", []string{"type T = { A /* β */ node[string] }"}},
		{"CR separators", "type T = { Text string }\rtype U ~ *T\r", []string{"type T = { Text string }", "type U ~ *T"}},
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
		for _, def := range sourceDecls(t, n) {
			if def.Type() != "TypeDef" && def.Type() != "TypeAlias" {
				continue
			}
			if index >= len(manual.Types()) {
				t.Fatal("canonical parser added a type declaration")
			}
			span := sourceField(t, def, "SourceSpan")
			got = append(got, string([]rune(source)[int(span.Start()):int(span.End())]))
			want := *manual.Types()[index].(grammar.SourceNode).Source()
			adapted := want
			if err := sourceast.NewPositions(source).SetSpan(&adapted, int(span.Start()), int(span.End())); err != nil {
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
