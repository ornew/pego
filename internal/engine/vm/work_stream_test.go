//go:build !pego_work_controls_reference

package vm

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkStreamDiscardAndStackClear(t *testing.T) {
	for _, u := range []core.PositionUnit{core.Bytes, core.CodePoints} {
		t.Run(u.String(), func(t *testing.T) {
			in := core.Input{Unit: u, Eof: true, BaseLine: 1, BaseCol: 1, Lines: []int{0}, Control: core.NewWorkControl(nil, limits.Limits{MaxWork: 128})}
			if u == core.Bytes {
				in.Bs = []byte(strings.Repeat("a", 258))
			} else {
				in.In = []rune(strings.Repeat("a", 258))
			}
			got := catchStreamPanic(func() { in.Discard(129) })
			if _, ok := got.(core.Fatal); !ok || in.Base != 0 || in.Control.Used != 128 {
				t.Fatalf("discard interruption=%v base=%d steps=%d", got, in.Base, in.Control.Used)
			}
		})
	}
	for _, phase := range []string{"trail", "values"} {
		t.Run(phase, func(t *testing.T) {
			node := value.Ptr(value.Data{Text: "owned"})
			p := &core.Parser{Input: core.Input{Unit: core.Bytes, Eof: true, Bs: []byte(strings.Repeat("a", 3000)), BaseLine: 1, BaseCol: 1, Lines: []int{0}, Control: core.NewWorkControl(nil, limits.Limits{MaxWork: 953 + 128})}, Memo: core.NewMemoTable()}
			if phase == "trail" {
				p.Trail = make([]core.Undo, 0, 129)
			} else {
				machineOf(p).vals = make([]any, 1, 130)
				machineOf(p).vals[0] = node
				for i := 1; i < cap(machineOf(p).vals); i++ {
					machineOf(p).vals[:cap(machineOf(p).vals)][i] = node
				}
			}
			got := catchStreamPanic(func() { p.Commit(2048) })
			if _, ok := got.(core.Fatal); !ok || p.Base != 2047 || p.Pruned != 0 || value.D(node).Text != "owned" {
				t.Fatalf("commit %s interruption=%v base=%d pruned=%d", phase, got, p.Base, p.Pruned)
			}
			if phase == "values" && (machineOf(p).vals[0] != node || machineOf(p).vals[:cap(machineOf(p).vals)][129] != node) {
				t.Fatal("clear crossed reservation or live values")
			}
		})
	}
}
