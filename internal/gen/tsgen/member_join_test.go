package tsgen_test

import (
	"os/exec"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestMemberAccessOnJoinGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	gentest.TSParsersCorpus(t, node, append(gentest.JoinMemberCases(), gentest.TypedJoinMemberCases()...), gen.GenOptions{})
}
