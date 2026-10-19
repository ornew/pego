package grammar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type metadataTestContext struct {
	context.Context
	cancel          context.CancelFunc
	calls, cancelAt int
}

func (c *metadataTestContext) Err() error {
	c.calls++
	if c.calls == c.cancelAt {
		c.cancel()
	}
	return c.Context.Err()
}

func metadataContext(t *testing.T, cancelAt int) *metadataTestContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &metadataTestContext{Context: ctx, cancel: cancel, cancelAt: cancelAt}
}

func TestMetadataCollectionFactsAndOwnership(t *testing.T) {
	r1, r2 := &RuleDef{Name: "a"}, &RuleDef{Name: "b"}
	t1, t2 := &TypeDef{Name: "A"}, &TypeDef{Name: "B"}
	c1, c2, c3 := &Comment{Text: "// first"}, &Comment{Text: "// body"}, &Comment{Text: "// last"}
	r1.Expr = &Group{BodyBreak: &LineBreak{Comments: []*Comment{c2}}, Expr: &Literal{Value: "a"}}
	g := &Grammar{File: File{Pkg: Pkg{Break: &LineBreak{Comments: []*Comment{c1}}}, Decls: []Decl{t1, r1, t2, r2}, EndBreak: &LineBreak{Trailing: c3}}}
	x, y, z := &Literal{Value: "x"}, &Literal{Value: "y"}, &Literal{Value: "z"}
	a := &Attribute{Args: []any{nil, 1, "scalar", x}, KwArgs: map[string]any{"z": z, "a": y, "scalar": true}}
	before, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{context.Background(), metadataContext(t, 0)} {
		rules, err := g.RulesContext(ctx)
		if err != nil || !reflect.DeepEqual(rules, []*RuleDef{r1, r2}) {
			t.Fatalf("rules: %v / %v", rules, err)
		}
		types, err := g.TypesContext(ctx)
		if err != nil || !reflect.DeepEqual(types, []TypeDecl{t1, t2}) {
			t.Fatalf("types: %v / %v", types, err)
		}
		comments, err := g.AllCommentsContext(ctx)
		if err != nil || !reflect.DeepEqual(comments, []*Comment{c1, c2, c3}) {
			t.Fatalf("comments: %v / %v", comments, err)
		}
		args, err := a.ExpressionArgsContext(ctx)
		if err != nil || !reflect.DeepEqual(args, []Expr{x, y, z}) {
			t.Fatalf("arguments: %v / %v", args, err)
		}
		// Results share the documented node identities, but never slice storage.
		rules[0], types[0], comments[0], args[0] = nil, nil, nil, nil
		if g.Rules()[0] != r1 || g.Types()[0] != t1 || g.AllComments()[0] != c1 || a.ExpressionArgs()[0] != x {
			t.Fatal("returned collection aliases retained slice storage")
		}
	}
	after, err := json.Marshal(g)
	if err != nil || string(before) != string(after) {
		t.Fatal("collection changed caller-owned AST")
	}
}

func TestMetadataCollectionEmptyAndCancellation(t *testing.T) {
	g := &Grammar{}
	a := &Attribute{}
	for _, ctx := range []context.Context{context.Background(), metadataContext(t, 0)} {
		r, re := g.RulesContext(ctx)
		ty, te := g.TypesContext(ctx)
		c, ce := g.AllCommentsContext(ctx)
		args, ae := a.ExpressionArgsContext(ctx)
		if r != nil || ty != nil || c != nil || args != nil || re != nil || te != nil || ce != nil || ae != nil {
			t.Fatal("empty collections changed nil/empty behavior")
		}
	}
	for range 1024 {
		g.File.Decls = append(g.File.Decls, &RuleDef{Name: "a"}, &TypeDef{Name: "A"})
		a.Args = append(a.Args, &Literal{})
	}
	for _, cancelAt := range []int{1, 2} {
		ctx := metadataContext(t, cancelAt)
		if result, err := g.RulesContext(ctx); result != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("partial rules: %v / %v", result, err)
		}
		ctx = metadataContext(t, cancelAt)
		if result, err := g.TypesContext(ctx); result != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("partial types: %v / %v", result, err)
		}
		ctx = metadataContext(t, cancelAt)
		if result, err := g.AllCommentsContext(ctx); result != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("partial comments: %v / %v", result, err)
		}
		ctx = metadataContext(t, cancelAt)
		if result, err := a.ExpressionArgsContext(ctx); result != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("partial arguments: %v / %v", result, err)
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	if result, err := g.RulesContext(ctx); result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error changed: %v / %v", result, err)
	}
}

