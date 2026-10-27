package engine

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
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
	// The finished source names the runtime's types with its package, the execution source
	// without.
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

// ordinaryHelperName reports whether the runtime has an Ordinary form of the method name, which
// the nil-control branch calls instead (the runtime writes them by hand).
func ordinaryHelperName(name string) bool {
	switch name {
	case "NewNode", "NewTerminal", "NewBranch", "NewMatch", "SetCapture", "Expect", "Peek", "MatchLiteral", "MatchLiteralFold", "MatchAny",
		"AtEnd", "AtLineStart", "AtEndBatch", "AtLineStartBatch", "AtLineEnd", "InputWork":
		return true
	}
	return false
}
