package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// A typed-only parser has the typed entry points and none of the Node family. These tests check what it
// lacks, the messages of the combinations that are refused, and that its typed entry points return what
// those of a parser with every entry point return.

// ordinaryOnlyTypedEntries prints only the typed entry points, which a typed-only parser has.
const ordinaryOnlyTypedEntries = `package PKG

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	e := err.Error()
	var se *SyntaxError
	if errors.As(err, &se) {
		e += fmt.Sprintf(" @%d:%d:%d", se.Line, se.Col, se.Pos)
	}
	return e
}

func showAST(v any, err error) string {
	j, jerr := json.Marshal(v)
	if jerr != nil {
		return "marshal: " + jerr.Error()
	}
	return string(j) + " | " + errText(err)
}

func TestTranscript(t *testing.T) {
	var b strings.Builder
	for _, in := range INPUTS {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			v, err := ParseAST(in, WithUnit(unit))
			fmt.Fprintf(&b, "ast %q %v: %s\n", in, unit, showAST(v, err))
			w, err := ParseRuleAST("sum", in, WithUnit(unit))
			fmt.Fprintf(&b, "rule sum %q %v: %s\n", in, unit, showAST(w, err))
			w, err = ParseRuleAST("a", in, WithUnit(unit))
			fmt.Fprintf(&b, "rule a %q %v: %s\n", in, unit, showAST(w, err))
		}
		v, err := ParseAST(in, WithMaxDepth(5))
		fmt.Fprintf(&b, "depth %q: %s\n", in, showAST(v, err))
		_, err = ParseRuleAST("nope", in)
		fmt.Fprintf(&b, "unknown rule: %s\n", errText(err))
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("TRANSCRIPT_DIR"), "PKG.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`

