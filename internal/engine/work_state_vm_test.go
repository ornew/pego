//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/limits"
)

func TestWorkStateVMChildAssembly(t *testing.T) {
	for _, op := range []Op{OpSeq, OpEndRepeat} {
		for _, lim := range []limits.WorkLimits{{MaxSteps: 64}, {MaxSteps: 10000}} {
			p := stateWorkParser(lim)
			start := 3
			retained := &Node{text: "retained", fields: Fields{{Name: "keep", Value: "unchanged"}}}
			p.vals = append(p.vals, &start)
			want := make([]*Node, 320)
			for i := range want {
				if i%3 != 0 {
					want[i] = retained
				}
				p.vals = append(p.vals, nodeOrNil(want[i]))
			}
			instruction := Instr{Op: op, A: 320}
			if op == OpEndRepeat {
				instruction.A = 1
				p.reps = []repState{{base: 1, count: 320}}
			}
			vm := &vmProgram{m: &Module{Code: []Instr{instruction, {Op: OpEnd}}}}
			body := vmBody{}
			var result *Node
			err := catchFatal(t, func() { _, result, _ = p.step(vm, &body, false, false, nil) })
			if lim.MaxSteps == 64 {
				if !errors.Is(err, limits.ErrWorkLimit) || result != nil || len(p.vals) != 321 {
					t.Fatalf("%v assembly interruption err=%v result=%v stack=%d", op, err, result, len(p.vals))
				}
			} else if err != nil || result == nil || !reflect.DeepEqual(result.children, want) || result.start != 3 {
				t.Fatalf("%v assembled children/position differs: %v", op, err)
			}
			if retained.text != "retained" || retained.fields[0].Value != "unchanged" {
				t.Fatal("assembly changed retained node")
			}
		}
	}
}

func TestWorkStateVMCutAndFailureUnwind(t *testing.T) {
	for _, cut := range []bool{false, true} {
		p := stateWorkParser(limits.WorkLimits{MaxSteps: 64})
		for range 320 {
			p.ents = append(p.ents, vmEntry{kind: eChoice, cut: true})
		}
		vm := &vmProgram{m: &Module{Code: []Instr{{Op: OpCut}, {Op: OpEnd}}}}
		if cut {
			for i := range p.ents {
				p.ents[i].kind = eLabel
			}
			p.ents[0].kind = eChoice
			p.ents[0].cut = false
		}
		err := catchFatal(t, func() {
			if cut {
				body := vmBody{}
				p.step(vm, &body, false, false, nil)
			} else {
				p.unwind(vm.m, 0)
			}
		})
		if !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("cut=%t err=%v", cut, err)
		}
		if cut && p.ents[0].cut {
			t.Fatal("cut reached its target after budget exhaustion")
		}
		if !cut && len(p.ents) != 256 {
			t.Fatalf("unwind retained %d entries, want256", len(p.ents))
		}
		p.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
		if err := catchFatal(t, func() {
			if cut {
				body := vmBody{}
				p.step(vm, &body, false, false, nil)
			} else {
				p.unwind(vm.m, 0)
			}
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cut=%t cancellation=%v", cut, err)
		}
	}
}

func TestWorkStateOperatorsRuleFrameSnapshotAndRestoration(t *testing.T) {
	for _, iterative := range []bool{false, true} {
		for _, limit := range []uint64{128, 449, 10000} {
			p := stateWorkParser(limits.WorkLimits{MaxSteps: limit})
			retained := &Node{text: "retained", fields: Fields{{Name: "keep", Value: "unchanged"}}}
			before, _ := json.Marshal(retained)
			op := &operatorsRuleOp{kind: grammar.Prefix, line: &operatorsRuleLine{scope: &scope{names: make([]string, 320)}}}
			setup := func(f *frame) {
				for i := range f.vals {
					f.vals[i] = retained
				}
				p.trail = []undo{{f: f, slot: 0, old: nil}}
				p.pos = 1
			}
			var best operatorsRuleAttempt
			err := catchFatal(t, func() {
				if iterative {
					f := &longestFrame{started: true, ops: []*operatorsRuleOp{op}, f: p.newFrame(320)}
					setup(f.f)
					f.next(p, nil, iresult{ok: true, v: retained})
					best = p.attempt
				} else {
					op.line.m = func(p *parser) (*Node, bool) { setup(p.frame); return retained, true }
					best, _ = p.longest([]*operatorsRuleOp{op})
				}
			})
			if limit < 10000 {
				if !errors.Is(err, limits.ErrWorkLimit) || best.op != nil {
					t.Fatalf("iterative=%t limit=%d err=%v partial=%v", iterative, limit, err, best.op)
				}
				if p.control.stepsUsed != limit {
					t.Fatalf("snapshot/restore boundary used=%d want%d", p.control.stepsUsed, limit)
				}
			} else if err != nil || best.op != op || best.frame.vals[0] != retained {
				t.Fatalf("iterative=%t winning captures lost: %v", iterative, err)
			}
			after, _ := json.Marshal(retained)
			if string(before) != string(after) {
				t.Fatal("OperatorsRule frame copy changed public node")
			}
		}
	}
}

func TestWorkStateProjectedFrameClear(t *testing.T) {
	const count = 320
	var items []grammar.Expr
	for i := range count {
		items = append(items, &grammar.Capture{Name: fmt.Sprintf("f%d", i), Expr: &grammar.Atomic{Expr: &grammar.Top{}}})
	}
	c := &compiler{}
	m := c.projectRepeat(&grammar.Repeat{Expr: &grammar.Seq{Items: items}, Min: 0, Max: 1}, "f0")
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { m(p) }); !errors.Is(err, limits.ErrWorkLimit) || len(p.trail) != 0 {
		t.Fatalf("projected clear err=%v capture writes=%d", err, len(p.trail))
	}
}

func TestWorkStateBatchRollbackAndPoolRetry(t *testing.T) {
	var body strings.Builder
	body.WriteString(`c0:"a" `)
	for i := 1; i < 320; i++ {
		fmt.Fprintf(&body, "c%d:_ ", i)
	}
	var predicate strings.Builder
	predicate.WriteString(`text($c0)=="a"`)
	for i := 1; i < 320; i++ {
		fmt.Fprintf(&predicate, ` && text($c%d)==""`, i)
	}
	src := `rule main = (` + body.String() + ` {` + predicate.String() + `} arm "!" / fallback) #recover(skip=.*) $$
rule fallback = .* #atomic
rule arm = _`
	prog := compile(t, src)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				armed := false
				fallback := 0
				var observed *parser
				n, err := prog.ParseWith("main", "a", ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
					observed = e.p
					if e.Kind == TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if e.Kind == TraceExit && e.Rule == "arm" && e.Matched {
						if len(e.p.trail) < 320 {
							t.Fatalf("%v/%v recognize=%t: outer capture trail=%d", b, u, recognize, len(e.p.trail))
						}
						armed = true
						e.p.control.limits.MaxSteps = e.p.control.stepsUsed + 20
					}
				}})
				if !armed || n != nil || !errors.Is(err, limits.ErrWorkLimit) || fallback != 0 || observed == nil || observed.scratch != nil || observed.control != nil {
					t.Fatalf("%v/%v recognize=%t: armed=%t returned=%t err=%v fallback=%d", b, u, recognize, armed, n != nil, err, fallback)
				}
				n, err = prog.ParseWith("main", "a", ParseOptions{Backend: b, Unit: u, Recognize: recognize})
				if err != nil || (recognize && n != nil) || (!recognize && n == nil) {
					t.Fatalf("%v/%v retry: %v", b, u, err)
				}
			}
		}
	}
}
