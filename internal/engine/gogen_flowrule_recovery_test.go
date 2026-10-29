package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedTypedRecoveryBodies(t *testing.T) {
	t.Parallel()
	gentest.NeedsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated code")
	}
	marker := "// Recovery inlined under the runtime-owned frame."
	for _, c := range gentest.TypedRecoveryCases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts := gen.GenOptions{Package: "recoverybody", Start: "main", Types: true, Recognize: true}
		candidate, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !strings.Contains(string(candidate), "TParse(trules") || !strings.Contains(string(candidate), marker) {
			t.Fatalf("%s: typed recovery route not exercised", c.Name)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedRecoveryCases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedRecoveryCases, gen.GenOptions{ConvertTypes: true})
}
