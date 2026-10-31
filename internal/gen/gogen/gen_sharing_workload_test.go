package gogen_test

import (
	"os/exec"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedSharingLocalLayout(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedSharingLocalLayout)
}

func testGeneratedSharingLocalLayout(t *testing.T) {
	c := gentest.TypedLocalLayoutCases[0]
	if testing.Short() {
		return
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	gentest.GeneratedParsersCorpus(t, goBin, []gentest.Case{c}, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedLocalLayoutCases[:5], gen.GenOptions{DisableMethodSharing: true, DisableLiteralSharing: true})
}
