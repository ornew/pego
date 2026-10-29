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
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkStateVMChildAssembly(t *testing.T) {
	for _, op := range []bytecode.Op{bytecode.OpSeq, bytecode.OpEndRepeat} {
		for _, lim := range []limits.WorkLimits{{MaxSteps: 64}, {MaxSteps: 10000}} {
			p := stateWorkParser(lim)
			start := 3
			retained := value.Ptr(value.Data{Text: "retained", Fields: value.Fields{{Name: "keep", Value: "unchanged"}}})
			vmMachineOf(p).vals = append(vmMachineOf(p).vals, &start)
			want := make([]*value.Node, 320)
			for i := range want {
				if i%3 != 0 {
					want[i] = retained
				}
				vmMachineOf(p).vals = append(vmMachineOf(p).vals, core.NodeOrNil(want[i]))
			}
			instruction := bytecode.Instr{Op: op, A: 320}
			if op == bytecode.OpEndRepeat {
				instruction.A = 1
				vmMachineOf(p).reps = []repState{{base: 1, count: 320}}
			}
			vm := &vmProgram{m: &bytecode.Module{Code: []bytecode.Instr{instruction, {Op: bytecode.OpEnd}}}}
			body := vmBody{}
			var result *value.Node
			err := catchFatal(t, func() { _, result, _ = vmMachineOf(p).step(vm, &body, false, false, nil) })
			if lim.MaxSteps == 64 {
				if !errors.Is(err, limits.ErrWorkLimit) || result != nil || len(vmMachineOf(p).vals) != 321 {
					t.Fatalf("%v assembly interruption err=%v result=%v stack=%d", op, err, result, len(vmMachineOf(p).vals))
				}
			} else if err != nil || result == nil || !reflect.DeepEqual(value.D(result).Children, want) || value.D(result).Start != 3 {
				t.Fatalf("%v assembled children/position differs: %v", op, err)
			}
			if value.D(retained).Text != "retained" || value.D(retained).Fields[0].Value != "unchanged" {
				t.Fatal("assembly changed retained node")
			}
		}
	}
}

func TestWorkStateVMCutAndFailureUnwind(t *testing.T) {
	for _, cut := range []bool{false, true} {
		p := stateWorkParser(limits.WorkLimits{MaxSteps: 64})
		for range 320 {
			vmMachineOf(p).ents = append(vmMachineOf(p).ents, vmEntry{kind: eChoice, cut: true})
		}
		vm := &vmProgram{m: &bytecode.Module{Code: []bytecode.Instr{{Op: bytecode.OpCut}, {Op: bytecode.OpEnd}}}}
		if cut {
			for i := range vmMachineOf(p).ents {
				vmMachineOf(p).ents[i].kind = eLabel
			}
			vmMachineOf(p).ents[0].kind = eChoice
			vmMachineOf(p).ents[0].cut = false
		}
		err := catchFatal(t, func() {
			if cut {
				body := vmBody{}
				vmMachineOf(p).step(vm, &body, false, false, nil)
			} else {
				vmMachineOf(p).unwind(vm.m, 0)
			}
		})
		if !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("cut=%t err=%v", cut, err)
		}
		if cut && vmMachineOf(p).ents[0].cut {
			t.Fatal("cut reached its target after budget exhaustion")
		}
		if !cut && len(vmMachineOf(p).ents) != 256 {
			t.Fatalf("unwind retained %d entries, want256", len(vmMachineOf(p).ents))
		}
		p.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
		if err := catchFatal(t, func() {
			if cut {
				body := vmBody{}
				vmMachineOf(p).step(vm, &body, false, false, nil)
			} else {
				vmMachineOf(p).unwind(vm.m, 0)
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
			retained := value.Ptr(value.Data{Text: "retained", Fields: value.Fields{{Name: "keep", Value: "unchanged"}}})
			before, _ := json.Marshal(retained)
			op := &core.OperatorsRuleOp{Kind: grammar.Prefix, Line: &core.OperatorsRuleLine{Scope: &compile.Scope{Names: make([]string, 320)}}}
			setup := func(f *core.Frame) {
				for i := range f.Vals {
					f.Vals[i] = retained
				}
				p.Trail = []core.Undo{{F: f, Slot: 0, Old: nil}}
				p.Pos = 1
			}
			var best core.OperatorsRuleAttempt
			err := catchFatal(t, func() {
				if iterative {
					f := &longestFrame{started: true, ops: []*core.OperatorsRuleOp{op}, f: p.NewFrame(320)}
					setup(f.f)
					f.next(ivmMachineOf(p), nil, iresult{ok: true, v: retained})
					best = ivmMachineOf(p).attempt
				} else {
					op.Line.M = func(p *core.Parser) (*value.Node, bool) { setup(p.Frame); return retained, true }
					best, _ = p.Longest([]*core.OperatorsRuleOp{op})
				}
			})
			if limit < 10000 {
				if !errors.Is(err, limits.ErrWorkLimit) || best.Op != nil {
					t.Fatalf("iterative=%t limit=%d err=%v partial=%v", iterative, limit, err, best.Op)
				}
				if p.Control.StepsUsed != limit {
					t.Fatalf("snapshot/restore boundary used=%d want%d", p.Control.StepsUsed, limit)
				}
			} else if err != nil || best.Op != op || best.Frame.Vals[0] != retained {
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
	c := newClosureWalker(&compile.Compiler{}, false)
	m := c.ProjectRepeat(&grammar.Repeat{Expr: &grammar.Seq{Items: items}, Min: 0, Max: 1}, "f0")
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { m(p) }); !errors.Is(err, limits.ErrWorkLimit) || len(p.Trail) != 0 {
		t.Fatalf("projected clear err=%v capture writes=%d", err, len(p.Trail))
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
	prog := mustCompile(t, src)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				armed := false
				fallback := 0
				var observed *core.Parser
				n, err := prog.ParseWith("main", "a", ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
					observed = core.TraceParser(e)
					if e.Kind == TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if e.Kind == TraceExit && e.Rule == "arm" && e.Matched {
						if len(core.TraceParser(e).Trail) < 320 {
							t.Fatalf("%v/%v recognize=%t: outer capture trail=%d", b, u, recognize, len(core.TraceParser(e).Trail))
						}
						armed = true
						core.TraceParser(e).Control.Limits.MaxSteps = core.TraceParser(e).Control.StepsUsed + 20
					}
				}})
				if !armed || n != nil || !errors.Is(err, limits.ErrWorkLimit) || fallback != 0 || observed == nil || observed.Scratch != nil || observed.Control != nil {
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
