package apitest_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

func TestPublicStreamDiagnosticTail(t *testing.T) {
	p, err := compileTestSource(`rule main = item+ #stream $$
rule item = "ab" @error(message="record") #recover(skip=[^a]+)`, "main")
	if err != nil {
		t.Fatal(err)
	}
	const input = "abX!abY?abZ#abW!ab"
	for _, backend := range []zeroparse.Backend{zeroparse.Closure, zeroparse.Bytecode, zeroparse.BytecodeIterative} {
		for _, unit := range []zeroparse.PositionUnit{zeroparse.CodePoints, zeroparse.Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				base := []zeroparse.ParseOption{zeroparse.WithBackend(backend), zeroparse.WithUnit(unit)}
				allErr := p.ParseStream(strings.NewReader(input), nil, base...)
				var all zeroparse.SyntaxErrors
				if !errors.As(allErr, &all) || len(all) != 4 {
					t.Fatalf("legacy recoveries: %v", allErr)
				}
				zeroErr := p.ParseStream(strings.NewReader(input), nil, append(base, zeroparse.WithDiagnosticLimit(0))...)
				if _, truncated := zeroErr.(*zeroparse.TruncatedSyntaxErrors); truncated {
					t.Fatalf("zero limit truncated: %v", zeroErr)
				}
				var zero zeroparse.SyntaxErrors
				if !errors.As(zeroErr, &zero) || !reflect.DeepEqual(zero, all) {
					t.Fatalf("zero limit changed result: %v", zeroErr)
				}

				var events []string
				var seen []*zeroparse.SyntaxError
				opts := append(base, zeroparse.WithDiagnosticLimit(2), zeroparse.WithDiagnosticCallback(func(e *zeroparse.SyntaxError) error {
					events = append(events, "diagnostic")
					seen = append(seen, e)
					return nil
				}))
				err := p.ParseStream(strings.NewReader(input), func(*zeroparse.Node) error {
					events = append(events, "element")
					return nil
				}, opts...)
				var truncated *zeroparse.TruncatedSyntaxErrors
				var retained zeroparse.SyntaxErrors
				if !errors.As(err, &truncated) || !errors.As(err, &retained) {
					t.Fatalf("truncated error does not unwrap: %T %v", err, err)
				}
				if truncated.Total != 4 || truncated.Dropped != 2 || truncated.CountsSaturated ||
					!reflect.DeepEqual(retained, all[2:]) || !reflect.DeepEqual(truncated.Errors, retained) ||
					!strings.Contains(err.Error(), "truncated") {
					t.Fatalf("bounded tail: %+v, retained=%v", truncated, retained)
				}
				if len(seen) != len(all) || !reflect.DeepEqual(seen, []*zeroparse.SyntaxError(all)) {
					t.Fatalf("callback lost committed recoveries: got=%v want=%v", seen, all)
				}
				if !reflect.DeepEqual(events, []string{
					"element", "diagnostic", "element", "element", "diagnostic", "element",
					"element", "diagnostic", "element", "element", "diagnostic", "element", "element",
				}) {
					t.Fatalf("callback order: %v", events)
				}
				for _, limit := range []int{1, 4, 8} {
					err := p.ParseStream(strings.NewReader(input), nil, append(base, zeroparse.WithDiagnosticLimit(limit))...)
					var got zeroparse.SyntaxErrors
					if !errors.As(err, &got) || !reflect.DeepEqual(got, all[max(0, len(all)-limit):]) {
						t.Fatalf("limit %d: %v", limit, err)
					}
					_, isTruncated := err.(*zeroparse.TruncatedSyntaxErrors)
					if isTruncated != (limit < len(all)) {
						t.Fatalf("limit %d wrapper: %T", limit, err)
					}
				}
			})
		}
	}
}

func TestPublicStreamDiagnosticTailIteratorAndSuffix(t *testing.T) {
	p, err := compileTestSource(`rule main = item* #stream ("b" #recover(skip="x")) $$
rule item = "a" #recover(skip="!")`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []zeroparse.Backend{zeroparse.Closure, zeroparse.Bytecode, zeroparse.BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			var events []string
			it, err := p.Iterate(strings.NewReader("!!x"), zeroparse.WithBackend(backend), zeroparse.WithDiagnosticLimit(1), zeroparse.WithDiagnosticCallback(func(*zeroparse.SyntaxError) error {
				events = append(events, "diagnostic")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, ok, err := it.Next(); !ok || err != nil {
					t.Fatalf("element: ok=%v err=%v", ok, err)
				}
				events = append(events, "element")
			}
			_, ok, terminal := it.Next()
			var truncated *zeroparse.TruncatedSyntaxErrors
			if ok || !errors.As(terminal, &truncated) || truncated.Total != 3 || truncated.Dropped != 2 || len(truncated.Errors) != 1 {
				t.Fatalf("terminal: ok=%v err=%v", ok, terminal)
			}
			if !reflect.DeepEqual(events, []string{"diagnostic", "element", "diagnostic", "element", "diagnostic"}) {
				t.Fatalf("suffix order: %v", events)
			}
			if _, ok, again := it.Next(); ok || again != terminal {
				t.Fatal("iterator changed terminal truncated error")
			}
		})
	}
}

func TestPublicStreamDiagnosticLimitCallbackStopsBeforeElement(t *testing.T) {
	p, err := compileTestSource(`rule main = item+ #stream $$
rule item = "ab" #recover(skip="!")`, "main")
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	for _, backend := range []zeroparse.Backend{zeroparse.Closure, zeroparse.Bytecode, zeroparse.BytecodeIterative} {
		elements := 0
		err := p.ParseStream(strings.NewReader("!ab"), func(*zeroparse.Node) error {
			elements++
			return nil
		}, zeroparse.WithBackend(backend), zeroparse.WithDiagnosticLimit(1), zeroparse.WithDiagnosticCallback(func(*zeroparse.SyntaxError) error {
			return stop
		}))
		if err != stop || elements != 0 {
			t.Fatalf("%v callback failure emitted element: count=%d err=%v", backend, elements, err)
		}
	}
}

func TestPublicStreamDiagnosticLimitValidation(t *testing.T) {
	p, err := compileTestSource(`rule main = "a"* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{-1, 0, 1} {
		opt := zeroparse.WithDiagnosticLimit(limit)
		if _, err := p.Parse("a", opt); err == nil {
			t.Fatalf("Parse accepted limit %d", limit)
		}
		if _, err := p.NewDocument("a", opt); err == nil {
			t.Fatalf("NewDocument accepted limit %d", limit)
		}
		d, err := p.NewDocument("a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(opt); err == nil {
			t.Fatalf("Document.Parse accepted limit %d", limit)
		}
		if _, err := d.Snapshot(opt); err == nil {
			t.Fatalf("Document.Snapshot accepted limit %d", limit)
		}
	}
	if err := p.ParseStream(strings.NewReader("a"), nil, zeroparse.WithDiagnosticLimit(-1)); err == nil {
		t.Fatal("ParseStream accepted negative limit")
	}
	if it, err := p.Iterate(strings.NewReader("a"), zeroparse.WithDiagnosticLimit(-1)); it != nil || err == nil {
		t.Fatalf("Iterate accepted negative limit: it=%v err=%v", it, err)
	}
}
