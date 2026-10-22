package engine

import (
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"path"
	"strings"
)

// typesPackageName is the name of the package, a subpackage of the parser package, that declares
// the Go types of the typed values of a parser: types/types.go.
const typesPackageName = "types"

// typesPreamble starts the source of the types package: the types of the runtime that the types of
// the grammar need to be their own (so that they can implement the join interfaces of the
// grammar), and the hooks that make the values of these types from those the typed runtime builds.
const typesPreamble = `// TNode is a node whose Value has type T: a string node (TNode[string]) holds the text a
// literal, a character class, ., a #atomic or _ matched, and a tuple or list node holds its
// elements. A node of a statically unknown value is a TNode[any] whose Value is a string, or a
// []any of the elements of a tuple or list.
type TNode[T any] rt.TNode[T]

// Error is input skipped by error recovery (#recover).
type Error rt.Error

// PegoHooks makes the values of the types of this package from those the typed runtime builds, and
// back. It is for generated parsers.
var PegoHooks = &rt.TPubHooks{
	Match: func(m *rt.TNode[string]) any { return (*TNode[string])(m) },
	Error: func(e *rt.Error) any { return (*Error)(e) },
	Node:  func(n *rt.TNode[any]) any { return (*TNode[any])(n) },
	Back: func(v any) any {
		switch x := v.(type) {
		case *TNode[string]:
			return (*rt.TNode[string])(x)
		case *Error:
			return (*rt.Error)(x)
		case *TNode[any]:
			return (*rt.TNode[any])(x)
		}
		return v
	},
}

`

// typesImport returns the import path of the types package of the parser.
func (opts GenOptions) typesImport() string {
	return path.Join(opts.Module, typesPackageName)
}

// typesFile returns types/types.go for the body of the package (its declarations, in which the
// runtime is written as rt), and the description of the package for qualifyRuntime.
func (opts GenOptions) typesFile(body string) ([]byte, *typesPackage, error) {
	formatted, err := format.Source([]byte("package " + typesPackageName + "\n" + body))
	if err != nil {
		return nil, nil, fmt.Errorf("generated types do not compile: %w\n%s", err, body)
	}
	src := strings.TrimPrefix(string(formatted), "package "+typesPackageName+"\n")
	declared, err := declaredNames("package p\n" + src)
	if err != nil {
		return nil, nil, err
	}
	// The names that only the types package declares (the others are types of the grammar, which
	// the parser package refers to as types.Name).
	own := map[string]bool{}
	for _, name := range declared {
		if name == "Error" || name == "TNode" || name == "Unit" || name == hooksName || name == nodeTypesName || tupleName(name) {
			own[name] = true
		}
	}
	decls := map[string]goast.Expr{}
	f, err := goparser.ParseFile(token.NewFileSet(), "types.go", "package p\n"+src, goparser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range f.Decls {
		if d, ok := d.(*goast.GenDecl); ok && d.Tok == token.TYPE {
			for _, s := range d.Specs {
				ts := s.(*goast.TypeSpec)
				decls[ts.Name.Name] = ts.Type
			}
		}
	}
	// The source names the runtime explicitly, so none of its names is qualified here.
	qualified, err := qualifyRuntime(opts.runtimeImport(), src, &runtimeInfo{}, nil)
	if err != nil {
		return nil, nil, err
	}
	var file strings.Builder
	file.WriteString(generatedHeader + "\n\n")
	if !opts.OmitPackageDoc {
		fmt.Fprintf(&file, "// Package %s declares the Go types of the typed values of the parser in package %s.\n", typesPackageName, opts.Package)
	}
	fmt.Fprintf(&file, "package %s\n\n%s", typesPackageName, qualified)
	out, err := format.Source([]byte(file.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("generated types do not compile: %w", err)
	}
	return out, &typesPackage{importPath: opts.typesImport(), declared: func(name string) bool { return own[name] }, types: decls}, nil
}
