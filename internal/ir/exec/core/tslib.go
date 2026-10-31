package core

import (
	"fmt"
	"strings"

	"github.com/ornew/zeroparse/internal/ir/exec"
	"github.com/ornew/zeroparse/internal/ir/exec/exects"
	"github.com/ornew/zeroparse/internal/ir/exec/ops"
	"github.com/ornew/zeroparse/internal/ir/exec/pass"
)

// TSMethod is a printed method of the TypeScript runtime's Parser class.
type TSMethod struct {
	// Name is the method's name, Text its source, indented for the class body.
	Name, Text string
}

// tsSpecs are the methods of the TypeScript runtime that the compositions replace. The runtime's
// call observes before callBody, and it has neither Documents nor streams.
var tsSpecs = []struct {
	fn   string
	on   []string
	name string
}{
	{fn: "call", on: []string{ops.Observe}, name: "callBody"},
	{fn: "grow", name: "grow"},
}

// TSMethods returns the printed methods of the TypeScript runtime.
func TSMethods() ([]TSMethod, error) {
	m := Build()
	var out []TSMethod
	for _, s := range tsSpecs {
		f, err := pass.Specialize(m, m.Func(s.fn), s.fn, s.on)
		if err != nil {
			return nil, err
		}
		src, err := exects.Method(m, f, tsTarget)
		if err != nil {
			return nil, err
		}
		out = append(out, TSMethod{Name: s.name, Text: src})
	}
	return out, nil
}

// tsTarget is the TypeScript implementation of the operations in the runtime's Parser class.
var tsTarget = &exects.Target{
	Source: "internal/ir/exec/core",
	Packed: ops.Value,
	Handle: func(name string) string {
		return map[string]string{
			ops.Rule: "Rule", ops.Value: "R", ops.Entry: "MemoEntry | null", ops.Values: "Value[] | null",
			ops.Errs: "SyntaxError[]", ops.Kept: "number[]",
		}[name]
	},
	FuncName: func(name string) string { return map[string]string{"call": "callBody", "grow": "grow"}[name] },
	Reg: func(name string) string {
		return map[string]string{ops.Pos: "this.pos", ops.Silent: "this.silent"}[name]
	},
	Op: func(c *exec.Call, a []string) string {
		switch c.Callee {
		case "call.spend", "call.growStep":
			return "this.spend()"
		case "call.first":
			return "this.firstCall(" + a[0] + ")"
		case "rule.memo", "rule.leader", "rule.id":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "rule.")
		case "rule.plain":
			return a[0] + ".scope.length === 0"
		case "rule.invoke":
			return fmt.Sprintf("this.invoke(%s, %s)", a[0], a[1])
		case "rule.invokePlain":
			return fmt.Sprintf("this.invokePlainBody(%s, %s)", a[0], a[1])
		case "env.memoKey":
			return "this.envValues(" + a[0] + ".vars)"
		case "rec.mark":
			return "this.recovered.length"
		case "rec.truncate":
			return "this.recovered.length = " + a[0]
		case "rec.since":
			return fmt.Sprintf("this.recovered.length > %s ? (this.control === null ? this.recovered.slice(%s) : "+
				"copyErrorsSpend(this.recovered, %s, this.recovered.length, this.control)) : noErrs", a[0], a[0], a[0])
		case "rec.none":
			return a[0] + ".length === 0"
		case "exp.keep":
			return fmt.Sprintf("this.keep(%sFrom, %sTo)", a[0], a[0])
		case "exp.merge":
			return fmt.Sprintf("this.mergeExpected(%s, %sFrom, %sTo)", a[0], a[1], a[1])
		case "exp.mergeKept":
			return fmt.Sprintf("this.mergeKept(%s, %s)", a[0], a[1])
		case "memo.far", "memo.expected", "memo.ok", "memo.end", "memo.growing", "memo.silent", "memo.errs":
			return a[0] + "." + strings.TrimPrefix(c.Callee, "memo.")
		case "memo.value":
			return a[0] + ".node"
		case "memo.setExpected":
			return fmt.Sprintf("%s.far = %s;\n%s.expected = %s", a[0], a[1], a[0], a[2])
		case "memo.grown":
			return fmt.Sprintf("this.memoGet(%s, %s, %s, %s) as MemoEntry", a[0], a[1], a[2], a[3])
		case "memo.finishSeed":
			return fmt.Sprintf("%s.growing = false;\n%s.silent = %s", a[0], a[0], a[1])
		case "obs.memoHit":
			return "this.observation?.memoHit()"
		case "memo.detached":
			return "newEntry(undefined, false, 0, false, false, noErrs)"
		case "lr.stale":
			return fmt.Sprintf("this.memberStale(%s.member, %s)", a[0], a[1])
		case "lr.read":
			return "this.readResult(" + a[0] + ")"
		case "lr.depth":
			return "this.growths"
		case "lr.enter":
			return "this.enterCall()"
		case "lr.leave":
			return "this.leaveCall(" + a[0] + ")"
		case "lr.changeable":
			return "this.readDepth <= " + a[0]
		case "lr.end":
			return "this.growths--;\nthis.seedPos = " + a[0]
		case "memo.mark":
			return a[0] + ".depth = this.growths"
		case "memo.settle":
			return fmt.Sprintf("%s.unstable = this.readDepth <= this.growths && (%s.silent || %s.errs.length > 0)", a[0], a[0], a[0])
		}
		panic("core: no TypeScript implementation of " + c.Callee)
	},
	Stmt: func(c *exec.Call, a []string, dst []string, decl string) (string, bool) {
		lhs := func(name string) string {
			if decl != "" {
				return decl + " " + name
			}
			return name
		}
		key := func() string { return strings.Join(a[:4], ", ") }
		entry := func(e, node, ok, end, silent, growing, errs string) string {
			return fmt.Sprintf("%s = newEntry(%s, %s, %s, %s, %s, %s);\nthis.memoPut(%s, %s);", lhs(e), node, ok, end, silent, growing, errs, key(), e)
		}
		switch c.Callee {
		case "memo.lookup":
			s := fmt.Sprintf("%s = this.memoGet(%s);", lhs(dst[0]), key())
			if dst[1] != "_" {
				s += fmt.Sprintf("\n%s = %s !== null;", lhs(dst[1]), dst[0])
			}
			return s, true
		case "memo.store":
			return entry(dst[0], a[7], a[4], a[5], a[6], "false", a[8]), true
		case "memo.seed":
			return entry(dst[0], "undefined", "false", a[1], "false", "true", "noErrs"), true
		case "memo.reseed":
			return entry(dst[0], a[5], "true", a[4], "false", "true", a[6]), true
		case "lr.begin":
			return fmt.Sprintf("%s = this.seedPos;\nthis.growths++;\nthis.seedPos = %s;", lhs(dst[0]), a[0]), true
		case "rec.add":
			return fmt.Sprintf("for (const x of %s) {\n  this.spend();\n  this.recovered.push(x);\n}", a[0]), true
		case "exp.isolate":
			m := dst[0]
			return fmt.Sprintf("const %sFar = this.farthest;\nconst %sBase = this.expBase;\nthis.expBase = this.expTop;\nthis.farthest = %s;", m, m, a[0]), true
		case "exp.unisolate":
			far, set, m := dst[0], dst[1], a[0]
			return fmt.Sprintf("const %s = this.farthest;\nconst %sFrom = this.expBase;\nconst %sTo = this.expTop;\n"+
				"this.expTop = this.expBase;\nthis.expBase = %sBase;\nthis.farthest = %sFar;", far, set, set, m, m), true
		}
		return "", false
	},
}
