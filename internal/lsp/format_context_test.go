package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Done is non-nil so the formatting operation uses its controlled traversal.
type formatTestContext struct {
	context.Context
	checks, cancelAt int
}

func (c *formatTestContext) Err() error {
	c.checks++
	if c.cancelAt > 0 && c.checks >= c.cancelAt {
		return context.Canceled
	}
	return c.Context.Err()
}
func formatContext(t *testing.T, at int) *formatTestContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &formatTestContext{Context: ctx, cancelAt: at}
}

func TestFormatEditsContextParity(t *testing.T) {
	for _, text := range []string{
		"rule main=\"😀\" // comment\n", "rule main = \"a\"\n", "rule main = (\n  \"a\"\n  / \"b\"\n)\n", "rule main = )\n",
		"rule main=\"a\"\r\nrule other=\"b\"\r\n", "rule main=\"a\"\rrule other=\"b\"\r",
	} {
		a := analyze(text)
		want := formatEdits(a)
		for _, ctx := range []context.Context{context.Background(), formatContext(t, 0)} {
			got, err := formatEditsContext(ctx, a)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%q: got %+v / %v want %+v", text, got, err, want)
			}
		}
	}
}

func TestFormatEditsContextCancellationReturnsNoEdits(t *testing.T) {
	a := analyze("rule main = \"" + strings.Repeat("x", 8192) + "\"\n")
	count := formatContext(t, 0)
	if _, err := formatEditsContext(count, a); err != nil || count.checks < 8 {
		t.Fatalf("checks=%d err=%v", count.checks, err)
	}
	for _, at := range []int{1, 3, count.checks - 1, count.checks} {
		got, err := formatEditsContext(formatContext(t, at), a)
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("check %d: partial edits %+v / %v", at, got, err)
		}
	}
	uri := "file:///cancel-format.zp"
	s := &Server{requestContext: formatContext(t, 3), docs: map[string]*document{uri: {uri: uri, an: a}}}
	params, _ := json.Marshal(docParams(uri))
	got, err := s.formatting(params)
	if got != nil || err == nil || err.Code != codeRequestCancelled {
		t.Fatalf("request returned %+v / %+v", got, err)
	}
}

func TestFormatEditPostprocessingCancellation(t *testing.T) {
	long := strings.Repeat("😀abc", 4096)
	for _, name := range []string{"line-end", "newlines", "prefix", "suffix"} {
		t.Run(name, func(t *testing.T) {
			ctx := formatContext(t, 2)
			var err error
			switch name {
			case "line-end":
				_, err = lineEndContext(ctx, long)
			case "newlines":
				_, err = formatNewlinesContext(ctx, long+"\n", "\r\n")
			case "prefix":
				_, err = minimalEditContext(ctx, newTextIndex(long), long, long+"x")
			case "suffix":
				_, err = minimalEditContext(ctx, newTextIndex(long), long, "x"+long)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("postprocessing did not cancel: %v", err)
			}
		})
	}
}

func TestMinimalEditContextParity(t *testing.T) {
	texts := []string{"", "a", "😀x", "😀y", "é\r\nx", "é\r\ny", "\r", strings.Repeat("😀x\r\n", 1024)}
	for _, old := range texts {
		idx := newTextIndex(old)
		for _, new := range texts {
			got, err := minimalEditContext(formatContext(t, 0), idx, old, new)
			want := minimalEdit(idx, old, new)
			if err != nil || got != want {
				t.Fatalf("old %d new %d: %+v / %v want %+v", len(old), len(new), got, err, want)
			}
		}
	}
}

func TestFormatSyntaxErrorStillChecksCancellation(t *testing.T) {
	a := analyze("rule main = )\n")
	got, err := formatEditsContext(formatContext(t, 2), a)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("syntax-error path returned %+v / %v", got, err)
	}
}
