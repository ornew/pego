package pego_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego"
)

func TestParsePrefixRuleSemantics(t *testing.T) {
	p, err := compileTestSource(`rule main = expr $$
rule expr = expr "+" "a" / "a"
rule cut = "a" #cut "b" / "a" "c"
rule nilaction = missing:"z"? "a" => $missing
rule recoveryThenFailure = ("a" #recover(skip="x")) "!"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
			for _, recognize := range []bool{false, true} {
				opts := []pego.ParseOption{pego.WithBackend(backend), pego.WithUnit(unit)}
				if recognize {
					opts = append(opts, pego.RecognizeOnly())
				}
				r, err := p.ParsePrefix("expr", "a+a+asuffix", opts...)
				if err != nil || r.Consumed != 5 || r.ConsumedBytes != 5 {
					t.Fatalf("left recursion %v/%v/%v: %+v %v", backend, unit, recognize, r, err)
				}
				reference, err := p.WithStart("expr")
				if err != nil {
					t.Fatal(err)
				}
				node, err := reference.Parse("a+a+a", opts...)
				if err != nil {
					t.Fatal(err)
				}
				if !recognize && (r.Node == nil || node == nil || r.Node.String() != node.String()) {
					t.Fatalf("left-recursive prefix/reference: %v / %v", r.Node, node)
				}
				r, err = p.ParsePrefix("cut", "actail", opts...)
				if err == nil || r != (pego.PrefixResult{}) {
					t.Fatalf("cut failure: %+v %v", r, err)
				}
				r, err = p.ParsePrefix("nilaction", "atail", opts...)
				if err != nil || r.Node != nil || r.Consumed != 1 || r.ConsumedBytes != 1 {
					t.Fatalf("nil action: %+v %v", r, err)
				}
				r, err = p.ParsePrefix("recoveryThenFailure", "x?tail", opts...)
				var syntax *pego.SyntaxError
				var recovered pego.SyntaxErrors
				if !errors.As(err, &syntax) || errors.As(err, &recovered) || r != (pego.PrefixResult{}) {
					t.Fatalf("unrecovered failure after recovery: %+v %v", r, err)
				}
			}
		}
	}
}

func TestParsePrefixCompiled(t *testing.T) {
	p, err := compileTestSource(`rule main = value $$
rule value = x:"é😀" "!" => $x`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []pego.CompiledMode{pego.CompiledParse, pego.CompiledRecognize} {
		data, err := p.Marshal(mode)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := pego.LoadParser(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []pego.Backend{pego.Bytecode, pego.BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				opts := []pego.ParseOption{pego.WithBackend(backend)}
				if recognize {
					opts = append(opts, pego.RecognizeOnly())
				}
				r, err := loaded.ParsePrefix("value", "é😀!tail", opts...)
				if recognize != (mode == pego.CompiledRecognize) {
					// Each mode refuses the operations of the other.
					if err == nil || !strings.Contains(err.Error(), "the compiled grammar is in "+mode.String()+" mode") {
						t.Fatalf("compiled %s mode %v/recognize=%v: %+v %v", mode, backend, recognize, r, err)
					}
					continue
				}
				if err != nil || r.Consumed != 3 || r.ConsumedBytes != 7 || recognize && r.Node != nil {
					t.Fatalf("compiled %s mode %v/recognize=%v: %+v %v", mode, backend, recognize, r, err)
				}
				// The module's configured start remains a whole-input parse.
				if _, err = loaded.Parse("é😀!tail", opts...); err == nil {
					t.Fatal("compiled whole-input parse accepted suffix")
				}
			}
		}
	}
}

func TestParsePrefixConcurrentAndReentrant(t *testing.T) {
	p, err := compileTestSource(`rule main = "z" $$
rule item = "é😀"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 192)
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for range 4 {
			wg.Add(1)
			go func(b pego.Backend) {
				defer wg.Done()
				for range 8 {
					r, err := p.ParsePrefix("item", "é😀tail", pego.WithBackend(b), pego.WithTrace(func(e pego.TraceEvent) {
						if e.Kind == pego.TraceEnter && e.Rule == "item" {
							inner, innerErr := p.ParsePrefix("item", "é😀other", pego.WithBackend(b), pego.WithUnit(pego.Bytes))
							if innerErr != nil || inner.Consumed != 6 || inner.ConsumedBytes != 6 {
								failures <- fmt.Errorf("nested: %+v %v", inner, innerErr)
							}
						}
					}))
					if err != nil || r.Consumed != 2 || r.ConsumedBytes != 6 {
						failures <- fmt.Errorf("concurrent: %+v %v", r, err)
						return
					}
				}
			}(backend)
		}
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if p.Start() != "main" {
		t.Fatal("prefix changed configured start")
	}
}

func TestParsePrefixRejectsDiagnosticCallback(t *testing.T) {
	parser, err := compileTestSource(`rule main = ("a" ";") #recover(skip=[^;]* ";")`, "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			for _, start := range []string{"main", "undefined"} {
				diagnosticCalls, traceCalls := 0, 0
				opts := []pego.ParseOption{
					pego.WithBackend(backend),
					pego.WithContext(ctx),
					pego.WithTrace(func(pego.TraceEvent) { traceCalls++ }),
					pego.WithDiagnosticCallback(func(*pego.SyntaxError) error { diagnosticCalls++; return nil }),
				}
				if recognize {
					opts = append(opts, pego.RecognizeOnly())
				}
				_, referenceErr := parser.Parse("x;", opts...)
				result, prefixErr := parser.ParsePrefix(start, "x;tail", opts...)
				if referenceErr == nil || prefixErr == nil || prefixErr.Error() != referenceErr.Error() || errors.Is(prefixErr, context.Canceled) || result != (pego.PrefixResult{}) {
					t.Fatalf("%v/recognize=%v/start=%s: result=%+v error=%v; Parse error=%v", backend, recognize, start, result, prefixErr, referenceErr)
				}
				if diagnosticCalls != 0 || traceCalls != 0 {
					t.Fatalf("invalid callback option executed user code: diagnostic=%d trace=%d", diagnosticCalls, traceCalls)
				}
			}
			// Disabling callback delivery preserves the recovered result/error API.
			opts := []pego.ParseOption{pego.WithBackend(backend), pego.WithDiagnosticCallback(nil)}
			if recognize {
				opts = append(opts, pego.RecognizeOnly())
			}
			result, prefixErr := parser.ParsePrefix("main", "x;tail", opts...)
			var recovered pego.SyntaxErrors
			if !errors.As(prefixErr, &recovered) || len(recovered) != 1 || result.Consumed != 2 || result.ConsumedBytes != 2 {
				t.Fatalf("nil callback: result=%+v error=%v", result, prefixErr)
			}
		}
	}
}
