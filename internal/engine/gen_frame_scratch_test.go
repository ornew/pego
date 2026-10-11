package engine

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGeneratedNodeFrameScratchCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"cut and lookahead": true, "left recursion": true, "recover": true,
		"fold and lists": true, "projected and indexed repetition": true,
		"runtime error": true, "indent": true, "restricted Pratt prefixes": true,
		"empty prefix and postfix": true,
		"typed/LR_element_frames":  true, "typed/LR_lookahead_memo": true,
		"typed/Pratt_environment_rollback": true, "typed/Pratt_lookahead_memo": true,
		"typed/recovery_capture_environment": true, "typed/recovery_cut_scope": true,
	}
	cases := append([]genCase(nil), nodeDirectCases...)
	for _, c := range genCorpus(t) {
		if wanted[c.name] {
			cases = append(cases, c)
			delete(wanted, c.name)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing selected capture cases: %v", wanted)
	}
	for _, reference := range []bool{false, true} {
		name := "scratch"
		if reference {
			name = "reference"
		}
		t.Run(name, func(t *testing.T) {
			testGeneratedParsersCorpus(t, goBin, cases, GenOptions{disableNodeFrameScratch: reference})
		})
	}
}

// Typed parsing keeps its independent arenas; its source must be unaffected.
func TestGeneratedNodeFrameScratchTypedSource(t *testing.T) {
	g, err := syntax.Parse(nodeFrameScratchGrammar)
	if err != nil {
		t.Fatal(err)
	}
	optimized, err := Generate(g, GenOptions{Package: "frames", Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := Generate(g, GenOptions{Package: "frames", Start: "main", Types: true, Recognize: true, disableNodeFrameScratch: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(optimized), "tparse(trules") || !strings.Contains(string(reference), "const nodeFrameScratch = false") {
		t.Fatal("missing typed route or static reference")
	}
	if string(optimized) != strings.Replace(string(reference), "const nodeFrameScratch = false", "const nodeFrameScratch = true", 1) {
		t.Fatal("reference changed more than the Node allocation control")
	}
}
