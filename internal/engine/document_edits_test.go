package engine

import (
	"fmt"
	"github.com/ornew/pego/internal/gentest"
	"math/rand"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego/internal/engine/core"
)

//go:noinline
func documentVersionForCollection(t *testing.T, finalized chan<- struct{}) DocumentVersion {
	t.Helper()
	p := mustCompile(t, `rule main = .*`)
	d, err := p.NewDocument("main", strings.Repeat("x", 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetFinalizer(d, func(*Document) { finalized <- struct{}{} })
	return d.Version()
}

func TestDocumentVersionDoesNotRetainDocument(t *testing.T) {
	t.Parallel()
	finalized := make(chan struct{}, 1)
	version := documentVersionForCollection(t, finalized)
	defer runtime.KeepAlive(version)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		select {
		case <-finalized:
			if version.Sequence() != 1 {
				t.Fatal("retained token changed")
			}
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("version token retained its document")
}

func batchOracle(text string, unit PositionUnit, edits []TextEdit) string {
	if unit == Bytes {
		var out strings.Builder
		pos := 0
		for _, e := range edits {
			out.WriteString(text[pos:e.Start])
			out.WriteString(e.Text)
			pos = e.End
		}
		out.WriteString(text[pos:])
		return out.String()
	}
	input := []rune(text)
	out := []rune{}
	pos := 0
	for _, e := range edits {
		out = append(out, input[pos:e.Start]...)
		out = append(out, []rune(e.Text)...)
		pos = e.End
	}
	out = append(out, input[pos:]...)
	return string(out)
}

func TestDocumentBatchFreshParity(t *testing.T) {
	t.Parallel()
	replacements := []string{"", "a", "\n", "é", "😀", "\xc3", "\xa9", "\xff", "@name\n", "x = 1+2\n"}
	for gi, grammar := range []string{`rule main = item* $$
rule item = .`, gentest.IncrementalGrammar, `rule main = ("a" @error("need a") #recover(skip=.))* $$`} {
		original := mustCompile(t, grammar)
		programs := []*Program{original}
		loaded := saveLoad(t, original, "main", CompiledDocument)
		programs = append(programs, loaded)
		for pi, p := range programs {
			for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if pi == 1 && back == Closure {
					_, err := p.NewDocumentWith("main", "", ParseOptions{Backend: back})
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, large := range []bool{false, true} {
						t.Run(fmt.Sprintf("%d/%d/%s/%s/%v", gi, pi, back, unit, large), func(t *testing.T) {
							initial := "é\xc3x\xa9\n"
							if large {
								initial += strings.Repeat("a", core.DocumentPieceMinSize)
							}
							d, err := p.NewDocumentWith("main", initial, ParseOptions{Backend: back, Unit: unit})
							if err != nil {
								t.Fatal(err)
							}
							rng := rand.New(rand.NewSource(39))
							for i := 0; i < 60; i++ {
								// Include an ordinary edit not yet parsed when the batch is captured.
								if i%3 == 0 {
									if err := d.Edit(0, 0, "a"); err != nil {
										t.Fatal(err)
									}
								}
								before := d.Text()
								positions := decodedBoundaries(before)
								if unit == CodePoints {
									for j := range positions {
										positions[j] = j
									}
								}
								var edits []TextEdit
								at := 0
								for j := 0; j < 3 && at < len(positions); j++ {
									start := at + rng.Intn(len(positions)-at)
									end := start + rng.Intn(min(3, len(positions)-start))
									edits = append(edits, TextEdit{Start: positions[start], End: positions[end], Text: replacements[rng.Intn(len(replacements))]})
									at = end + 1
								}
								wantText := batchOracle(before, unit, edits)
								base := d.Version()
								if err := d.EditBatch(base, edits...); err != nil {
									t.Fatal(err)
								}
								if d.Text() != wantText || d.Version().Sequence() != base.Sequence()+1 {
									t.Fatal("original-coordinate text/version mismatch")
								}
								got, err := d.Parse()
								// A document-mode program parses whole texts only when traced.
								want, werr := p.ParseWith("main", wantText, servedBy(CompiledDocument, ParseOptions{Backend: back, Unit: unit}))
								if dump(t, got, err) != dump(t, want, werr) {
									t.Fatalf("batch%d: %q got%s want%s", i, wantText, dump(t, got, err), dump(t, want, werr))
								}
							}
						})
					}
				}
			}
		}
	}
}
