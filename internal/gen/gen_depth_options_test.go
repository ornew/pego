package gen_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedMaxDepthValidation(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedMaxDepthValidation)
}

func testGeneratedMaxDepthValidation(t *testing.T) {
	g, err := parseTest(`rule main = "x"`)
	if err != nil {
		t.Fatal(err)
	}
	safe := uint64(9007199254740991)
	if uint64(^uint(0)>>1) >= safe+1 {
		if got, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", MaxDepth: int(safe)}); err != nil || len(got) == 0 {
			t.Fatalf("safe JavaScript boundary rejected: %v", err)
		}
		if got, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", MaxDepth: int(safe + 1)}); err == nil || len(got) != 0 {
			t.Fatal("unsafe JavaScript boundary accepted")
		}
	}
	for _, ts := range []bool{false, true} {
		generate := func(g *grammar.Grammar, opts gen.GenOptions) ([]byte, error) { return gentest.GenerateCode(t, g, opts) }
		if ts {
			generate = gentest.GenerateTSBundle
		}
		base, err := generate(g, gen.GenOptions{Package: "depth", Start: "main"})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []int{0, engine.DefaultMaxDepth} {
			got, err := generate(g, gen.GenOptions{Package: "depth", Start: "main", MaxDepth: n})
			if err != nil || !bytes.Equal(base, got) {
				t.Fatalf("ts=%v depth=%d equivalent default differs: %v", ts, n, err)
			}
		}
		for _, n := range []int{3, int(^uint(0) >> 1)} {
			got, err := generate(g, gen.GenOptions{Package: "depth", Start: "main", MaxDepth: n})
			if ts && uint64(n) > 9007199254740991 {
				if err == nil || len(got) != 0 {
					t.Fatal("unsafe JavaScript default accepted")
				}
				continue
			}
			want := fmt.Sprintf("var zpGrammar = rt.NewGrammar(%d)", n)
			if ts {
				want = fmt.Sprintf("const G = mkGrammar(%d, ", n)
			}
			if err != nil || !strings.Contains(string(got), want) {
				t.Fatalf("custom default %d: %v", n, err)
			}
		}
		if got, err := generate(g, gen.GenOptions{Package: "depth", Start: "main", MaxDepth: -1}); err == nil || len(got) != 0 {
			t.Fatal("negative default accepted")
		}
	}
}
