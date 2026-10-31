package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestMemberAccessOnJoinGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testMemberAccessOnJoinGeneratedGoParity)
}

func testMemberAccessOnJoinGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, append(gentest.JoinMemberCases(), gentest.TypedJoinMemberCases()...), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.JoinMemberCases(), gen.GenOptions{ConvertTypes: true}, 0)
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedJoinMemberCases(), gen.GenOptions{ConvertTypes: true}, 0)
}
