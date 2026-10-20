package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"reflect"
	"sort"
)

// ordinaryMethodKey identifies a generated execution function by the parser type of its first
// parameter, which is named p.
type ordinaryMethodKey struct{ receiver, name string }

func ordinaryExecutionMethods(src string) (map[ordinaryMethodKey]bool, error) {
	file, err := goparser.ParseFile(token.NewFileSet(), "methods.go", "package generated\n"+src, goparser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	methods := make(map[ordinaryMethodKey]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*goast.FuncDecl)
		if !ok {
			continue
		}
		key, ok := ordinaryMethodIdentity(fn)
		if !ok {
			continue
		}
		if _, exists := methods[key]; exists {
			return nil, fmt.Errorf("duplicate generated execution function %s", fn.Name.Name)
		}
		methods[key] = false
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("missing generated execution functions")
	}
	return methods, nil
}

// ordinaryMethodIdentity recognizes a rule body or other generated function whose first parameter
// is the parser p, a *Parser or a *TParser.
func ordinaryMethodIdentity(fn *goast.FuncDecl) (ordinaryMethodKey, bool) {
	if fn.Recv != nil || fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return ordinaryMethodKey{}, false
	}
	first := fn.Type.Params.List[0]
	ptr, ok := first.Type.(*goast.StarExpr)
	if !ok {
		return ordinaryMethodKey{}, false
	}
	// The finished source names the runtime's types with its package, the source being
	// specialized without.
	var name string
	switch x := ptr.X.(type) {
	case *goast.Ident:
		name = x.Name
	case *goast.SelectorExpr:
		name = x.Sel.Name
	}
	if (name != "Parser" && name != "TParser") || len(first.Names) != 1 || first.Names[0].Name != "p" {
		return ordinaryMethodKey{}, false
	}
	return ordinaryMethodKey{name, fn.Name.Name}, true
}

// specializeOrdinaryMethods gives generated execution functions a branch for
// ordinary parses, which runs under guard (p.control == nil, and outside a
// Document if the parser has Documents). The general body remains unchanged.
func specializeOrdinaryMethods(src []byte, executionSource, guard string) ([]byte, error) {
	methods, err := ordinaryExecutionMethods(executionSource)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "generated.go", src, goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	type insertion struct {
		offset int
		text   []byte
	}
	var insertions []insertion
	for _, decl := range file.Decls {
		fn, ok := decl.(*goast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key, validReceiver := ordinaryMethodIdentity(fn)
		if !validReceiver {
			continue
		}
		if _, execution := methods[key]; !execution {
			continue
		}
		methods[key] = true
		var body bytes.Buffer
		if err := format.Node(&body, fset, fn.Body); err != nil {
			return nil, err
		}
		cloneFset := token.NewFileSet()
		clone, err := goparser.ParseFile(cloneFset, "ordinary.go", "package ordinary\nfunc _() "+body.String(), goparser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		plain := clone.Decls[0].(*goast.FuncDecl).Body
		goast.Inspect(plain, func(n goast.Node) bool {
			switch x := n.(type) {
			case *goast.CallExpr:
				if sel, ok := x.Fun.(*goast.SelectorExpr); ok && ordinaryReceiver(sel.X) && ordinaryHelperName(sel.Sel.Name) {
					sel.Sel.Name += "Ordinary"
				}
			case *goast.BlockStmt:
				x.List = stripOrdinaryWork(x.List)
			case *goast.CaseClause:
				x.Body = stripOrdinaryWork(x.Body)
			case *goast.CommClause:
				x.Body = stripOrdinaryWork(x.Body)
			case *goast.LabeledStmt:
				x.Label.Name += "_ordinary"
			case *goast.BranchStmt:
				if x.Label != nil {
					x.Label.Name += "_ordinary"
				}
			}
			return true
		})
		// Rewrite only reads of the parser's control. Generated functions never
		// change invocation control ownership; reject a future writer explicitly.
		var writer, shadow bool
		goast.Inspect(plain, func(n goast.Node) bool {
			receiverName := func(x goast.Expr) {
				if id, ok := x.(*goast.Ident); ok && id.Name == "p" {
					shadow = true
				}
			}
			switch x := n.(type) {
			case *goast.AssignStmt:
				for _, lhs := range x.Lhs {
					if ordinaryControl(lhs) {
						writer = true
					}
					receiverName(lhs)
				}
			case *goast.IncDecStmt:
				if ordinaryControl(x.X) {
					writer = true
				}
			case *goast.UnaryExpr:
				if x.Op == token.AND && ordinaryControl(x.X) {
					writer = true
				}
			case *goast.ValueSpec:
				for _, name := range x.Names {
					receiverName(name)
				}
			case *goast.RangeStmt:
				if x.Tok == token.DEFINE {
					receiverName(x.Key)
					receiverName(x.Value)
				}
			case *goast.FuncLit:
				if x.Type.Params != nil {
					for _, field := range x.Type.Params.List {
						for _, name := range field.Names {
							receiverName(name)
						}
					}
				}
			}
			return true
		})
		if writer {
			return nil, fmt.Errorf("generated function %s writes invocation control", fn.Name.Name)
		}
		if shadow {
			return nil, fmt.Errorf("generated function %s shadows or replaces the parser parameter", fn.Name.Name)
		}
		rewriteOrdinaryControl(plain)
		var plainText bytes.Buffer
		if err := format.Node(&plainText, cloneFset, plain); err != nil {
			return nil, err
		}
		brace := fset.Position(fn.Body.Lbrace).Offset
		branch := append([]byte("\nif "+guard+" "), plainText.Bytes()...)
		branch = append(branch, '\n')
		insertions = append(insertions, insertion{brace + 1, branch})
	}
	for key, found := range methods {
		if !found {
			return nil, fmt.Errorf("missing generated function %s.%s", key.receiver, key.name)
		}
	}
	// Positions from independently parsed clones must never be interpreted
	// in the original file's FileSet. Insert formatted branches into the
	// original text so comments and controlled bodies retain their locations.
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].offset < insertions[j].offset })
	var result []byte
	extra := 0
	for _, edit := range insertions {
		extra += len(edit.text)
	}
	result = make([]byte, 0, len(src)+extra)
	offset := 0
	for _, edit := range insertions {
		result = append(result, src[offset:edit.offset]...)
		result = append(result, edit.text...)
		offset = edit.offset
	}
	result = append(result, src[offset:]...)
	return format.Source(result)
}

