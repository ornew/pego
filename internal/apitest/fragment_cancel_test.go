//go:build !pego_work_controls_reference

package apitest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego"
)

func TestParsePrefixCancellation(t *testing.T) {
	p, err := compileTestSource(`rule main = "abc"`, "main")
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
