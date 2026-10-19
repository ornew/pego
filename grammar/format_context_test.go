package grammar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestFormatContextParityAndCancellation(t *testing.T) {
	g := &Grammar{File: File{Pkg: Pkg{Name: "example"}}}
	seq := &Seq{}
	call := &Call{Func: "list"}
	union := &UnionType{}
	attrs := &Attribute{Name: "custom", KwArgs: map[string]any{}}
	for i := range 1024 {
		value := fmt.Sprintf("value%d\n😀", i)
		seq.Items = append(seq.Items, &Literal{Value: value})
		call.Args = append(call.Args, &StringLit{Value: value})
		union.Types = append(union.Types, &TypeRef{Name: fmt.Sprintf("T%d", i)})
		attrs.KwArgs[fmt.Sprintf("k%04d", 1024-i)] = value
		g.File.Decls = append(g.File.Decls, &RuleDef{Name: fmt.Sprintf("r%d", i), Expr: &Literal{Value: value}})
	}
	attrExpr := &Attributed{Expr: seq, Attrs: []*Attribute{attrs}}
	cases := []struct {
		name       string
		ordinary   func() string
		controlled func(context.Context) (string, error)
	}{
		{"grammar", func() string { return Format(g) }, func(ctx context.Context) (string, error) { return FormatContext(ctx, g) }},
		{"expression", func() string { return FormatExpr(attrExpr) }, func(ctx context.Context) (string, error) { return FormatExprContext(ctx, attrExpr) }},
		{"term", func() string { return FormatTerm(call) }, func(ctx context.Context) (string, error) { return FormatTermContext(ctx, call) }},
		{"type", func() string { return FormatType(union) }, func(ctx context.Context) (string, error) { return FormatTypeContext(ctx, union) }},
	}
	before, _ := json.Marshal(g)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.ordinary()
			for _, ctx := range []context.Context{context.Background(), metadataContext(t, 0)} {
				got, err := tc.controlled(ctx)
				if err != nil || got != want {
					t.Fatalf("output mismatch: %v", err)
				}
			}
			count := metadataContext(t, 0)
			if _, err := tc.controlled(count); err != nil || count.calls < 4 {
				t.Fatalf("insufficient traversal: checks=%d err=%v", count.calls, err)
			}
			for _, at := range []int{1, 2, count.calls / 2} {
				got, err := tc.controlled(metadataContext(t, at))
				if got != "" || !errors.Is(err, context.Canceled) {
					t.Fatalf("check %d: partial result of %d bytes / %v", at, len(got), err)
				}
			}
		})
	}
	after, _ := json.Marshal(g)
	if string(before) != string(after) {
		t.Fatal("formatting mutated the source AST")
	}
}

func TestFormatInnerPhasesCancel(t *testing.T) {
	comments := make([]*Comment, 1024)
	parts := make([]string, 1024)
	ranges := make([]CharRange, 1024)
	keys := make([]string, 1024)
	for i := range comments {
		comments[i] = &Comment{Text: "/* explanation */"}
		parts[i] = "part"
		ranges[i] = CharRange{Lo: 'a', Hi: 'z'}
		keys[i] = fmt.Sprintf("k%04d", 1024-i)
	}
	long := strings.Repeat("😀x\n", 8192)
	phases := map[string]func(*metadataControl){
		"quote":               func(c *metadataControl) { quote(long, c) },
		"ranges":              func(c *metadataControl) { formatCharClass(&CharClass{Ranges: ranges}, c) },
		"layout":              func(c *metadataControl) { formatSourceLayout(&SourceLayout{Before: comments}, "x", c) },
		"group-comments":      func(c *metadataControl) { groupInlineComments(&LineBreak{Comments: comments}, false, c) },
		"keyword-sort":        func(c *metadataControl) { sortFormatKeys(keys, c) },
		"join-lengths":        func(c *metadataControl) { joinFormatParts(parts, " ", c) },
		"join-copy":           func(c *metadataControl) { joinFormatParts([]string{long, long}, " ", c) },
		"output":              func(c *metadataControl) { p := printer{lines: []*line{{text: long}}, control: c}; _ = p.String() },
		"rune-count":          func(c *metadataControl) { formatRuneCount(long, c) },
		"newline-scan":        func(c *metadataControl) { hasFormatNewline(strings.Repeat("x", 16384), c) },
		"newline-replacement": func(c *metadataControl) { replaceFormatNewlines(long, "\n", "\n    ", c) },
		"trailing-spaces":     func(c *metadataControl) { trimFormatRight(strings.Repeat(" ", 16384), " ", c) },
	}
	for name, phase := range phases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got, ok := recover().(metadataCancellation)
				if !ok || !errors.Is(got.err, context.Canceled) {
					t.Fatalf("wrong unwind: %+v", got)
				}
			}()
			phase(&metadataControl{ctx: metadataContext(t, 1)})
			t.Fatal("phase completed without observing cancellation")
		})
	}
}

