package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGenerateStreamOptions(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateStreamOptions)
}

func testGenerateStreamOptions(t *testing.T) {
	g, err := parseTest(`rule main = item* #stream $$
rule item = "a"
rule other = "b"`)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"stream:", "streamInput", "func ParseStream", "\"bufio\"", "WithDiagnostic", "func Iterate", "StreamIterator"} {
		if strings.Contains(string(plain), marker) {
			t.Errorf("ordinary output contains %s", marker)
		}
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Stream: true})
	if err != nil || !strings.Contains(string(code), "func ParseStream(") || strings.Contains(string(code), "stream:") {
		t.Fatalf("stream output: %v", err)
	}
	for _, tc := range []struct {
		opts gen.GenOptions
		want string
	}{
		{gen.GenOptions{Start: "other", Stream: true}, "needs a #stream repetition"},
	} {
		tc.opts.Package = "fixture"
		if _, err := gentest.GenerateCode(t, g, tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.opts, err, tc.want)
		}
	}
	if _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Stream: true}); err == nil {
		t.Error("TypeScript accepted stream generation")
	}
	typed, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Stream: true, Types: true})
	if err != nil || !strings.Contains(string(typed), "func ParseStreamAST(") || !strings.Contains(string(typed), "type StreamValue = ") {
		t.Fatalf("typed stream output: %v", err)
	}
	if typed, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Types: true}); err != nil || strings.Contains(string(typed), "ParseStreamAST") {
		t.Fatalf("typed output without streams: %v", err)
	}
}
