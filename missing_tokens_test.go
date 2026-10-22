//go:build !pego_work_controls_reference

package pego_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/engine"
)

func TestMissingTokensPublicOptions(t *testing.T) {
	p, err := compileTestSource(`rule main = ";" #missing $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]pego.ParseOption{nil, {pego.WithMissingTokens(0)}, {pego.WithMissingTokens(2), pego.WithMissingTokens(0)}} {
		if n, err := p.Parse("", options...); n != nil || err == nil {
			t.Fatal("disabled insertion accepted input", n, err)
		}
	}
	calls := 0
	base := pego.WithMissingTokens(2)
	counted := pego.ParseOption(func(o *engine.ParseOptions) { calls++; base(o) })
	if _, err := p.Parse(";", counted); err != nil || calls != 1 {
		t.Fatal("options did not resolve exactly once", calls, err)
	}
	n, err := p.Parse("", pego.WithMissingTokens(-1), pego.WithMissingTokens(2))
	var recovered pego.SyntaxErrors
	if n == nil || !errors.As(err, &recovered) || len(recovered) != 1 {
		t.Fatal(n, err)
	}
	if _, err := p.Parse(";", pego.WithMissingTokens(2), pego.WithMissingTokens(-1)); err == nil || !strings.Contains(err.Error(), "nonnegative") {
		t.Fatal(err)
	}
	if _, err := p.Parse(";", pego.WithMissingTokens(-1)); err == nil {
		t.Fatal("invalid limit accepted")
	}
	if n, err := p.Parse("", pego.RecognizeOnly(), pego.WithMissingTokens(2)); n != nil || !errors.As(err, &recovered) {
		t.Fatal("recognition", n, err)
	}
	if n, err := p.Parse(";", pego.WithMissingTokens(2)); n == nil || err != nil {
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
				n, err := p.Parse("", pego.WithMissingTokens(1))
				var recovered pego.SyntaxErrors
				if n == nil || !errors.As(err, &recovered) || len(recovered) != 1 {
					t.Error(n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	entered := false
	_, err = p.Parse("", pego.WithMissingTokens(1), pego.WithTrace(func(pego.TraceEvent) {
		if entered {
			return
		}
		entered = true
		n, err := p.Parse("", pego.WithMissingTokens(1))
		var recovered pego.SyntaxErrors
		if n == nil || !errors.As(err, &recovered) {
			t.Error("reentrant invocation shared a ledger", err)
		}
	}))
	var recovered pego.SyntaxErrors
	if !entered || !errors.As(err, &recovered) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Parse("", pego.WithMissingTokens(1), pego.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n, err := p.Parse("", pego.WithMissingTokens(1)); n == nil || !errors.As(err, &recovered) {
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
	for _, backend := range []pego.Backend{pego.Bytecode, pego.BytecodeIterative} {
		for _, limit := range []int{0, 2} {
			if n, err := p.Parse(";", pego.WithBackend(backend), pego.WithMissingTokens(limit)); n != nil || err == nil || !strings.Contains(err.Error(), "not supported") {
				t.Fatal(n, err)
			}
		}
	}
	r := &missingUnusableReader{}
	if err := p.ParseStream(r, func(*pego.Node) error { t.Fatal("unsupported stream emitted"); return nil }, pego.WithMissingTokens(2)); err == nil {
		t.Fatal("stream accepted insertion")
	}
	if it, err := p.Iterate(r, pego.WithMissingTokens(2)); it != nil || err == nil {
		t.Fatal(it, err)
	}
	if r.calls != 0 {
		t.Fatal("rejected stream touched reader")
	}
	if d, err := p.NewDocument(";", pego.WithMissingTokens(2)); d != nil || err == nil {
		t.Fatal(d, err)
	}
	if _, err := p.MarshalBinary(); err == nil {
		t.Fatal("binary serialization silently discarded #missing")
	}
	if _, err := pego.GenerateGo(p.Grammar(), "example", "main"); err == nil {
		t.Fatal("Go generator silently discarded #missing")
	}
	if _, err := pego.GenerateTypeScript(p.Grammar(), "main"); err == nil {
		t.Fatal("TS generator silently discarded #missing")
	}
	strict, err := compileTestSource(`rule main = ";" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.Parse(";", pego.WithBackend(pego.Bytecode), pego.WithMissingTokens(1)); err == nil {
		t.Fatal("unsupported VM enabled mode accepted")
	}
	d, err := strict.NewDocument(";")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := d.Parse(pego.WithMissingTokens(1)); n != nil || err == nil {
		t.Fatal("existing Document enabled insertion", n, err)
	}
}
