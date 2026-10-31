//go:build !zp_budget_controls_reference

package apitest_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
	"github.com/ornew/zeroparse/internal/engine"
)

func TestMissingTokensPublicOptions(t *testing.T) {
	p, err := compileTestSource(`rule main = ";" #missing $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]zp.ParseOption{nil, {zp.WithMissingTokens(0)}, {zp.WithMissingTokens(2), zp.WithMissingTokens(0)}} {
		if n, err := p.Parse("", options...); n != nil || err == nil {
			t.Fatal("disabled insertion accepted input", n, err)
		}
	}
	calls := 0
	base := zp.WithMissingTokens(2)
	counted := zp.ParseOption(func(o *engine.ParseOptions) { calls++; base(o) })
	if _, err := p.Parse(";", counted); err != nil || calls != 1 {
		t.Fatal("options did not resolve exactly once", calls, err)
	}
	n, err := p.Parse("", zp.WithMissingTokens(-1), zp.WithMissingTokens(2))
	var recovered zp.SyntaxErrors
	if n == nil || !errors.As(err, &recovered) || len(recovered) != 1 {
		t.Fatal(n, err)
	}
	if _, err := p.Parse(";", zp.WithMissingTokens(2), zp.WithMissingTokens(-1)); err == nil || !strings.Contains(err.Error(), "nonnegative") {
		t.Fatal(err)
	}
	if _, err := p.Parse(";", zp.WithMissingTokens(-1)); err == nil {
		t.Fatal("invalid limit accepted")
	}
	if n, err := p.Parse("", zp.RecognizeOnly(), zp.WithMissingTokens(2)); n != nil || !errors.As(err, &recovered) {
		t.Fatal("recognition", n, err)
	}
	if n, err := p.Parse(";", zp.WithMissingTokens(2)); n == nil || err != nil {
		t.Fatal("ordinary match", n, err)
	}
}

func TestMissingTokensConcurrentReentrantAndRetry(t *testing.T) {
	p, err := compileTestSource(`rule main = ";" #missing $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				n, err := p.Parse("", zp.WithMissingTokens(1))
				var recovered zp.SyntaxErrors
				if n == nil || !errors.As(err, &recovered) || len(recovered) != 1 {
					t.Error(n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	entered := false
	_, err = p.Parse("", zp.WithMissingTokens(1), zp.WithTrace(func(zp.TraceEvent) {
		if entered {
			return
		}
		entered = true
		n, err := p.Parse("", zp.WithMissingTokens(1))
		var recovered zp.SyntaxErrors
		if n == nil || !errors.As(err, &recovered) {
			t.Error("reentrant invocation shared a ledger", err)
		}
	}))
	var recovered zp.SyntaxErrors
	if !entered || !errors.As(err, &recovered) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Parse("", zp.WithMissingTokens(1), zp.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n, err := p.Parse("", zp.WithMissingTokens(1)); n == nil || !errors.As(err, &recovered) {
		t.Fatal("cancelled invocation poisoned retry", n, err)
	}
}

type missingUnusableReader struct{ calls int }

func (r *missingUnusableReader) Read([]byte) (int, error) {
	r.calls++
	panic("unsupported entry read input")
}

func TestMissingTokensUnsupportedPublicPaths(t *testing.T) {
	p, err := compileTestSource(`rule main = (";" #missing)* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []zp.Backend{zp.Bytecode, zp.BytecodeIterative} {
		for _, limit := range []int{0, 2} {
			if n, err := p.Parse(";", zp.WithBackend(backend), zp.WithMissingTokens(limit)); n != nil || err == nil || !strings.Contains(err.Error(), "not supported") {
				t.Fatal(n, err)
			}
		}
	}
	r := &missingUnusableReader{}
	if err := p.ParseStream(r, func(*zp.Node) error { t.Fatal("unsupported stream emitted"); return nil }, zp.WithMissingTokens(2)); err == nil {
		t.Fatal("stream accepted insertion")
	}
	if it, err := p.Iterate(r, zp.WithMissingTokens(2)); it != nil || err == nil {
		t.Fatal(it, err)
	}
	if r.calls != 0 {
		t.Fatal("rejected stream touched reader")
	}
	if d, err := p.NewDocument(";", zp.WithMissingTokens(2)); d != nil || err == nil {
		t.Fatal(d, err)
	}
	if _, err := p.MarshalBinary(); err == nil {
		t.Fatal("binary serialization silently discarded #missing")
	}
	if _, err := (gen.Go{Module: "example"}).Generate(p.Grammar(), "main"); err == nil {
		t.Fatal("Go generator silently discarded #missing")
	}
	if _, err := (gen.TypeScript{}).Generate(p.Grammar(), "main"); err == nil {
		t.Fatal("TS generator silently discarded #missing")
	}
	strict, err := compileTestSource(`rule main = ";" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.Parse(";", zp.WithBackend(zp.Bytecode), zp.WithMissingTokens(1)); err == nil {
		t.Fatal("unsupported VM enabled mode accepted")
	}
	d, err := strict.NewDocument(";")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := d.Parse(zp.WithMissingTokens(1)); n != nil || err == nil {
		t.Fatal("existing Document enabled insertion", n, err)
	}
}
