package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/syntax"
)

func metadataWideSource(n int, invalid bool) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "// Definition %d\ntype T%d = { Text string }\n", i, i)
		if i == 0 {
			b.WriteString("@start\n")
		}
		fmt.Fprintf(&b, "rule r%d T%d = ", i, i)
		if invalid {
			b.WriteString("missing\n")
		} else {
			fmt.Fprintf(&b, "t:(\"a\") #atomic => T%d{Text: text($t)}\n", i)
		}
	}
	return b.String()
}
func metadataSources(tb testing.TB) []semanticSourceCase {
	return append(semanticRepresentativeSources(tb),
		semanticSourceCase{"wide512", metadataWideSource(512, false)},
		semanticSourceCase{"partial512", metadataWideSource(512, false) + "rule unfinished = (\n"},
		semanticSourceCase{"compile-errors512", metadataWideSource(512, true)},
		semanticSourceCase{"docs512", "@start\n" + strings.Repeat("// Documentation line\n", 512) + "rule main = \"a\"\n"},
	)
}
func metadataPrepared(tb testing.TB, text string) *analysis {
	tb.Helper()
	a := newAnalysis(text)
	toks, err := syntax.TokenizeContext(context.Background(), text)
	if err != nil {
		tb.Fatal(err)
	}
	for _, tok := range toks {
		start, end := a.idx.zpOffset(tok.Pos), a.idx.zpOffset(tok.End)
		a.tokAt[start] = len(a.toks)
		a.toks = append(a.toks, token{tok, start, end})
	}
	a.g, a.syntaxErrs, err = syntax.ParsePartialContext(context.Background(), text)
	if err != nil {
		tb.Fatal(err)
	}
	return a
}

// Prepared syntax is immutable; each metadata invocation owns its maps and slices.
func metadataClone(seed *analysis) *analysis {
	a := newAnalysisWithIndex(seed.text, seed.idx)
	a.toks, a.tokAt, a.g, a.syntaxErrs = seed.toks, seed.tokAt, seed.g, seed.syntaxErrs
	return a
}
func metadataFingerprint(tb testing.TB, a *analysis) []byte {
	tb.Helper()
	type defDTO struct {
		Kind                                    symKind
		Name, Doc                               string
		NameStart, NameEnd, Start, End, Keyword int
		RuleStatement, TypeStatement            int
	}
	type occDTO struct {
		Kind                   symKind
		Name                   string
		Start, End, Definition int
	}
	type semDTO struct {
		Type semTokenType
		Mods int
	}
	type tokDTO struct {
		Token      syntax.Token
		Start, End int
	}
	ruleStatements := map[*grammar.RuleDef]int{}
	typeStatements := map[grammar.TypeDecl]int{}
	for i, stmt := range a.g.File.Decls {
		switch stmt := stmt.(type) {
		case *grammar.RuleDef:
			ruleStatements[stmt] = i
		case grammar.TypeDecl:
			typeStatements[stmt] = i
		}
	}
	defs := make([]defDTO, len(a.defs))
	ids := map[*definition]int{}
	for i, d := range a.defs {
		ids[d] = i
		ri, ti := -1, -1
		if d.rule != nil {
			var ok bool
			ri, ok = ruleStatements[d.rule]
			if !ok {
				tb.Fatal("rule definition points outside grammar statements")
			}
		}
		if d.typ != nil {
			var ok bool
			ti, ok = typeStatements[d.typ]
			if !ok {
				tb.Fatal("type definition points outside grammar statements")
			}
		}
		defs[i] = defDTO{d.kind, d.name, d.doc, d.nameStart, d.nameEnd, d.start, d.end, d.kwStart, ri, ti}
	}
	definitionIndex := func(d *definition) int {
		tb.Helper()
		id, ok := ids[d]
		if !ok {
			tb.Fatal("reference points outside analysis definitions")
		}
		return id
	}
	occs := make([]occDTO, len(a.occs))
	for i, o := range a.occs {
		id := -1
		if o.def != nil {
			id = definitionIndex(o.def)
		}
		occs[i] = occDTO{o.kind, o.name, o.start, o.end, id}
	}
	rules, types := map[string]int{}, map[string]int{}
	for n, d := range a.rules {
		rules[n] = definitionIndex(d)
	}
	for n, d := range a.types {
		types[n] = definitionIndex(d)
	}
	sem := map[int]semDTO{}
	for k, v := range a.sem {
		sem[k] = semDTO{v.typ, v.mods}
	}
	toks := make([]tokDTO, len(a.toks))
	for i, t := range a.toks {
		toks[i] = tokDTO{t.Token, t.start, t.end}
	}
	out, err := json.Marshal(struct {
		Grammar      any
		Program      bool
		Tokens       []tokDTO
		TokenIndex   map[int]int
		Starts       []bool
		Definitions  []defDTO
		Occurrences  []occDTO
		Rules, Types map[string]int
		Semantic     map[int]semDTO
		Variables    map[string]bool
		SyntaxErrors syntax.ErrorList
		Diagnostics  []Diagnostic
	}{a.g, a.prog != nil, toks, a.tokAt, a.declarationStarts, defs, occs, rules, types, sem, a.vars, a.syntaxErrs, a.diags})
	if err != nil {
		tb.Fatal(err)
	}
	return out
}

// This observable corpus also runs against the exact parent implementation.
func TestMetadataHistoricalFingerprints(t *testing.T) {
	sources := append(semanticResponseSources(t), metadataSources(t)[2:]...)
	sources = append(sources, semanticSourceCase{"duplicate-reserved", "rule main = \"a\"\nrule main = \"b\"\nrule _reserved = \"x\"\n"})
	for _, s := range sources {
		a, err := analyzeContext(context.Background(), s.text)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("metadata %s %x", s.name, sha256.Sum256(metadataFingerprint(t, a)))
	}
}

var metadataBenchmarkResult *analysis

func BenchmarkAnalysisMetadata(b *testing.B) {
	for _, s := range metadataSources(b) {
		seed := metadataPrepared(b, s.text)
		for _, full := range []bool{true, false} {
			stage := "metadata"
			if full {
				stage = "full"
			}
			for _, live := range []bool{false, true} {
				mode := "ordinary"
				if live {
					mode = "cancellable"
				}
				b.Run(s.name+"/"+stage+"/"+mode, func(b *testing.B) {
					ctx := context.Background()
					if live {
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(ctx)
						defer cancel()
					}
					b.ReportAllocs()
					for b.Loop() {
						var a *analysis
						var err error
						if full {
							a, err = analyzeContext(ctx, s.text)
						} else {
							a = metadataClone(seed)
							err = metadataBenchmarkCollect(ctx, a)
						}
						if err != nil {
							b.Fatal(err)
						}
						metadataBenchmarkResult = a
					}
				})
			}
		}
	}
}
