package flow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestVerifyRejects checks that Verify rejects functions that break each invariant it checks.
func TestVerifyRejects(t *testing.T) {
	newFunc := func(body ...Stmt) *Func {
		return &Func{Name: "f", Body: body, Fail: &Label{Name: "fail"},
			Start: &Var{Name: "start", Type: TypeInt, Entry: true}, Result: &Var{Name: "v", Type: TypeValue, Entry: true},
			Ctx: &Var{Name: "c", Type: TypeCtx, Entry: true}}
	}
	inner, outer := &Label{Name: "L1"}, &Label{Name: "L2"}
	stray := &Var{Name: "x9", Type: TypeInt}
	for _, c := range []struct {
		name string
		f    *Func
		want string
	}{
		{"exit to a block that does not enclose it", newFunc(&Block{Label: inner}, &Exit{To: inner}), "does not enclose"},
		{"continue outside its loop", newFunc(&Block{Label: outer, Body: []Stmt{&Continue{To: outer}}}), "continue"},
		// The printer writes break and continue without labels, which reach the innermost loop only.
		{"exit to an outer loop", newFunc(&Loop{Label: outer, Body: []Stmt{
			&Loop{Label: inner, Body: []Stmt{&Exit{To: outer}, &Continue{To: outer}, &Exit{To: inner}}}}}), "not the innermost"},
		{"continue of an outer loop", newFunc(&Loop{Label: outer, Body: []Stmt{
			&Loop{Label: inner, Body: []Stmt{&Continue{To: outer}, &Exit{To: inner}}}, &Exit{To: outer}}}), "continue of a loop that is not the innermost"},
		{"undeclared variable", newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&Use{V: stray}}}), "not declared"},
		{"assignment arity", newFunc(&Set{Dst: []LValue{RegPos, &Discard{}}, X: []Expr{&Int{N: 1}}}), "1 values to 2 places"},
		{"label used twice", newFunc(&Block{Label: inner}, &Block{Label: inner}), "label of its own"},
		{"unbounded loop that never exits", newFunc(&Loop{Label: &Label{}, Body: []Stmt{&Step{}}}), "never exits"},
		{"framed function with entry variables", func() *Func { f := newFunc(); f.Frame = true; return f }(), "entry variable"},
		{"local function with a value", func() *Func { f := newFunc(); f.Value = &Nil{}; return f }(), "local function"},
		{"typed match in a Node function", func() *Func {
			f := newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&NewMatch{Start: &Int{}, Text: &Str{}}}})
			f.Node = true
			return f
		}(), "typed match in a Node function"},
		{"typed node nested in a Node function", func() *Func {
			f := newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&Kids{Base: &NewNode{Start: &Int{}, Kids: &Nil{}}}}})
			f.Node = true
			return f
		}(), "typed node in a Node function"},
		{"Node terminal in a typed function", newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&NewTerminalNode{Start: &Int{}, Text: &Str{}}}}), "Node terminal in a typed function"},
		{"Node branch nested in a typed function", newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&Kids{Base: &NewBranchNode{Start: &Int{}, Kids: &Nil{}}}}}), "Node branch in a typed function"},
		{"local capture values in a Node function", func() *Func {
			f := newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&AttachCaptures{X: &Nil{}, Names: &Names{}, Vals: []*Var{}, Start: &Int{}}}})
			f.Node = true
			return f
		}(), "typed capture values of local variables in a Node function"},
		{"frame trail drop in a Node function", func() *Func {
			f := newFunc(&DropTrail{M: &Nil{}})
			f.Node = true
			return f
		}(), "frame trail in a Node function"},
		{"frame release in a Node function", func() *Func {
			f := newFunc(&FreeFrame{F: &Var{Name: "fr", Type: TypeValue}})
			f.Node = true
			return f
		}(), "frame release in a Node function"},
		{"stream start in a typed function", newFunc(&StreamStart{}), "stream start outside a framed Node function"},
		{"stream element in a local Node function", func() *Func {
			f := newFunc(&EmitStream{X: &Nil{}})
			f.Node = true
			return f
		}(), "stream element outside a framed Node function"},
		{"undeclared cut flag", func() *Func {
			return &Func{Name: "f", Fail: &Label{Name: "fail"}, Frame: true, Cut: &Var{Name: "cut1", Type: TypeBool}}
		}(), "not declared"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Verify(c.f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want an error containing %q", err, c.want)
			}
		})
	}
	// A valid function of the same parts verifies; an Exit may leave a loop for a block that
	// encloses it, which the printer writes as a goto.
	block := &Label{Name: "L3"}
	ok := newFunc(&Block{Label: inner, Body: []Stmt{&Set{Dst: []LValue{RegPos}, X: []Expr{&Int{N: 1}}}, &Exit{To: inner}}},
		&Loop{Label: outer, Body: []Stmt{&Step{}, &Continue{To: outer}, &Exit{To: outer}}},
		&Block{Label: block, Body: []Stmt{&Loop{Label: &Label{}, While: &Cmp{Op: "<", L: &Int{}, R: &Int{N: 1}}, Body: []Stmt{&Exit{To: block}}}}})
	ok.Body = append(ok.Body, &Exit{To: ok.Fail})
	if err := Verify(ok); err != nil {
		t.Errorf("a valid function: %v", err)
	}
}

// TestSubexprsCoversExprFields checks that Subexprs reports every field of an expression that is itself
// an expression: Verify reaches nested value operations through it, so a field it missed would let a
// typed operation hide from the check of a Node function.
func TestSubexprsCoversExprFields(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "flow.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	isExpr := map[string]bool{}
	fields := map[string][]string{}
	var subexprs *ast.FuncDecl
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.Name == "expr" && d.Recv != nil {
				if star, ok := d.Recv.List[0].Type.(*ast.StarExpr); ok {
					isExpr[star.X.(*ast.Ident).Name] = true
				}
			}
			if d.Name.Name == "Subexprs" {
				subexprs = d
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, f := range st.Fields.List {
					if id, ok := f.Type.(*ast.Ident); ok && id.Name == "Expr" {
						for _, n := range f.Names {
							fields[ts.Name.Name] = append(fields[ts.Name.Name], n.Name)
						}
					}
				}
			}
		}
	}
	if subexprs == nil {
		t.Fatal("Subexprs not found")
	}
	// The fields each case of Subexprs passes on, by the name of the case's type.
	covered := map[string]map[string]bool{}
	ast.Inspect(subexprs, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok || len(cc.List) != 1 {
			return true
		}
		star, ok := cc.List[0].(*ast.StarExpr)
		if !ok {
			return true
		}
		name := star.X.(*ast.Ident).Name
		covered[name] = map[string]bool{}
		for _, st := range cc.Body {
			ast.Inspect(st, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					covered[name][sel.Sel.Name] = true
				}
				return true
			})
		}
		return true
	})
	for typ, fs := range fields {
		if !isExpr[typ] {
			continue
		}
		for _, f := range fs {
			if !covered[typ][f] {
				t.Errorf("Subexprs does not report the field %s of %s", f, typ)
			}
		}
	}
}
