package vm

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/compiletest"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/value"
)

// Completed calls can leave references beyond slice length. Stream commits
// must release them at the next prune without disturbing active root values.
func TestStreamStorageCommitReferences(t *testing.T) {
	prog := &core.SharedProgram{}
	core.InitSharedProgram(prog, compiletest.Program(t, `rule main = "a"`, compile.Options{SourceInfo: true}))
	p := core.NewParser(prog, strings.Repeat("a", 4096), core.CodePoints, false)
	live := value.Ptr(value.Data{Text: "live"})
	dead := value.Ptr(value.Data{Text: "dead"})
	root := p.NewFrame(1)
	root.Vals[0] = live
	p.Frame = root
	p.Trail = make([]core.Undo, 3)
	p.Trail[0] = core.Undo{F: root, Old: live}
	p.Trail[2] = core.Undo{F: &core.Frame{Vals: []*value.Node{dead}}, Old: dead}
	p.Trail = p.Trail[:1]
	machineOf(p).vals = []any{live, dead, dead}
	machineOf(p).vals = machineOf(p).vals[:1]
	p.Commit(1023)
	if len(p.Trail) != 0 || p.Pruned != 0 || machineOf(p).vals[0] != live || root.Vals[0] != live {
		t.Fatal("early commit damaged live state or pruned too soon")
	}
	p.Commit(1024)
	if p.Pruned != 1024 || machineOf(p).vals[0] != live || root.Vals[0] != live {
		t.Fatal("pruning damaged live root or active VM value")
	}
	if core.StreamUndoCleanup {
		for _, u := range p.Trail[:cap(p.Trail)] {
			if u.F != nil || u.Old != nil {
				t.Fatal("inactive undo entry retained a completed value")
			}
		}
	}
	if core.StreamStackCleanup {
		for _, v := range machineOf(p).vals[len(machineOf(p).vals):cap(machineOf(p).vals)] {
			if v != nil {
				t.Fatal("inactive VM stack entry retained a completed value")
			}
		}
	}
}

func TestVMStreamRepeatShape(t *testing.T) {
	code := compiletest.Program(t, `rule main = (x:"x" #atomic)* #stream $$`, compile.Options{SourceInfo: true}).BytecodeModule().Code
	ip := -1
	for i, in := range code {
		if in.Op == bytecode.OpRepeat {
			ip = i
			break
		}
	}
	if ip < 0 {
		t.Fatal("missing repetition")
	}
	vm := &Program{m: &bytecode.Module{Code: code}}
	if !vm.isStreamRepeat(ip) {
		t.Fatal("stream repetition not recognized")
	}
	for _, target := range []int32{-1, 0, int32(ip + 2), int32(len(code)), int32(len(code) + 1)} {
		copied := append([]bytecode.Instr(nil), code...)
		copied[ip+1].A = target
		vm.m = &bytecode.Module{Code: copied}
		if vm.isStreamRepeat(ip) {
			t.Fatalf("invalid target %d recognized as stream", target)
		}
	}
	for _, defect := range []string{"iterator", "end", "next", "flag"} {
		copied := append([]bytecode.Instr(nil), code...)
		end := int(copied[ip+1].A)
		switch defect {
		case "iterator":
			copied[ip+1].Op = bytecode.OpNext
		case "end":
			copied[end].Op = bytecode.OpNext
		case "next":
			copied[end-1].Op = bytecode.OpIter
		case "flag":
			copied[end-1].B = 1
		}
		vm.m = &bytecode.Module{Code: copied}
		if vm.isStreamRepeat(ip) {
			t.Fatalf("invalid %s recognized as stream", defect)
		}
	}
}
