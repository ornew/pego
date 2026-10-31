package gogen_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedTypedLRBodies(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedLRBodies)
}

func testGeneratedTypedLRBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range gentest.TypedLRBodyCases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts := gen.GenOptions{Package: "lrbody", Start: "main", Types: true, Recognize: true}
		code, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !strings.Contains(string(code), "TParse(trules") {
			t.Fatalf("%s: typed runtime is not exercised", c.Name)
		}
		inlined := strings.Contains(string(code), "(typed LR body inlined)")
		if strings.HasSuffix(c.Name, "_fallback") {
			if inlined {
				t.Fatalf("%s: unsupported body must retain the general route", c.Name)
			}
		} else if !inlined {
			t.Fatalf("%s: no unfinished typed LR body", c.Name)
		}
		for _, r := range mustCompile(t, c.Src).RuleList {
			if r.Leader && strings.Contains(string(code), "// "+r.Name+", invoked as by invoke (body inlined)") {
				t.Fatalf("%s: leader %s must keep the growth wrapper", c.Name, r.Name)
			}
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedLRBodyCases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedLRBodyCases, gen.GenOptions{ConvertTypes: true})
}