func TestGeneratedTypedOnly(t *testing.T) {
	g, err := syntax.Parse(ordinaryOnlyTyped)
	if err != nil {
		t.Fatal(err)
	}
	gen := func(opts GenOptions) (string, error) {
		opts.Package, opts.Module, opts.Start, opts.Runtime = "p", "example.com/p", "main", RuntimeImport
		files, err := Generate(g, opts)
		if err != nil {
			return "", err
		}
		return string(mergeGenerated(files)), nil
	}
	only, err := gen(GenOptions{Types: true, TypedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	full, err := gen(GenOptions{Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"func Parse(", "func ParseRule(", "func Recognize(", "recRules", "pegoRecGrammar", "RunParse("} {
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
		opts GenOptions
		want string
	}{
		{"without types", GenOptions{TypedOnly: true}, "typed-only output needs typed output (-types)"},
		{"recognize", GenOptions{Types: true, TypedOnly: true, Recognize: true}, "typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node"},
		{"document", GenOptions{Types: true, TypedOnly: true, Document: true}, "typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node"},
		{"stream", GenOptions{Types: true, TypedOnly: true, Stream: true}, "typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node"},
	} {
		if _, err := gen(tc.opts); err == nil || err.Error() != tc.want {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := GenerateTS(g, GenOptions{Start: "main", TypedOnly: true}); err == nil || err.Error() != "typed-only generation is only supported for Go" {
		t.Errorf("TypeScript: error %v", err)
	}

	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("ordtest")), 0o644); err != nil {
		t.Fatal(err)
	}
	ordinaryOnlyPackage(t, dir, "full", ordinaryOnlyTyped, GenOptions{Types: true, Recognize: true, Document: true, WorkControls: true}, ordinaryOnlyTypedEntries, ordinaryOnlyTypedInputs)
	ordinaryOnlyPackage(t, dir, "only", ordinaryOnlyTyped, GenOptions{Types: true, TypedOnly: true}, ordinaryOnlyTypedEntries, ordinaryOnlyTypedInputs)
	ordinaryOnlyPackage(t, dir, "mutated", strings.Replace(ordinaryOnlyTyped, `"+"`, `"-"`, 1), GenOptions{Types: true, TypedOnly: true}, ordinaryOnlyTypedEntries, ordinaryOnlyTypedInputs)
	out := runTranscripts(t, dir, "full", "only", "mutated")
	if out["full"] != out["only"] {
		t.Errorf("the typed-only parser differs from the full one:\n%s", firstTranscriptDifference(out["full"], out["only"]))
	}
	if !strings.Contains(out["only"], "syntax error") || !strings.Contains(out["only"], `"Text":"a"`) {
		t.Error("the transcript has neither a syntax error nor a value")
	}
	if out["mutated"] == out["only"] {
		t.Error("the transcripts of a mutated grammar are the same: the comparison cannot fail")
	}
}

// ordinaryOnlyConverted needs the Node result of its rules: an action reads a field of a struct, so the
// typed entry points convert the Node that the rule functions build instead of building the values
// directly.
const ordinaryOnlyConverted = `type Item = { Names []string; N int }
rule main Item = i:item "!" => Item{Names: $i.Names, N: len($i.Names)}
rule item Item = n:[a-z]+ => Item{Names: list(text($n)), N: 0}`

var ordinaryOnlyConvertedInputs = []string{"abc!", "!", "abc", "", "é!", "ab\xff!", "abc!x", "a!b!", "ab !"}

func TestGeneratedTypedOnlyConvertedEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	program := strings.NewReplacer(`"sum"`, `"main"`, `"a"`, `"item"`).Replace(ordinaryOnlyTypedEntries)
	g, err := syntax.Parse(ordinaryOnlyConverted)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(g, GenOptions{Package: "p", Module: "example.com/p", Start: "main", Runtime: RuntimeImport, Types: true, TypedOnly: true})
	if err != nil {
		t.Fatalf("a typed-only parser whose entries convert Nodes: %v", err)
	}
	src := string(mergeGenerated(files))
	if !strings.Contains(src, "RunParseConverted(") {
		t.Fatal("the entries do not convert a Node: the grammar tests nothing")
	}
	for _, absent := range []string{"func Parse(", "func ParseRule(", "func Recognize("} {
		if strings.Contains(src, absent) {
			t.Errorf("a typed-only parser has %q", absent)
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("ordtest")), 0o644); err != nil {
		t.Fatal(err)
	}
	ordinaryOnlyPackage(t, dir, "full", ordinaryOnlyConverted, GenOptions{Types: true, Recognize: true}, program, ordinaryOnlyConvertedInputs)
	ordinaryOnlyPackage(t, dir, "only", ordinaryOnlyConverted, GenOptions{Types: true, TypedOnly: true}, program, ordinaryOnlyConvertedInputs)
	ordinaryOnlyPackage(t, dir, "mutated", strings.Replace(ordinaryOnlyConverted, `"!"`, `"?"`, 1), GenOptions{Types: true, TypedOnly: true}, program, ordinaryOnlyConvertedInputs)
	out := runTranscripts(t, dir, "full", "only", "mutated")
	if out["full"] != out["only"] {
		t.Errorf("the typed-only parser differs from the full one:\n%s", firstTranscriptDifference(out["full"], out["only"]))
	}
	if !strings.Contains(out["only"], `"Names":["abc"]`) || !strings.Contains(out["only"], "syntax error") {
		t.Error("the transcript has neither a value nor a syntax error")
	}
	if out["mutated"] == out["only"] {
		t.Error("the transcripts of a mutated grammar are the same: the comparison cannot fail")
	}

	// Negative control: without the Node rule functions the same entries cannot be generated.
	_, err = Generate(g, GenOptions{Package: "p", Module: "example.com/p", Start: "main", Runtime: RuntimeImport, Types: true, TypedOnly: true, disableKeepNodes: true})
	if err == nil || !strings.Contains(err.Error(), "missing generated execution functions") {
		t.Errorf("a typed-only parser without the Node rule functions: error %v, want missing generated execution functions", err)
	}
}
