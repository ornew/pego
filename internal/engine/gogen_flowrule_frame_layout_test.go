package engine

import (
	"github.com/ornew/pego/internal/gentest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedTypedFrameLayouts(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedTypedFrameLayouts)
}

func testGeneratedTypedFrameLayouts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	cases := append([]gentest.Case(nil), gentest.TypedFrameLayoutCases...)
	for _, c := range gentest.TypedLRBodyCases {
		if c.Name == "typed/LR_unreachable_capture" {
			cases = append(cases, c)
		}
	}
	for _, c := range gentest.TypedOperatorsRuleBodyCases {
		if c.Name == "typed/OperatorsRule_unreachable_capture" {
			cases = append(cases, c)
		}
	}
	for _, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts := gen.GenOptions{Package: "layouts", Start: "main", Types: true, Recognize: true}
		candidate, err := generateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		marker := "(typed OperatorsRule body inlined)"
		if strings.Contains(c.Name, "/LR_") {
			marker = "(typed LR body inlined)"
		}
		if !strings.Contains(string(candidate), marker) {
			t.Fatalf("%s: candidate route not exercised", c.Name)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true})
}
