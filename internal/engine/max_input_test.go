//go:build !pego_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/document"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

const maxInputGrammar = `rule main = [a-zé]* $$`

// maxInputVariants runs run for every backend, with MaxInput and the default budget.
func maxInputVariants(t *testing.T, run func(t *testing.T, o ParseOptions)) {
	t.Helper()
	budgetStreamVariants(t, run)
}

func wantInputLimit(t *testing.T, err error, limit, length uint64) {
	t.Helper()
	var le *limits.LimitError
	if !errors.As(err, &le) || !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("error %v, want a LimitError", err)
	}
	if le.Kind != limits.LimitInput || le.Limit != limit || le.Requested != length || le.Used != 0 {
		t.Errorf("LimitError %+v, want kind input, limit %d, requested %d", *le, limit, length)
	}
	want := fmt.Sprintf("parse input limit exceeded: limit %d, length %d", limit, length)
	if err.Error() != want {
		t.Errorf("error text %q, want %q", err.Error(), want)
	}
}

// A batch parse of an input longer than MaxInput fails before any work, with the length of the
// input in the position unit. The input of exactly MaxInput units parses.
func TestMaxInputBatch(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, maxInputGrammar)
	text := strings.Repeat("é", 10) // 10 code points, 20 bytes
	maxInputVariants(t, func(t *testing.T, o ParseOptions) {
		units := uint64(10)
		if o.Unit == Bytes {
			units = 20
		}
		for _, c := range []struct {
			max  uint64
			fail bool
		}{{0, false}, {limits.Unlimited, false}, {units, false}, {units + 1, false}, {units - 1, true}, {1, true}} {
			o.Limits = limits.Limits{MaxInput: c.max}
			_, err := prog.ParseWith("main", text, o)
			if c.fail {
				wantInputLimit(t, err, c.max, units)
			} else if err != nil {
				t.Errorf("MaxInput %d: %v", c.max, err)
			}
		}
		// The check does not depend on the budget and does not need one.
		o.Limits = limits.Limits{MaxInput: 1, Budget: limits.Unlimited}
		_, err := prog.ParseWith("main", text, o)
		wantInputLimit(t, err, 1, units)
		// Recognition and a prefix parse apply the same limit.
		o.Recognize = true
		_, err = prog.ParseWith("main", text, o)
		wantInputLimit(t, err, 1, units)
		o.Recognize = false
		_, err = prog.ParsePrefixWith("main", text, o)
		wantInputLimit(t, err, 1, units)
	})
}

// The limit is checked before any caller context is consulted: an input over the limit never
// reaches the parser, even for a context that is already canceled.
func TestMaxInputBeforeParsing(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = [a-z]* $$`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := prog.ParseWith("main", "abc", ParseOptions{Context: ctx, Limits: limits.Limits{MaxInput: 2}})
	wantInputLimit(t, err, 2, 3)
}

// A stream fails when the parser needs a unit beyond MaxInput and the stream has one, whatever the
// chunk size of the reader. A stream of exactly MaxInput units completes.
func TestMaxInputStream(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	text := "#records\na=1\nbc=22\n" + strings.Repeat("k=7\n", 50)
	maxInputVariants(t, func(t *testing.T, o ParseOptions) {
		n := uint64(len(text)) // ASCII: units equal bytes
		run := func(max uint64, size int) (string, error) {
			o.Limits = limits.Limits{MaxInput: max}
			var out []string
			r := &budgetStreamReader{chunks: chunks(text, size)}
			err := prog.ParseStreamWith("main", r, func(v *value.Node) error { out = append(out, gentest.ResultJSON(v, nil)); return nil }, o)
			return fmt.Sprint(out), err
		}
		for _, max := range []uint64{0, limits.Unlimited, n, n + 1} {
			for _, size := range []int{1, 7, len(text)} {
				if _, err := run(max, size); err != nil {
					t.Errorf("MaxInput %d, chunk size %d: %v", max, size, err)
				}
			}
		}
		var want string
		for _, size := range []int{1, 7, len(text)} {
			out, err := run(n-1, size)
			var le *limits.LimitError
			if !errors.As(err, &le) || le.Kind != limits.LimitInput || le.Limit != n-1 {
				t.Fatalf("chunk size %d: error %v, want a LimitError of kind input", size, err)
			}
			if le.Requested != n {
				t.Errorf("chunk size %d: requested %d, want %d", size, le.Requested, n)
			}
			got := out + " " + err.Error()
			if want == "" {
				want = got
			} else if got != want {
				t.Errorf("chunk size %d gives %q, chunk size 1 gives %q", size, got, want)
			}
		}
		// A limit far below the first demand fails with the units demanded so far.
		_, err := run(4, 1)
		var le *limits.LimitError
		if !errors.As(err, &le) || le.Limit != 4 || le.Requested < 5 {
			t.Errorf("limit 4: error %v", err)
		}
	})
}

// A Document applies MaxInput to its initial text and to the text after each edit, on every parse.
func TestMaxInputDocument(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, maxInputGrammar)
	maxInputVariants(t, func(t *testing.T, o ParseOptions) {
		o.Limits = limits.Limits{MaxInput: 5}
		if _, err := prog.NewDocumentWith("main", "abcdef", o); err == nil {
			t.Fatal("a text over the limit created a Document")
		} else {
			wantInputLimit(t, err, 5, 6)
		}
		d, err := prog.NewDocumentWith("main", "abcde", o)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(); err != nil {
			t.Fatal(err)
		}
		if err := d.Edit(0, 0, "f"); err != nil {
			t.Fatal(err)
		}
		_, err = d.Parse()
		wantInputLimit(t, err, 5, 6)
		// The failed parse leaves the Document usable: removing the excess parses again.
		if err := d.Edit(0, 1, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(); err != nil {
			t.Errorf("after the edit that restores the length: %v", err)
		}
		// The limit is a per-parse option: another parse of the long text without it succeeds.
		if err := d.Edit(0, 0, "f"); err != nil {
			t.Fatal(err)
		}
		o.Limits = limits.Limits{}
		if _, err := d.ParseWith(o); err != nil {
			t.Errorf("without MaxInput: %v", err)
		}
		var _ *document.Document = d
	})
}
