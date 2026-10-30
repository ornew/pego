package ops

import (
	"flag"
	"os"
	"testing"

	"github.com/ornew/pego/internal/ir/exec"
)

var update = flag.Bool("update", false, "update golden files")

// TestPilotGolden checks that the pilot declarations verify and that their listing is
// testdata/pilot.golden: a change of a contract, a precondition or a cost bound shows in review.
func TestPilotGolden(t *testing.T) {
	m := Pilot()
	if err := exec.Verify(m); err != nil {
		t.Fatal(err)
	}
	got := exec.String(m)
	const golden = "testdata/pilot.golden"
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
