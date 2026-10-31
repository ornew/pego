package apitest_test

import (
	"errors"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/engine"
)

func TestParsePrefixCursor(t *testing.T) {
	p, err := compileTestSource(`rule main = "(" inner ")" $$
rule inner = x:"é😀" "!" => $x
rule empty = x:"a"? => $x
rule nilvalue = -"é😀"
rule eof = "é😀" $$
rule bad = "z"
rule recovered = ("a" ";") #recover(skip=[^;]* ";")
rule recoverySuffix = "a" ";" #recover(skip=[^;]* ";")
type A = { N int }
rule fatal = v1:"a" => A{N: 1 / (len($v1) - 1)}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
			for _, recognize := range []bool{false, true} {
				opts := []zp.ParseOption{zp.WithBackend(b), zp.WithUnit(unit)}
				if recognize {
					opts = append(opts, zp.RecognizeOnly())
				}
				want := 3
				if unit == zp.Bytes {
					want = 7
				}
				r, err := p.ParsePrefix("inner", "é😀!tail", opts...)
				if err != nil || r.Consumed != want || r.ConsumedBytes != 7 || recognize && r.Node != nil {
					t.Fatalf("%v/%v/%v: %+v %v", b, unit, recognize, r, err)
				}
				if !recognize && (r.Node == nil || int(r.Node.End()) >= r.Consumed) {
					t.Fatalf("action span must differ: %+v", r)
				}
				n, err := p.WithStart("inner")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = n.Parse("é😀!tail", opts...); err == nil {
					t.Fatal("whole-input Parse accepted suffix")
				}
				r, err = p.ParsePrefix("empty", "tail", opts...)
				if err != nil || r.Consumed != 0 || r.ConsumedBytes != 0 {
					t.Fatalf("empty: %+v %v", r, err)
				}
				r, err = p.ParsePrefix("nilvalue", "é😀tail", opts...)
				// A rule of dropped elements produces the empty tuple.
				if err != nil || recognize != (r.Node == nil) || !recognize && r.Node.String() != "()" || r.ConsumedBytes != 6 {
					t.Fatalf("discarded: %+v %v", r, err)
				}
				r, err = p.ParsePrefix("eof", "é😀", opts...)
				if err != nil || r.ConsumedBytes != 6 {
					t.Fatalf("explicit EOF success: %+v %v", r, err)
				}
				for _, name := range []string{"bad", "eof", "undefined", "recoverySuffix"} {
					r, err = p.ParsePrefix(name, "é😀tail", opts...)
					if err == nil || r.Node != nil || r.Consumed != 0 || r.ConsumedBytes != 0 {
						t.Fatalf("failure %s: %+v %v", name, r, err)
					}
				}
				r, err = p.ParsePrefix("recoverySuffix", "x;tail", opts...)
				if err == nil || r.Node != nil || r.Consumed != 0 || r.ConsumedBytes != 0 {
					t.Fatalf("failure before protected suffix: %+v %v", r, err)
				}
				r, err = p.ParsePrefix("recovered", "x;tail", opts...)
				var recovered zp.SyntaxErrors
				if !errors.As(err, &recovered) || r.Consumed != 2 || r.ConsumedBytes != 2 {
					t.Fatalf("recovered: %+v %v", r, err)
				}
				// The whole-input reference protects the same grouped expression.
				// The ungrouped recoverySuffix rule above fails before recovery.
				reference, refErr := p.WithStart("recovered")
				if refErr != nil {
					t.Fatal(refErr)
				}
				node, refErr := reference.Parse("x;", opts...)
				var referenceErrors zp.SyntaxErrors
				if !errors.As(refErr, &referenceErrors) || len(referenceErrors) != len(recovered) {
					t.Fatalf("whole-input recovery reference: %v", refErr)
				}
				if !recognize && (node == nil || r.Node == nil || node.String() != r.Node.String()) {
					t.Fatalf("prefix/reference nodes: %v / %v", r.Node, node)
				}
				if !recognize {
					r, err = p.ParsePrefix("fatal", "atail", opts...)
					if err == nil || r.Node != nil || r.Consumed != 0 || r.ConsumedBytes != 0 {
						t.Fatalf("fatal: %+v %v", r, err)
					}
				}
			}
		}
	}
	if p.Start() != "main" {
		t.Fatal("prefix changed start")
	}
	calls := 0
	_, err = p.ParsePrefix("inner", "é😀!", func(o *engine.ParseOptions) { calls++ })
	if err != nil || calls != 1 {
		t.Fatalf("options evaluated %d times: %v", calls, err)
	}
}