func TestFormatStringHelpersParity(t *testing.T) {
	c := &metadataControl{ctx: metadataContext(t, 0)}
	for _, s := range []string{"", "😀x\xff\x80", strings.Repeat("😀\n ", 8192)} {
		if got := formatRuneCount(s, c); got != formatRuneCount(s, nil) {
			t.Fatal("rune count", got)
		}
		if hasFormatNewline(s, c) != hasFormatNewline(s, nil) {
			t.Fatal("newline scan")
		}
		if trimFormatRight(s, " ", c) != trimFormatRight(s, " ", nil) {
			t.Fatal("trim")
		}
		for _, sep := range []string{"\r\n", "\n    ", ""} {
			if replaceFormatNewlines(s, "\n", sep, c) != strings.ReplaceAll(s, "\n", sep) {
				t.Fatal("newline conversion")
			}
			parts := []string{s, "", s}
			if joinFormatParts(parts, sep, c) != strings.Join(parts, sep) {
				t.Fatal("join")
			}
		}
	}
	// An overflowed traversal counter still reaches its next checkpoint.
	c.steps = ^uint32(0) - 2
	if got := formatRuneCount("😀abc", c); got != 4 || c.steps != 1 {
		t.Fatalf("counter wrap: %d / %d", got, c.steps)
	}
}

func TestFormatContextPreservesForeignPanic(t *testing.T) {
	marker := &struct{}{}
	ctx := &metadataPanicContext{Context: metadataContext(t, 0), marker: marker}
	term := &StringLit{Value: strings.Repeat("x", 1024)}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("foreign panic replaced: %v", got)
		}
	}()
	FormatTermContext(ctx, term)
}

func TestFormatDeepRecursionCancellation(t *testing.T) {
	var expr Expr = &Literal{Value: "x"}
	var term Term = &VarRef{Name: "x"}
	var typ TypeExpr = &TypeRef{Name: "T"}
	for range 1024 {
		expr = &Optional{Expr: expr}
		term = &Unary{Op: "-", X: term}
		typ = &ListType{Elem: typ}
	}
	cases := []func(context.Context) (string, error){
		func(ctx context.Context) (string, error) { return FormatExprContext(ctx, expr) },
		func(ctx context.Context) (string, error) { return FormatTermContext(ctx, term) },
		func(ctx context.Context) (string, error) { return FormatTypeContext(ctx, typ) },
	}
	for _, run := range cases {
		if got, err := run(metadataContext(t, 2)); got != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("deep traversal returned %d bytes / %v", len(got), err)
		}
	}
}

func TestFormatContextFinalCheckDiscardsOutput(t *testing.T) {
	cases := []func(context.Context) (string, error){
		func(ctx context.Context) (string, error) { return FormatContext(ctx, &Grammar{}) },
		func(ctx context.Context) (string, error) { return FormatExprContext(ctx, &Literal{Value: "x"}) },
		func(ctx context.Context) (string, error) { return FormatTermContext(ctx, &IntLit{Value: 1}) },
		func(ctx context.Context) (string, error) { return FormatTypeContext(ctx, &TypeRef{Name: "T"}) },
	}
	for _, run := range cases {
		if got, err := run(metadataContext(t, 2)); got != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("final check returned %q / %v", got, err)
		}
	}
}

// Cancel only after observing a recursive descent and then a shallower frame.
// This distinguishes normal-return checkpoints from the public final check.
// No sleeps, wall-clock thresholds or mutable AST hooks are involved.
type formatUnwindContext struct {
	context.Context
	cancel                  context.CancelFunc
	function                string
	maximum, cancelledDepth int
}

func (c *formatUnwindContext) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	pcs := make([]uintptr, 16384)
	n := runtime.Callers(1, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	depth := 0
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, "."+c.function) {
			depth++
		}
		if !more {
			break
		}
	}
	if depth < c.maximum {
		c.cancelledDepth = depth
		c.cancel()
		return c.Context.Err()
	}
	c.maximum = max(c.maximum, depth)
	return nil
}
func TestFormatCancellationDuringRecursiveReturn(t *testing.T) {
	var expr Expr = &Literal{Value: "x"}
	var typ TypeExpr = &TypeRef{Name: "T"}
	var grouped Expr = &Literal{Value: "x"}
	for range 2048 {
		expr = &Optional{Expr: expr}
		typ = &ListType{Elem: typ}
		grouped = &Group{Expr: grouped, OneLine: true}
	}
	for _, name := range []string{"formatExpr", "formatType", "group"} {
		t.Run(name, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			ctx := &formatUnwindContext{Context: base, cancel: cancel, function: name}
			var result string
			var err error
			if name == "group" {
				result, err = FormatExprContext(ctx, grouped)
			} else if name == "formatExpr" {
				result, err = FormatExprContext(ctx, expr)
			} else {
				result, err = FormatTypeContext(ctx, typ)
			}
			if result != "" || !errors.Is(err, context.Canceled) || ctx.cancelledDepth == 0 {
				t.Fatalf("return traversal did not cancel before final check: %d bytes / %v at depth %d", len(result), err, ctx.cancelledDepth)
			}
		})
	}
}

func TestFormatEmptyTypeDef(t *testing.T) {
	got := Format(&Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T"}}}})
	if !strings.Contains(got, "type T = {}") {
		t.Errorf("got %q, want type T = {}", got)
	}
}
