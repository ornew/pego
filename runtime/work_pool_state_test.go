package runtime

import "testing"

// TestFirstCallChargeIndependentOfReusedStorage checks that the steps charged for the first-call
// bookkeeping of a controlled parse are the same on a fresh parser and on one that a parse released
// with its storage, for dense and sparse tables.
func TestFirstCallChargeIndependentOfReusedStorage(t *testing.T) {
	for _, stride := range []int{10, 65, 130} {
		run := func(p *Parser, n int) uint64 {
			p.Control = newWorkControl(nil, WorkLimits{MaxSteps: 1 << 40})
			p.n, p.memo.stride, p.memo.sparse = n, stride, stride > 64
			for _, pos := range []int{0, 1, 63, 64, 1024, 1025, 3000, n - 1} {
				p.Pos = pos
				for _, r := range []int{0, stride / 2, stride - 1} {
					p.FirstCall(&Rule{Seen: r})
				}
			}
			return p.Control.stepsUsed()
		}
		const n = 4096
		fresh := run(&Parser{}, n)
		p := &Parser{}
		run(p, 2*n) // a larger parse leaves bigger storage and set bits
		p.release()
		if got := run(p, n); got != fresh {
			t.Errorf("stride %d: reused parser charged %d steps, fresh %d", stride, got, fresh)
		}
		p.release()
		if got := run(p, n); got != fresh {
			t.Errorf("stride %d: parser reused twice charged %d steps, fresh %d", stride, got, fresh)
		}
	}
}
