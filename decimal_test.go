package pego_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestInvalidSourceDecimalReferences(t *testing.T) {
	for _, digits := range []string{"١", "１２", "1١", strconv.FormatUint(uint64(^uint(0)>>1)+1, 10)} {
		source := `def main="a" -> $` + digits
		if g, err := pego.ParseGrammar(source); g != nil || err == nil || !strings.Contains(err.Error(), "invalid positional reference") {
			t.Fatalf("ParseGrammar accepted %q: %v, %v", source, g, err)
		}
		if p, err := pego.CompileSource(source, "main"); p != nil || err == nil || !strings.Contains(err.Error(), "invalid positional reference") {
			t.Fatalf("CompileSource accepted %q: %v, %v", source, p, err)
		}
	}
}

func TestDecimalReferenceValues(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`def main="a" "b" -> $0`, `["a" "b"]`},
		{`def main="a" "b" -> $000`, `["a" "b"]`},
		{`def main="a" "b" -> $001`, `"a"`},
		{`def main="a" "b" -> $002`, `"b"`},
	} {
		p, err := pego.CompileSource(tc.source, "main")
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				n, err := p.Parse("ab", pego.WithBackend(backend), pego.WithUnit(unit))
				if err != nil || n.String() != tc.want {
					t.Fatalf("%s backend=%v unit=%v: %v, %v", tc.source, backend, unit, n, err)
				}
			}
		}
	}
}
