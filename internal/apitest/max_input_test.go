//go:build !pego_budget_controls_reference

package apitest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func wantInputLimit(t *testing.T, what string, err error, limit, length uint64) {
	t.Helper()
	var le *pego.LimitError
	if !errors.As(err, &le) || !errors.Is(err, pego.ErrLimit) {
		t.Fatalf("%s: error %v, want a LimitError", what, err)
	}
	if le.Kind != pego.LimitInput || le.Limit != limit || le.Requested != length {
		t.Errorf("%s: %+v, want kind LimitInput, limit %d, length %d", what, *le, limit, length)
	}
}

// Limits.MaxInput bounds the input of every public entry point in the position unit: zero and
// Unlimited set no bound, and an input over it fails with LimitInput and its length.
func TestMaxInputPublic(t *testing.T) {
	p, err := pego.CompileSource(`@start
rule main = [a-zé]* $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := pego.CompileSource(`@start
rule main = line* #stream $$
rule line = [a-z]+ "\n"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	text := "ééé" // 3 code points, 6 bytes
	for _, c := range []struct {
		unit pego.PositionUnit
		n    uint64
	}{{pego.CodePoints, 3}, {pego.Bytes, 6}} {
		with := func(max uint64) []pego.ParseOption {
			return []pego.ParseOption{pego.WithUnit(c.unit), pego.WithLimits(pego.Limits{MaxInput: max})}
		}
		for _, max := range []uint64{0, pego.Unlimited, c.n} {
			if _, err := p.Parse(text, with(max)...); err != nil {
				t.Errorf("unit %d, MaxInput %d: %v", c.unit, max, err)
			}
		}
		_, err := p.Parse(text, with(c.n-1)...)
		wantInputLimit(t, "Parse", err, c.n-1, c.n)
		_, err = p.NewDocument(text, with(c.n-1)...)
		wantInputLimit(t, "NewDocument", err, c.n-1, c.n)
		d, err := p.NewDocument("é", with(c.n)...)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Edit(0, 0, "éé"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(); err != nil {
			t.Fatalf("Document at the limit: %v", err)
		}
		if err := d.Edit(0, 0, "a"); err != nil {
			t.Fatal(err)
		}
		_, err = d.Parse()
		wantInputLimit(t, "Document.Parse after an edit", err, c.n, c.n+1)

		lines := "ab\ncd\n"
		sn := uint64(len(lines))
		if err := stream.ParseStream(strings.NewReader(lines), nil, with(sn)...); err != nil {
			t.Errorf("stream at the limit: %v", err)
		}
		err = stream.ParseStream(strings.NewReader(lines), nil, with(sn-1)...)
		wantInputLimit(t, "ParseStream", err, sn-1, sn)
		it, err := stream.Iterate(strings.NewReader(lines), with(sn-1)...)
		if err != nil {
			t.Fatal(err)
		}
		for {
			_, ok, err := it.Next()
			if err != nil {
				wantInputLimit(t, "Iterate", err, sn-1, sn)
				break
			}
			if !ok {
				t.Fatal("the iterator ended without the limit error")
			}
		}
	}
}
