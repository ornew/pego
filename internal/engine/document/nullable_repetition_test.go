package document_test

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/ornew/zeroparse/internal/engine"
)

func TestNullableRepetitionResumedMinimum(t *testing.T) {
	const prefix = 1000
	p := mustCompile(t, fmt.Sprintf("rule main = item #repeat(%d, ) $$\nrule item = \"é\" / _", prefix+1))
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			d, err := p.NewDocumentWith("main", strings.Repeat("é", prefix), ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil || len(d.Runs()) == 0 {
				t.Fatalf("initial repetition not recorded: %v", err)
			}
			end := 1
			if unit == Bytes {
				end = len("é")
			}
			if err := d.Edit(0, end, ""); err != nil {
				t.Fatal(err)
			}
			got, err := d.Parse()
			want, werr := p.ParseWith("main", d.Text(), ParseOptions{Backend: backend, Unit: unit})
			if err == nil || resultJSON(got, err) != resultJSON(want, werr) {
				t.Errorf("resumed %s/%s: %v / %v", backend, unit, err, werr)
			}
			if d.Resumed() < prefix-10 {
				t.Errorf("tail was not reused: %d", d.Resumed())
			}
			if err := d.Edit(0, 0, "é"); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Errorf("undo %s/%s: %v", backend, unit, err)
			}
		}
	}
}
