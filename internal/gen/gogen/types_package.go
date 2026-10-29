package gogen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"path"
	"strings"

	"github.com/ornew/pego/internal/gen"
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
func typesImport(opts gen.GenOptions) string {
	return path.Join(opts.Module, typesPackageName)
}

// typesPackageFile returns types/types.go for the body of the package (its declarations, in which the
// runtime is written as rt), and the description of the package for QualifyRuntime.
func typesPackageFile(opts gen.GenOptions, body string) ([]byte, *TypesPackage, error) {
	formatted, err := format.Source([]byte("package " + typesPackageName + "\n" + body))
	if err != nil {
		return nil, nil, fmt.Errorf("generated types do not compile: %w\n%s", err, body)
	}
	src := strings.TrimPrefix(string(formatted), "package "+typesPackageName+"\n")
	schema, err := schemaName(formatted)
	if err != nil {
		return nil, nil, err
	}
	src += fmt.Sprintf("\n// %[1]s ties the parser package to this package: the parser refers to it once, so a\n// parser and a types package generated from different schemas do not compile together.\n// It is for generated parsers; not a stable API.\nconst %[1]s = 0\n", schema)
	declared, err := DeclaredNames("package p\n" + src)
	if err != nil {
		return nil, nil, err
	}
	// The names that only the types package declares (the others are types of the grammar, which
	// the parser package refers to as types.Name).
	own := map[string]bool{}
	for _, name := range declared {
		if name == "Error" || name == "TNode" || name == "Unit" || name == hooksName || name == nodeTypesName || name == schema || tupleName(name) {
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
	qualified, err := QualifyRuntime(runtimeImport(opts), src, &runtimeInfo{}, nil)
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
	return out, &TypesPackage{ImportPath: typesImport(opts), Declared: func(name string) bool { return own[name] }, Types: decls, schema: schema}, nil
}

// schemaPrefix starts the name of the constant that ties a parser package to its types package.
const schemaPrefix = "PegoSchema_"

// schemaName returns the name of the schema constant of the types package whose source (with its package
// clause) is src: schemaPrefix and the hex digest of the declarations and code of the package without
// comments, as go/printer prints them. The parser relies on everything the package declares (the names,
// kinds and fields of the types, their methods, the hooks and the registered node types), and the printer
// makes the digest independent of the layout and comments of the source, which carry no meaning for the
// parser; the grammar's documentation comments are among them.
func schemaName(src []byte) (string, error) {
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "types.go", src, goparser.SkipObjectResolution)
	if err != nil {
		return "", err
	}
	var canon bytes.Buffer
	if err := printer.Fprint(&canon, fset, f); err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon.Bytes())
	return schemaPrefix + hex.EncodeToString(sum[:8]), nil
}