func TestMetadataTraversalStopsWithinCollection(t *testing.T) {
	rules, types := &Grammar{}, &Grammar{}
	comments := &Grammar{File: File{Pkg: Pkg{Break: &LineBreak{}}}}
	attrs := &Attribute{KwArgs: map[string]any{}}
	var term Term = &IntLit{}
	var typ TypeExpr = &TypeRef{Name: "A"}
	var expr Expr = &Literal{}
	for i := range 1024 {
		// Even statements and arguments excluded from the result need polling.
		rules.File.Decls = append(rules.File.Decls, &TypeDef{Name: "A"})
		types.File.Decls = append(types.File.Decls, &RuleDef{Name: "a"})
		comments.File.Pkg.Break.Comments = append(comments.File.Pkg.Break.Comments, &Comment{})
		attrs.Args = append(attrs.Args, i)
		attrs.KwArgs[fmt.Sprintf("key%04d", 1024-i)] = i
		term = &TermGroup{Term: term}
		typ = &TypeGroup{Type: typ}
		expr = &Group{Expr: expr}
	}
	var smallAttrs []*Attribute
	for range 16 {
		smallAttrs = append(smallAttrs, &Attribute{Args: attrs.Args[:64]})
	}
	for _, phase := range []struct {
		name     string
		run      func(*metadataControl)
		cancelAt int
	}{
		{"excluded-rules", func(c *metadataControl) { rules.rules(c) }, 1},
		{"excluded-types", func(c *metadataControl) { types.types(c) }, 1},
		{"comment-copy", func(c *metadataControl) { comments.allComments(c) }, 1},
		{"scalar-arguments", func(c *metadataControl) { attrs.expressionArgs(c) }, 1},
		{"keyword-sort", func(c *metadataControl) { (&Attribute{KwArgs: attrs.KwArgs}).expressionArgs(c) }, 5},
		{"nested-terms", func(c *metadataControl) { (&Grammar{File: File{Decls: []Decl{&RuleDef{Action: term}}}}).allComments(c) }, 1},
		{"nested-types", func(c *metadataControl) { (&Grammar{File: File{Decls: []Decl{&RuleDef{Type: typ}}}}).allComments(c) }, 1},
		{"nested-expressions", func(c *metadataControl) { (&Grammar{File: File{Decls: []Decl{&RuleDef{Expr: expr}}}}).allComments(c) }, 1},
		{"shared-small-arguments", func(c *metadataControl) {
			(&Grammar{File: File{Decls: []Decl{&RuleDef{Expr: &Attributed{Expr: &Literal{}, Attrs: smallAttrs}}}}}).allComments(c)
		}, 1},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &metadataControl{ctx: metadataContext(t, phase.cancelAt)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(metadataCancellation)
				if !ok || !errors.Is(stopped.err, context.Canceled) {
					t.Fatalf("collection did not stop internally: %v", failure)
				}
				if control.steps != uint32(phase.cancelAt*256) {
					t.Fatalf("stopped outside expected traversal poll: %d", control.steps)
				}
			}()
			phase.run(control)
		})
	}
}

type metadataPanicContext struct {
	context.Context
	marker any
	calls  int
}

func (c *metadataPanicContext) Err() error {
	c.calls++
	if c.calls == 2 {
		panic(c.marker)
	}
	return nil
}

func TestMetadataContextPreservesForeignPanics(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := &struct{}{}
	ctx := &metadataPanicContext{Context: base, marker: marker}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("panic identity changed: %v", got)
		}
	}()
	(&Grammar{}).AllCommentsContext(ctx)
}
