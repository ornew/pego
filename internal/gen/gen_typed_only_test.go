package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedTypedOnly(t *testing.T) {
	t.Parallel()
	g, err := parseTest(gentest.OrdinaryOnlyTyped)
	if err != nil {
		t.Fatal(err)
	}
	generate := func(opts gen.GenOptions) (string, error) {
		opts.Package, opts.Module, opts.Start, opts.Runtime = "p", "example.com/p", "main", gen.RuntimeImport
		files, err := gentest.Generate(t, g, opts)
		if err != nil {
			return "", err
		}
		return string(gentest.MergeGenerated(files)), nil
	}
	only, err := generate(gen.GenOptions{Types: true, TypedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	full, err := generate(gen.GenOptions{Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"func Parse(", "func ParseRule(", "func Recognize(", "recRules", "zpRecGrammar", "RunParse("} {
		if strings.Contains(only, absent) {
			t.Errorf("a typed-only parser has %q", absent)
		}
		if absent != "RunParse(" && !strings.Contains(full, absent) {
			t.Errorf("the control parser lacks %q: the check is void", absent)
		}
	}
	for _, present := range []string{"func ParseAST(", "func ParseRuleAST("} {
		if !strings.Contains(only, present) {
			t.Errorf("a typed-only parser lacks %q", present)
		}
	}
	if len(only)*3 >= len(full)*2 {
		t.Errorf("the typed-only parser is %d bytes, the full one %d: want under two thirds", len(only), len(full))
	}

	// The refused combinations, with their messages.
	for _, tc := range []struct {
		name string
		opts gen.GenOptions
		want string
	}{
		{"without types", gen.GenOptions{TypedOnly: true}, "typed-only output needs typed output (-types)"},
		{"recognize", gen.GenOptions{Types: true, TypedOnly: true, Recognize: true}, "typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node"},
		{"document", gen.GenOptions{Types: true, TypedOnly: true, Document: true}, "typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node"},
		{"stream", gen.GenOptions{Types: true, TypedOnly: true, Stream: true}, "typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node"},
	} {
		if _, err := generate(tc.opts); err == nil || err.Error() != tc.want {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := gentest.GenerateTS(g, gen.GenOptions{Start: "main", TypedOnly: true}); err == nil || err.Error() != "typed-only generation is only supported for Go" {
		t.Errorf("TypeScript: error %v", err)
	}

	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("ordtest")), 0o644); err != nil {
		t.Fatal(err)
	}
	gentest.OrdinaryOnlyPackage(t, dir, "full", gentest.OrdinaryOnlyTyped, gen.GenOptions{Types: true, Recognize: true, Document: true, BudgetControls: true}, gentest.OrdinaryOnlyTypedEntries, gentest.OrdinaryOnlyTypedInputs)
	gentest.OrdinaryOnlyPackage(t, dir, "only", gentest.OrdinaryOnlyTyped, gen.GenOptions{Types: true, TypedOnly: true}, gentest.OrdinaryOnlyTypedEntries, gentest.OrdinaryOnlyTypedInputs)
	gentest.OrdinaryOnlyPackage(t, dir, "mutated", strings.Replace(gentest.OrdinaryOnlyTyped, `"+"`, `"-"`, 1), gen.GenOptions{Types: true, TypedOnly: true}, gentest.OrdinaryOnlyTypedEntries, gentest.OrdinaryOnlyTypedInputs)
	out := gentest.RunTranscripts(t, dir, "full", "only", "mutated")
	if out["full"] != out["only"] {
		t.Errorf("the typed-only parser differs from the full one:\n%s", gentest.FirstTranscriptDifference(out["full"], out["only"]))
	}
	if !strings.Contains(out["only"], "syntax error") || !strings.Contains(out["only"], `"Text":"a"`) {
		t.Error("the transcript has neither a syntax error nor a value")
	}
	if out["mutated"] == out["only"] {
		t.Error("the transcripts of a mutated grammar are the same: the comparison cannot fail")
	}
}
