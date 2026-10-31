package apitest_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

func TestPublicStreamDiagnosticCallbackOrderAndAbort(t *testing.T) {
	p, err := compileTestSource(`rule main = item+ #stream $$
rule item = "ab" @error(message="record") #recover(skip=[^a]+)`, "main")
	if err != nil {
		t.Fatal(err)
	}
	const input = "abX!abY?ab"
	for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				var events []string
				var copies []*zp.SyntaxError
				options := []zp.ParseOption{zp.WithExpectations(), zp.WithBackend(backend), zp.WithUnit(unit), zp.WithDiagnosticCallback(func(e *zp.SyntaxError) error {
					events = append(events, "diagnostic")
					copies = append(copies, e)
					if len(e.Messages) != 1 || e.Messages[0] != "record" {
						t.Fatalf("callback diagnostic: %+v", e)
					}
					e.Messages[0] = "changed"
					return nil
				})}
				err := p.ParseStream(strings.NewReader(input), func(*zp.Node) error {
					events = append(events, "element")
					return nil
				}, options...)
				want := []string{"element", "diagnostic", "element", "element", "diagnostic", "element", "element"}
				if !reflect.DeepEqual(events, want) {
					t.Fatalf("delivery order: got %v want %v", events, want)
				}
				var recovered zp.SyntaxErrors
				if !errors.As(err, &recovered) || len(recovered) != 2 || len(copies) != 2 {
					t.Fatalf("recovered: %v, copies=%d", err, len(copies))
				}
				for i, e := range recovered {
					if e.Messages[0] != "record" || e == copies[i] || &e.Messages[0] == &copies[i].Messages[0] {
						t.Fatalf("callback changed retained diagnostic %d: %+v", i, e)
					}
				}

				stop := errors.New("diagnostic stop")
				var stopped []string
				abort := []zp.ParseOption{zp.WithExpectations(), zp.WithBackend(backend), zp.WithUnit(unit), zp.WithDiagnosticCallback(func(*zp.SyntaxError) error {
					stopped = append(stopped, "diagnostic")
					return stop
				})}
				err = p.ParseStream(strings.NewReader(input), func(*zp.Node) error {
					stopped = append(stopped, "element")
					return nil
				}, abort...)
				if err != stop || !reflect.DeepEqual(stopped, []string{"element", "diagnostic"}) {
					t.Fatalf("callback error emitted recovered element: events=%v err=%v", stopped, err)
				}

				it, err := p.Iterate(strings.NewReader(input), abort...)
				if err != nil {
					t.Fatal(err)
				}
				if n, ok, err := it.Next(); n == nil || !ok || err != nil {
					t.Fatalf("iterator first element: node=%v ok=%v err=%v", n, ok, err)
				}
				for range 2 {
					if n, ok, err := it.Next(); n != nil || ok || err != stop {
						t.Fatalf("iterator callback error: node=%v ok=%v err=%v", n, ok, err)
					}
				}
			})
		}
	}
}

func TestPublicStreamDiagnosticCallbackIsStreamOnly(t *testing.T) {
	p, err := compileTestSource(`rule main = "a"* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	callback := zp.WithDiagnosticCallback(func(*zp.SyntaxError) error { return nil })
	if _, err := p.Parse("a", callback); err == nil {
		t.Fatal("batch Parse accepted a stream diagnostic callback")
	}
	if _, err := p.NewDocument("a", callback); err == nil {
		t.Fatal("NewDocument accepted a stream diagnostic callback")
	}
	d, err := p.NewDocument("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Parse(callback); err == nil {
		t.Fatal("Document.Parse accepted a stream diagnostic callback")
	}
	if _, err := p.Parse("a", zp.WithDiagnosticCallback(nil)); err != nil {
		t.Fatalf("nil callback should be disabled: %v", err)
	}
}
