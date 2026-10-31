package apitest_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

func TestInvalidSourceDecimalReferences(t *testing.T) {
	for _, digits := range []string{"١", "１２", "1١", strconv.FormatUint(uint64(^uint(0)>>1)+1, 10)} {
		source := `def main="a" -> $` + digits
		if g, err := zp.ParseGrammar(source); g != nil || err == nil || !strings.Contains(err.Error(), "invalid positional reference") {
			t.Fatalf("ParseGrammar accepted %q: %v, %v", source, g, err)
		}
		if p, err := compileTestSource(source, "main"); p != nil || err == nil || !strings.Contains(err.Error(), "invalid positional reference") {
			t.Fatalf("CompileSource accepted %q: %v, %v", source, p, err)
		}
	}
}

func TestDecimalReferenceValues(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`rule main = v1:"a" v2:"b" => list($v1, $v2)`, `["a" "b"]`},
		{`rule main = v1:"a" v2:"b" => list($v1, $v2)`, `["a" "b"]`},
		{`rule main = v1:"a" "b" => $v1`, `"a"`},
		{`rule main = "a" v2:"b" => $v2`, `"b"`},
	} {
		p, err := compileTestSource(tc.source, "main")
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
			for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
				n, err := p.Parse("ab", zp.WithBackend(backend), zp.WithUnit(unit))
				if err != nil || n.String() != tc.want {
					t.Fatalf("%s backend=%v unit=%v: %v, %v", tc.source, backend, unit, n, err)
				}
			}
		}
	}
}