func ordinaryReceiver(x goast.Expr) bool {
	if id, ok := x.(*goast.Ident); ok {
		return id.Name == "p"
	}
	if s, ok := x.(*goast.SelectorExpr); ok && s.Sel.Name == "P" {
		if id, ok := s.X.(*goast.Ident); ok {
			return id.Name == "c"
		}
	}
	return false
}
func ordinaryControl(x goast.Expr) bool {
	s, ok := x.(*goast.SelectorExpr)
	return ok && s.Sel.Name == "Control" && ordinaryReceiver(s.X)
}
func stripOrdinaryWork(stmts []goast.Stmt) []goast.Stmt {
	out := stmts[:0]
	for _, s := range stmts {
		drop := false
		if e, ok := s.(*goast.ExprStmt); ok {
			if c, ok := e.X.(*goast.CallExpr); ok {
				if sel, ok := c.Fun.(*goast.SelectorExpr); ok && ordinaryReceiver(sel.X) {
					drop = sel.Sel.Name == "StepWork" && len(c.Args) == 0
					if sel.Sel.Name == "InputWork" && len(c.Args) == 1 {
						if v, ok := c.Args[0].(*goast.BasicLit); ok {
							drop = v.Kind == token.INT && v.Value == "1"
						}
					}
				}
			}
		}
		if !drop {
			out = append(out, s)
		}
	}
	return out
}

func rewriteOrdinaryControl(root goast.Node) {
	goast.Inspect(root, func(n goast.Node) bool {
		if n == nil {
			return true
		}
		v := reflect.ValueOf(n)
		if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
			return true
		}
		v = v.Elem()
		replace := func(f reflect.Value) {
			if !f.CanSet() || f.IsNil() {
				return
			}
			if e, ok := f.Interface().(goast.Expr); ok && ordinaryControl(e) {
				removeControlRead(f)
			}
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.Kind() == reflect.Interface {
				replace(f)
			}
			if f.Kind() == reflect.Slice {
				for j := 0; j < f.Len(); j++ {
					if f.Index(j).Kind() == reflect.Interface {
						replace(f.Index(j))
					}
				}
			}
		}
		return true
	})
}

// ordinaryHelperName reports whether the runtime has an Ordinary form of the method name, which
// the nil-control branch calls instead (the runtime writes them by hand).
func ordinaryHelperName(name string) bool {
	switch name {
	case "NewNode", "NewMatch", "SetCapture", "Expect", "Peek", "MatchLiteral", "MatchAny",
		"AtEnd", "AtLineStart", "AtEndBatch", "AtLineStartBatch", "AtLineEnd", "InputWork":
		return true
	}
	return false
}

// removeControlRead replaces a read of the receiver's control with a typed nil.
func removeControlRead(field reflect.Value) {
	if !field.CanSet() || field.IsNil() {
		return
	}
	selector, ok := field.Interface().(*goast.SelectorExpr)
	if !ok || selector.Sel.Name != "Control" {
		return
	}
	// The nil expression preserves signatures and ordinary converter mappings.
	nilControl := &goast.CallExpr{Fun: &goast.ParenExpr{X: &goast.StarExpr{X: goast.NewIdent("WorkControl")}}, Args: []goast.Expr{goast.NewIdent("nil")}}
	field.Set(reflect.ValueOf(nilControl))
}
