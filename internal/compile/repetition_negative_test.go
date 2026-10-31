package compile

import (
	"os"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
)

// BenchmarkBytecodeLowering isolates construction of the module from an
// already analyzed grammar; source parsing, AST checking and VM setup are out.
func BenchmarkBytecodeLowering(b *testing.B) {
	for _, c := range []struct{ name, path string }{
		{"JSON", "../../parsers/json/json.zp"},
		{"Minilang", "../../examples/minilang/minilang.zp"},
	} {
		b.Run(c.name, func(b *testing.B) {
			source, err := os.ReadFile(c.path)
			if err != nil {
				b.Fatal(err)
			}
			g, err := parseTest(string(source))
			if err != nil {
				b.Fatal(err)
			}
			p, err := compileStatic(g, Options{}, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				compiler := &bcompiler{prog: p, m: &bytecode.Module{}, strs: map[string]int{}}
				compiler.compile()
			}
		})
	}
}
