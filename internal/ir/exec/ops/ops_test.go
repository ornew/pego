package ops

import (
	"flag"
	"os"
	"testing"

	"github.com/ornew/pego/internal/ir/exec"
)

var update = flag.Bool("update", false, "update golden files")

// TestPilotGolden checks that the pilot declarations, and those with the transitional operations,
// verify and that their listings are testdata/pilot.golden and testdata/transitional.golden: a change
// of a contract, a precondition or a cost bound shows in review.
func TestPilotGolden(t *testing.T) {
	withTransitional := Pilot()
	Transitional(withTransitional)
	for golden, m := range map[string]*exec.Module{"testdata/pilot.golden": Pilot(), "testdata/transitional.golden": withTransitional} {
		if err := exec.Verify(m); err != nil {
			t.Fatal(err)
		}
		got := exec.String(m)
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("listing differs from %s; run go test -run TestPilotGolden -update", golden)
		}
	}
}
