package cel_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/cel"
)

// policies are the expressions of testdata/bench/policies.txt: typical access policies, admission checks and validation
// rules of 40 to 200 characters.
var policies = func() []string {
	data, err := os.ReadFile("testdata/bench/policies.txt")
	if err != nil {
		panic(err)
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
}()

// large is one expression of about 256 KB: all the policies, over and over, joined by &&.
var large = func() string {
	var b strings.Builder
	for i := 0; b.Len() < 256<<10; i++ {
		if i > 0 {
			b.WriteString("\n  && ")
		}
		b.WriteString("(" + policies[i%len(policies)] + ")")
	}
	return b.String()
}()

func policyBytes() (n int64) {
	for _, p := range policies {
		n += int64(len(p))
	}
	return n
}

func TestPolicies(t *testing.T) {
	if len(policies) < 20 {
		t.Fatalf("%d policies", len(policies))
	}
	for _, p := range policies {
		if _, err := cel.ParseExpr(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	if _, err := cel.ParseAST(large); err != nil {
		t.Error(err)
	}
}

// The benchmarks parse every policy in turn, so that the time per op is that of parsing the full suite of typical expressions.

func BenchmarkPoliciesParseAST(b *testing.B) {
	b.SetBytes(policyBytes())
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range policies {
			if _, err := cel.ParseAST(p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPoliciesParseExpr(b *testing.B) {
	b.SetBytes(policyBytes())
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range policies {
			if _, err := cel.ParseExpr(p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPoliciesRecognize(b *testing.B) {
	b.SetBytes(policyBytes())
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range policies {
			if err := cel.Recognize(p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPoliciesParse(b *testing.B) {
	b.SetBytes(policyBytes())
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range policies {
			if _, err := cel.Parse(p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkLargeParseAST(b *testing.B) {
	b.SetBytes(int64(len(large)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := cel.ParseAST(large); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeRecognize(b *testing.B) {
	b.SetBytes(int64(len(large)))
	b.ReportAllocs()
	for b.Loop() {
		if err := cel.Recognize(large); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeParse(b *testing.B) {
	b.SetBytes(int64(len(large)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := cel.Parse(large); err != nil {
			b.Fatal(err)
		}
	}
}
