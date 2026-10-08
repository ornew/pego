package main

import (
	"os"
	"strings"
	"testing"

	"cel.dev/cel-go/common"
	"cel.dev/cel-go/parser"
)

// The benchmarks of the parser of cel-go on the expressions of testdata/bench/policies.txt, for comparison with
// the benchmarks of package cel (bench_test.go there), which parse the same expressions. Run them with
//
//	go test -bench . -benchmem
//
// from this directory.

func policies(b *testing.B) []string {
	data, err := os.ReadFile("../../testdata/bench/policies.txt")
	if err != nil {
		b.Fatal(err)
	}
	var out []string
	for _, block := range strings.Split(string(data), "\n---\n") {
		comments := true
		for _, line := range strings.Split(block, "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "//") {
				comments = false
			}
		}
		if !comments {
			out = append(out, strings.TrimSpace(block))
		}
	}
	return out
}

func benchParse(b *testing.B, p *parser.Parser, src []string) {
	var n int64
	for _, s := range src {
		n += int64(len(s))
	}
	b.SetBytes(n)
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range src {
			if _, errs := p.Parse(common.NewTextSource(s)); len(errs.GetErrors()) > 0 {
				b.Fatalf("%q: %v", s, errs.ToDisplayString())
			}
		}
	}
}

// BenchmarkCELGoPolicies parses with the options of cel.NewEnv: the standard macros expanded.
func BenchmarkCELGoPolicies(b *testing.B) {
	p, err := parser.NewParser(parser.Macros(parser.AllMacros...), parser.EnableOptionalSyntax(true))
	if err != nil {
		b.Fatal(err)
	}
	benchParse(b, p, policies(b))
}

// BenchmarkCELGoPoliciesNoMacros parses with the reference configuration of this generator, which leaves the
// macros as calls, as package cel does.
func BenchmarkCELGoPoliciesNoMacros(b *testing.B) {
	benchParse(b, newParser(), policies(b))
}

// BenchmarkCELGoLarge parses one expression of about 256 KB, as BenchmarkLargeParseAST of package cel does.
func BenchmarkCELGoLarge(b *testing.B) {
	ps := policies(b)
	var sb strings.Builder
	for i := 0; sb.Len() < 256<<10; i++ {
		if i > 0 {
			sb.WriteString("\n  && ")
		}
		sb.WriteString("(" + ps[i%len(ps)] + ")")
	}
	p, err := parser.NewParser(parser.EnableOptionalSyntax(true), parser.EnableVariadicOperatorASTs(true), parser.MaxRecursionDepth(-1),
		parser.ExpressionSizeCodePointLimit(-1), parser.MaxExpressionNodeCount(-1))
	if err != nil {
		b.Fatal(err)
	}
	benchParse(b, p, []string{sb.String()})
}
