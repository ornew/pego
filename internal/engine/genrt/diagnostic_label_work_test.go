package genrt

import (
	"errors"
	"testing"
)

func TestDiagnosticRenameAccountsForLargeExpectationSets(t *testing.T) {
	ids := make([]ExpID, 512)
	for i := range ids {
		ids[i] = ExpID(i + 1)
	}
	p := &Parser{Control: newWorkControl(nil, WorkLimits{MaxSteps: 64})}
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				f, ok := v.(fatal)
				if !ok {
					panic(v)
				}
				got = f.err
			}
		}()
		RenamedExpected(p, ids, 999)
	}()
	if !errors.Is(got, ErrWorkLimit) {
		t.Fatalf("rename work limit = %v, want ErrWorkLimit", got)
	}
	if p.Control.stepsUsed() != 64 {
		t.Fatalf("rename steps = %d, want 64", p.Control.stepsUsed())
	}
}
