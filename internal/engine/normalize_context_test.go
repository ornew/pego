package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

func TestNormalizationContextCancellation(t *testing.T) {
	const size = 2048
	sequence := &grammar.Seq{Items: make([]grammar.Expr, size)}
	args := make([]any, size)
	keywords := make(map[string]any, size)
	block := &grammar.Block{Statements: make([]grammar.Term, size)}
	fields := make([]*grammar.Field, size)
	var groupedType grammar.TypeExpr = &grammar.TypeRef{Name: "Match"}
	var groupedTerm grammar.Term = &grammar.IntLit{Value: 1}
	for i := 0; i < size; i++ {
		sequence.Items[i] = &grammar.Literal{Value: "a"}
		args[i] = i
		keywords[fmt.Sprintf("K%d", i)] = i
		block.Statements[i] = &grammar.BoolLit{Value: true}
		fields[i] = &grammar.Field{Name: fmt.Sprintf("Field%d", i), Type: &grammar.TypeGroup{Type: &grammar.TypeRef{Name: "Match"}}}
		groupedType = &grammar.TypeGroup{Type: groupedType}
		groupedTerm = &grammar.TermGroup{Term: groupedTerm}
	}
	fixtures := map[string]*grammar.Grammar{}
	for name, expr := range map[string]grammar.Expr{
		"sequence":             sequence,
		"positional-arguments": &grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{{Name: "custom", Args: args}}},
		"keyword-arguments":    &grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{{Name: "custom", Args: []any{}, KwArgs: keywords}}},
		"predicate-block":      &grammar.Predicate{Term: block},
	} {
		fixtures[name] = &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: expr}}}}
	}
	fixtures["type-groups"] = &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Type: groupedType, Expr: &grammar.Literal{Value: "a"}}}}}
	fixtures["action-groups"] = &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Literal{Value: "a"}, Action: groupedTerm}}}}
	fixtures["struct-fields"] = &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.TypeDef{Name: "T", Type: &grammar.StructType{Fields: fields}}}}}
	union := &grammar.JoinType{Types: make([]grammar.TypeExpr, size)}
	action := &grammar.Call{Func: "list", Args: make([]grammar.Term, size)}
	for i := 0; i < size; i++ {
		union.Types[i] = &grammar.TypeRef{Name: "Match"}
		action.Args[i] = &grammar.BoolLit{Value: true}
	}
	union.Types[0] = &grammar.TypeGroup{Type: union.Types[0]}
	action.Args[0] = &grammar.TermGroup{Term: action.Args[0]}
	fixtures["grouped-type-copy"] = &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Type: union, Expr: &grammar.Literal{Value: "a"}}}}}
	fixtures["grouped-action-copy"] = &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Literal{Value: "a"}, Action: action}}}}
	for name, g := range fixtures {
		for _, eager := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/eager=%v", name, eager), func(t *testing.T) {
				before, err := json.Marshal(g)
				if err != nil {
					t.Fatal(err)
				}
				ctx := compileTestContext(t, 3)
				var result *grammar.Grammar
				var diagnostics error
				defer func() {
					failure := recover()
					cancelled, ok := failure.(compile.Cancellation)
					if !ok || !errors.Is(cancelled.Err, context.Canceled) || ctx.left != 0 {
						t.Fatalf("normalization did not cancel during traversal: %v", failure)
					}
					if result != nil || diagnostics != nil {
						t.Fatal("cancelled normalization published partial output")
					}
					after, err := json.Marshal(g)
					if err != nil || string(after) != string(before) {
						t.Fatalf("cancelled normalization changed source: %v", err)
					}
				}()
				result, diagnostics = compile.NormalizeAttributesControlled(g, eager, &compile.Control{Ctx: ctx})
				t.Fatal("normalization finished without cancellation")
			})
		}
	}
}

