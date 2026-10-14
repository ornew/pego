package pego_test

import (
	"context"
	"errors"
	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/engine"
	"testing"
)

func TestParsePrefixCursor(t *testing.T) {
	p, err := pego.CompileSource(`rule main = "(" inner ")" $$
rule inner = x:"é😀" "!" => $x
rule empty = x:"a"? => $x
rule nilvalue = -"é😀"
rule eof = "é😀" $$
rule bad = "z"
rule recovered = ("a" ";") #recover(skip=[^;]* ";")
rule recoverySuffix = "a" ";" #recover(skip=[^;]* ";")
type A struct { N int }
rule fatal = "a" => A{N: 1 / (len($1) - 1)}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
			for _, recognize := range []bool{false, true} {
				opts := []pego.ParseOption{pego.WithBackend(b), pego.WithUnit(unit)}
				if recognize {
					opts = append(opts, pego.RecognizeOnly())
				}
				want := 3
				if unit == pego.Bytes {
					want = 7
				}
				r, err := p.ParsePrefix("inner", "é😀!tail", opts...)
				if err != nil || r.Consumed != want || r.ConsumedBytes != 7 || recognize && r.Node != nil {
					t.Fatalf("%v/%v/%v: %+v %v", b, unit, recognize, r, err)
				}
				if !recognize && (r.Node == nil || int(r.Node.End) >= r.Consumed) {
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
				if err != nil || r.Node != nil || r.ConsumedBytes != 6 {
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
				var recovered pego.SyntaxErrors
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
				var referenceErrors pego.SyntaxErrors
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

func TestParsePrefixMalformedFragmentMapping(t *testing.T) {
	p, err := pego.CompileSource(`rule main = . .`, "main")
	if err != nil {
		t.Fatal(err)
	}
	source := "before\r\n😀\xffsuffix"
	start := len("before\r\n")
	fragment := source[start:]
	for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			opts := []pego.ParseOption{pego.WithBackend(b)}
			if recognize {
				opts = append(opts, pego.RecognizeOnly())
			}
			r, err := p.ParsePrefix("main", fragment, opts...)
			if err != nil || r.Consumed != 2 || r.ConsumedBytes != 5 {
				t.Fatalf("malformed: %+v %v", r, err)
			}
			off, err := pego.NewPositionMap(fragment).ByteOffsetFromUnit(r.Consumed, pego.PositionCodePoints)
			if err != nil || off != r.ConsumedBytes {
				t.Fatalf("fragment offset: %d %v", off, err)
			}
			pos, err := pego.NewPositionMap(source).Position(start+off, pego.PositionUTF16)
			if err != nil || pos != (pego.Position{Line: 1, Character: 3}) {
				t.Fatalf("absolute position: %+v %v", pos, err)
			}
		}
	}
}

func TestParsePrefixCancellation(t *testing.T) {
	p, err := pego.CompileSource(`rule main = "abc"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, err := p.ParsePrefix("main", "abctail", pego.WithBackend(b), pego.WithContext(ctx))
		if !errors.Is(err, context.Canceled) || r.Node != nil || r.Consumed != 0 || r.ConsumedBytes != 0 {
			t.Fatalf("cancelled: %+v %v", r, err)
		}
		r, err = p.ParsePrefix("main", "abctail", pego.WithBackend(b))
		if err != nil || r.ConsumedBytes != 3 {
			t.Fatalf("retry: %+v %v", r, err)
		}
	}
}
