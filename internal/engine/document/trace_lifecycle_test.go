package document_test

import (
	"errors"
	"fmt"
	"testing"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

func TestTraceLifecycleDocumentDetachment(t *testing.T) {
	prog := mustCompile(t, records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			for _, outcome := range []string{"success", "syntax", "trace-panic"} {
				t.Run(fmt.Sprintf("%s/%s/%s", b, u, outcome), func(t *testing.T) {
					text := "#records\na=1\nbc=22\n"
					if outcome == "syntax" {
						text += "bad=x\n"
					}
					var e TraceEvent
					token := &struct{ value string }{"trace panic"}
					armed := outcome == "trace-panic"
					opts := ParseOptions{Backend: b, Unit: u, Trace: func(v TraceEvent) {
						v.LineCol(v.Pos)
						v.Text()
						v.Failure()
						if v.Kind == TraceEnter && v.Rule == "main" {
							e = v
						}
						if armed && v.Kind == TraceExit && v.Rule == "record" && v.Matched {
							panic(token)
						}
					}}
					d, err := prog.NewDocumentWith("main", text, opts)
					if err != nil {
						t.Fatal(err)
					}
					var n *value.Node
					got := traceLifecycleRecover(func() { n, err = d.Parse() })
					switch outcome {
					case "success":
						if got != nil || err != nil || n == nil {
							t.Fatalf("node=%v err=%v panic=%v", n, err, got)
						}
					case "syntax":
						var se *SyntaxError
						if got != nil || n != nil || !errors.As(err, &se) {
							t.Fatalf("node=%v err=%v panic=%v", n, err, got)
						}
						if d.Memo().Len() == 0 {
							t.Fatal("syntax failure discarded reusable memo")
						}
					case "trace-panic":
						if got != token || n != nil || err != nil {
							t.Fatalf("node=%v err=%v panic=%v", n, err, got)
						}
						if d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
							t.Fatal("panic kept provisional document state")
						}
					}
					assertTraceParserDetached(t, core.TraceParser(e), outcome == "trace-panic")
					if e.Kind != TraceEnter || e.Rule != "main" || e.Depth != 1 || e.Pos != 0 {
						t.Fatal("saved event metadata changed")
					}
					var clone *value.Node
					var before string
					if n != nil {
						clone = n.Clone()
						before = resultJSON(clone, nil)
						if got, want := resultJSON(n, nil), resultJSON(prog.ParseWith("main", text, ParseOptions{Backend: b, Unit: u})); got != want {
							t.Fatal("returned document result differs")
						}
					}
					armed = false
					if outcome == "success" {
						if _, err := d.Parse(); err != nil || d.Stats().Reused == 0 {
							t.Fatal("unchanged document did not reuse memo")
						}
						assertTraceParserDetached(t, core.TraceParser(e), false)
					}
					fixed := "#records\nxy=3\n"
					if err := d.Edit(0, len(text), fixed); err != nil {
						t.Fatal(err)
					}
					if got, want := resultJSON(d.Parse()), resultJSON(prog.ParseWith("main", fixed, ParseOptions{Backend: b, Unit: u})); got != want {
						t.Fatalf("retry %s; fresh %s", got, want)
					}
					assertTraceParserDetached(t, core.TraceParser(e), false)
					if clone != nil && resultJSON(clone, nil) != before {
						t.Fatal("document teardown/retry changed cloned historical result")
					}
				})
			}
		}
	}
}
