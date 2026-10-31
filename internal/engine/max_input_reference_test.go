//go:build zp_budget_controls_reference

package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
)

// The reference build has no limits: a finite MaxInput is unavailable, and zero and Unlimited run.
func TestMaxInputUnavailableInReferenceBuild(t *testing.T) {
	prog := mustCompile(t, `rule main = [a-z]* $$`)
	for _, max := range []uint64{0, limits.Unlimited} {
		if _, err := prog.ParseWith("main", "abc", ParseOptions{Limits: limits.Limits{MaxInput: max}}); err != nil {
			t.Errorf("MaxInput %d: %v", max, err)
		}
	}
	_, err := prog.ParseWith("main", "abc", ParseOptions{Limits: limits.Limits{MaxInput: 100}})
	if err == nil || !strings.Contains(err.Error(), "unavailable in the reference build") {
		t.Errorf("finite MaxInput: error %v", err)
	}
}
