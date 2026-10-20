package engine

import (
	"os/exec"
	"testing"
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
		"runtime error": true, "indent": true, "restricted OperatorsRule prefixes": true,
		"empty prefix and postfix": true,
		"typed/LR_element_frames":  true, "typed/LR_lookahead_memo": true,
		"typed/OperatorsRule_environment_rollback": true, "typed/OperatorsRule_lookahead_memo": true,
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
	testGeneratedParsersCorpus(t, goBin, cases, GenOptions{})
}
