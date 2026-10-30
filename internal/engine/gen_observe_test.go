package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedTraceOptions(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTraceOptions)
}

func testGeneratedTraceOptions(t *testing.T) {
	g, err := parseTest(`rule main = "a"`)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"type TraceEvent", "func newProfileObservation", "func WithTrace"} {
		if strings.Contains(string(plain), marker) {
			t.Fatalf("ordinary output contains %s", marker)
		}
	}
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Types: true, Observability: true}); err != nil {
		t.Fatal("typed observability:", err)
	}
	for _, opts := range []gen.GenOptions{{Document: true}, {Document: true, Types: true}} {
		opts.Package, opts.Start, opts.Observability = "fixture", "main", true
		if _, err := gentest.GenerateCode(t, g, opts); err != nil {
			t.Fatal("Document observability:", err)
		}
	}
	if code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Observability: true}); err != nil || !strings.Contains(string(code), "export interface TraceEvent") {
		t.Fatal("TypeScript observability:", err)
	}
	if _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Observability: true, Document: true}); err == nil {
		t.Fatal("TypeScript accepted unsupported Document")
	}
}
