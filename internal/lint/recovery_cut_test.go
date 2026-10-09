package lint

import (
	"strings"
	"testing"
)

func TestRecoverySkipCutCanReachFallback(t *testing.T) {
	for _, expr := range []string{
		`(_|_ #recover(skip=(-- "x")))?`,
		`(_|_ #recover(skip=(-- "x")))*`,
		`((_|_ #recover(skip=(-- "x"))) / _)`,
	} {
		_, findings := lintSource(t, "def main = "+expr+` / ""`, Options{})
		for _, f := range findings {
			if f.Check == CheckShadowedAlt && strings.Contains(f.Message, "never tried") {
				t.Errorf("reachable fallback reported dead for %s: %s", expr, f)
			}
		}
	}
}

func TestGuaranteedDeadSuffixDoesNotChangeLeftRecursion(t *testing.T) {
	for _, prior := range []string{`""`, `empty`, `empty #recover(skip=empty)`} {
		_, findings := lintSource(t, "def main = r2\ndef r1 = r2?\ndef r2 = ((\"a\" / r1 / \"\\n\") / ("+prior+") / r2){0,1}\ndef empty = \"\"", Options{})
		found := false
		for _, f := range findings {
			if f.Check == CheckShadowedAlt && strings.Contains(f.Message, "never tried") {
				found = true
				if strings.Contains(f.Fix, "left-recursive") {
					t.Errorf("compiler-proven unreachable suffix has a recursion caveat: %s", f.Fix)
				}
			}
		}
		if !found {
			t.Errorf("no dead-suffix finding for %s", prior)
		}
	}
}
