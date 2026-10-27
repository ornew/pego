package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"sort"
)

// spliceOrdinaryBodies gives the generated execution functions of whole a branch for ordinary
// parses. The bodies of the branches come from the paired text that the printer wrote: the ordinary
// form of the text is formatted by itself and its function bodies are moved into the controlled
// functions of the same name, the branch running under guard (p.Control == nil, with
// && p.Document == nil in a parser with Documents). The controlled bodies stay unchanged.
//
// A parser without work controls (noControls) has no controls to check; its guard is
// p.Document == nil, and what follows the branch for a Document is not the controlled body but the
// rest form of the text, which charges nothing and reads no control. The branch and the rest form
// then replace the whole body.
func spliceOrdinaryBodies(whole []byte, pkg, paired, executionSource, guard string, noControls bool) ([]byte, error) {
	methods, err := ordinaryExecutionMethods(executionSource)
	if err != nil {
		return nil, err
	}
	bodies := func(form textForm) (map[ordinaryMethodKey][]byte, error) {
		src, err := format.Source([]byte("package " + pkg + "\n" + selectForm(paired, form)))
		if err != nil {
			return nil, fmt.Errorf("generated code does not compile: %w", err)
		}
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, "ordinary.go", src, goparser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		out := make(map[ordinaryMethodKey][]byte)
		for _, decl := range file.Decls {
			fn, ok := decl.(*goast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key, ok := ordinaryMethodIdentity(fn)
			if !ok {
				continue
			}
			var b bytes.Buffer
			if err := format.Node(&b, fset, fn.Body); err != nil {
				return nil, err
			}
			out[key] = b.Bytes()
		}
		return out, nil
	}
	ordinary, err := bodies(formOrdinaryText)
	if err != nil {
		return nil, err
	}
	var rest map[ordinaryMethodKey][]byte
	if noControls {
		if rest, err = bodies(formRestText); err != nil {
			return nil, err
		}
	}
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "generated.go", whole, goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	type insertion struct {
		offset int
		end    int // offset of the end of the replaced text; equal to offset for an insertion
		text   []byte
	}
	var insertions []insertion
	for _, decl := range file.Decls {
		fn, ok := decl.(*goast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key, ok := ordinaryMethodIdentity(fn)
		if !ok {
			continue
		}
		if _, execution := methods[key]; !execution {
			continue
		}
		methods[key] = true
		text, ok := ordinary[key]
		if !ok {
			return nil, fmt.Errorf("missing ordinary form of generated function %s", fn.Name.Name)
		}
		brace := fset.Position(fn.Body.Lbrace).Offset
		if noControls {
			other, ok := rest[key]
			if !ok {
				return nil, fmt.Errorf("missing rest form of generated function %s", fn.Name.Name)
			}
			branch := append([]byte("{\nif "+guard+" "), text...)
			branch = append(branch, '\n')
			branch = append(branch, other[1:]...)
			insertions = append(insertions, insertion{brace, fset.Position(fn.Body.Rbrace).Offset + 1, branch})
			continue
		}
		branch := append([]byte("\nif "+guard+" "), text...)
		branch = append(branch, '\n')
		insertions = append(insertions, insertion{brace + 1, brace + 1, branch})
	}
	for key, found := range methods {
		if !found {
			return nil, fmt.Errorf("missing generated function %s.%s", key.receiver, key.name)
		}
	}
	// Insert the formatted branches into the text of whole, so that its comments and controlled
	// bodies keep their places.
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].offset < insertions[j].offset })
	extra := 0
	for _, edit := range insertions {
		extra += len(edit.text) - (edit.end - edit.offset)
	}
	result := make([]byte, 0, len(whole)+extra)
	offset := 0
	for _, edit := range insertions {
		result = append(result, whole[offset:edit.offset]...)
		result = append(result, edit.text...)
		offset = edit.end
	}
	result = append(result, whole[offset:]...)
	return format.Source(result)
}