func TestNormalizationContextParity(t *testing.T) {
	for _, source := range []string{
		`rule main = ("a" #repeat(1, 3) #atomic @error("as")) "b" $$`,
		`rule main = ("a" #atomic ";") #recover(skip=([^;]+ ";")) $$`,
		`var x int
rule main = {x := (1); (x == 1)} "a"`,
		`type T = { Text (string) }
type Alias ~ (T)
rule main (Alias) = n:([0-9]+) => (T{Text: (text($n))})`,
		`rule main = #operators { skip " "*; operand [0-9]+; operand "(" main ")"; level { infix left "+" / "-" } }`,
		`rule main = "a" @error(nil, "bad")`,
		`rule main = "a" @error("a", message="b")`,
		`rule main = "a" @atomic`,
		`rule main = "a" #repeat(3, 1)`,
	} {
		t.Run(source, func(t *testing.T) {
			g, err := parseTest(source)
			if err != nil {
				t.Fatal(err)
			}
			before, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			for _, eager := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				got, gotErr := compile.NormalizeAttributesControlled(g, eager, &compile.Control{Ctx: ctx})
				want, wantErr := compile.NormalizeAttributesWithCopies(g, eager)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
					t.Fatalf("context changed normalized result: %v / %v", gotErr, wantErr)
				}
				after, err := grammar.MarshalJSON(g)
				if err != nil || string(after) != string(before) {
					t.Fatalf("normalization changed source: %v", err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, gotErr := CompileContext(ctx, g, compile.Options{})
			want, wantErr := Compile(g, compile.Options{})
			if !reflect.DeepEqual(gotErr, wantErr) {
				t.Fatalf("compile diagnostics differ: %v / %v", gotErr, wantErr)
			}
			if gotErr == nil && resultJSON(got.Parse("main", "a")) != resultJSON(want.Parse("main", "a")) {
				t.Fatal("context changed compiled behavior")
			}
		})
	}
}

func TestNormalizationCopiesObserveCancellation(t *testing.T) {
	const size = 2048
	values := make([]int, size)
	keywords := make(map[string]any, size)
	attrs := make([]*grammar.Attribute, size)
	terms := make([]grammar.Term, size)
	expressions := make([]grammar.Expr, size)
	for i := range values {
		values[i] = i
		keywords[fmt.Sprint(i)] = i
		attrs[i] = &grammar.Attribute{Name: "custom"}
		terms[i] = &grammar.BoolLit{Value: true}
		expressions[i] = &grammar.Predicate{Term: terms[i]}
	}
	for _, phase := range []string{"slice", "keywords", "attribute-append", "predicate-flatten"} {
		t.Run(phase, func(t *testing.T) {
			before, err := json.Marshal([]any{values, keywords, attrs, terms, expressions})
			if err != nil {
				t.Fatal(err)
			}
			ctx := compileTestContext(t, 3)
			control := &compile.Control{Ctx: ctx}
			defer func() {
				failure := recover()
				cancelled, ok := failure.(compile.Cancellation)
				if !ok || !errors.Is(cancelled.Err, context.Canceled) || ctx.left != 0 {
					t.Fatalf("copy did not cancel: %v", failure)
				}
				after, err := json.Marshal([]any{values, keywords, attrs, terms, expressions})
				if err != nil || string(after) != string(before) {
					t.Fatal("interrupted copy changed source")
				}
			}()
			switch phase {
			case "slice":
				compile.NormalizationClone(values, control)
			case "keywords":
				compile.CloneAttributeKeywords(keywords, control)
			case "attribute-append":
				compile.AppendNormalizedAttribute(attrs, &grammar.Attribute{Name: "other"}, control)
			case "predicate-flatten":
				source := []grammar.Expr{&grammar.Predicate{Term: &grammar.Block{Statements: terms}}}
				normalized := []grammar.Expr{&grammar.Seq{Items: expressions}}
				compile.ExpandPredicateBlocks(source, normalized, control)
			}
			t.Fatal("copy finished without cancellation")
		})
	}
}

func TestNormalizationFinalCheckpoint(t *testing.T) {
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Literal{Value: "a"}}}}}
	ctx := compileTestContext(t, 2)
	defer func() {
		cancelled, ok := recover().(compile.Cancellation)
		if !ok || !errors.Is(cancelled.Err, context.Canceled) || ctx.left != 0 {
			t.Fatal("short normalization ignored final cancellation")
		}
	}()
	compile.NormalizeAttributesControlled(g, false, &compile.Control{Ctx: ctx})
	t.Fatal("normalization returned a cancelled result")
}
