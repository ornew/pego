package gentest

import (
	"os"
	"os/exec"
	"testing"

	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

// GoVetAndTest runs go vet and, if test is set, go test in the module in dir.
func GoVetAndTest(t *testing.T, dir string, test bool) {
	t.Helper()
	args := [][]string{{"vet", "./..."}}
	if test {
		args = append(args, []string{"test", "-count=1", "./..."})
	}
	for _, a := range args {
		cmd := exec.Command("go", a...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", a[0], err, out)
		}
	}
}
