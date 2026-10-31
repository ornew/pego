package document_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	. "github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/engine/document"
	"github.com/ornew/zeroparse/internal/gentest"
)

// memberDocumentDiffers parses each input of the case with a Document on every backend: twice
// without an edit, and, for each position, after deleting the character there and inserting it
// again, and compares each outcome with a parse of the text from scratch. It returns the
// differences, ignoring an input that exhausts a limit.
func memberDocumentDiffers(t *testing.T, c gentest.Case) []string {
	t.Helper()
	prog := mustCompile(t, c.Src)
	var diffs []string
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		o := ParseOptions{Backend: backend, Expectations: true}
		for _, in := range c.Inputs {
			want := resultJSON(prog.ParseWith("", in, o))
			if strings.Contains(want, "limit") || strings.Contains(want, "budget") {
				continue
			}
			check := func(d *Document, step string) {
				if got := resultJSON(d.Parse()); got != want {
					diffs = append(diffs, fmt.Sprintf("%s %q %s %s:\n got %s\nwant %s", c.Name, in, backend, step, got, want))
				}
			}
			d, err := prog.NewDocumentWith("", in, o)
			if err != nil {
				t.Fatal(err)
			}
			check(d, "first parse")
			check(d, "second parse")
			// The inputs are ASCII, so code points are bytes.
			for k := range len(in) {
				if err := d.Edit(k, k+1, ""); err != nil {
					t.Fatal(err)
				}
				d.Parse()
				if err := d.Edit(k, k, in[k:k+1]); err != nil {
					t.Fatal(err)
				}
				check(d, fmt.Sprintf("after deleting and inserting again %d", k))
				check(d, fmt.Sprintf("again after deleting and inserting again %d", k))
			}
		}
	}
	return diffs
}

// TestDocumentLeftRecursionMemberReparse checks that a Document parse of grammars whose left
// recursions store results that depend on the order of evaluation (leaders that read the seeds of
// other leaders, members that read leader results with recovered errors) gives the outcome of a
// parse from scratch, when parsed again without an edit and after edits.
func TestDocumentLeftRecursionMemberReparse(t *testing.T) {
	t.Parallel()
	cases := append(gentest.LeftRecursionMemberCases(), gentest.LeftRecursionMemberSeedCases()...)
	cases = append(cases, gentest.LeftRecursionMemberDocumentCases()...)
	for _, c := range cases {
		for _, d := range memberDocumentDiffers(t, c) {
			t.Error(d)
		}
	}
}

// TestDocumentLeftRecursionMemberReparseRandom is TestDocumentLeftRecursionMemberReparse on
// random grammars with cycles, with and without cuts and recovery.
func TestDocumentLeftRecursionMemberReparseRandom(t *testing.T) {
	t.Parallel()
	seeds, recovering := 150, 600
	if testing.Short() {
		seeds, recovering = 20, 60
	}
	var cases []gentest.Case
	for s := int64(0); s < int64(seeds); s++ {
		cases = append(cases, gentest.RandomCycleGrammar(s, false), gentest.RandomCycleGrammar(s, true))
	}
	for s := int64(0); s < int64(recovering); s++ {
		cases = append(cases, gentest.RandomRecoveringCycleGrammar(s))
	}
	compiled, n := 0, 0
	for _, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			continue
		}
		if _, err := Compile(g, compile.Options{}); err != nil {
			continue
		}
		compiled++
		for _, d := range memberDocumentDiffers(t, c) {
			if n++; n <= 5 {
				t.Errorf("%s\n%s", c.Src, d)
			}
		}
	}
	if n > 0 || compiled < len(cases)*8/10 {
		t.Errorf("%d differences in %d grammars", n, compiled)
	}
}

// TestDocumentLeftRecursionMemberReparseControl checks that the checks of
// TestDocumentLeftRecursionMemberReparse detect a later parse that reuses provisional results:
// with them kept, each of LeftRecursionMemberDocumentCases differs from a parse from scratch.
func TestDocumentLeftRecursionMemberReparseControl(t *testing.T) {
	defer document.KeepProvisional()()
	for _, c := range gentest.LeftRecursionMemberDocumentCases() {
		if len(memberDocumentDiffers(t, c)) == 0 {
			t.Errorf("%s: no difference with provisional results kept", c.Name)
		}
	}
}
