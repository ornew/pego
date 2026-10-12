package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"reflect"
)

// removeGeneratedWorkState makes a private cost reference for ordinary calls.
// The disabled-control semantic reference keeps its state for helper tests;
// this separate variant removes that state from parser/option/converter layouts.
// It supports the pre-control unit/depth options and omits control options.
func removeGeneratedWorkState(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "reference.go", src, goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if err := checkGeneratedWorkAnchors(file); err != nil {
		return nil, err
	}
	var decls []goast.Decl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*goast.FuncDecl); ok && fn.Recv == nil {
			switch fn.Name.Name {
			case "WithContext", "WithWorkLimits":
				continue
			case "prepareOptions":
				fn.Body = &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{goast.NewIdent("o")}}}}
			case "resolveOptions":
				var kept []goast.Stmt
				for _, stmt := range fn.Body.List {
					// The reference exposes only ordinary options; remove the effective
					// control-option rejection with its removed context/limits fields.
					if cond, ok := stmt.(*goast.IfStmt); ok && referencesField(cond.Cond, "context", "limits") {
						continue
					}
					kept = append(kept, stmt)
				}
				fn.Body.List = kept
			}
		}
		decls = append(decls, decl)
	}
	file.Decls = decls
	goast.Inspect(file, func(n goast.Node) bool {
		if spec, ok := n.(*goast.TypeSpec); ok {
			if structure, ok := spec.Type.(*goast.StructType); ok {
				switch spec.Name.Name {
				case "parser", "parseOptions", "astConv", "tslabs":
					var kept []*goast.Field
					for _, field := range structure.Fields.List {
						if len(field.Names) == 0 {
							kept = append(kept, field)
							continue
						}
						var names []*goast.Ident
						for _, name := range field.Names {
							if name.Name != "control" && (spec.Name.Name != "parseOptions" || name.Name != "context" && name.Name != "limits" && name.Name != "prepared") {
								names = append(names, name)
							}
						}
						if len(names) != 0 {
							field.Names = names
							kept = append(kept, field)
						}
					}
					structure.Fields.List = kept
				}
			}
		}
		if block, ok := n.(*goast.BlockStmt); ok {
			var kept []goast.Stmt
			for _, stmt := range block.List {
				if assignment, ok := stmt.(*goast.AssignStmt); ok && len(assignment.Lhs) == 1 {
					if field, ok := assignment.Lhs[0].(*goast.SelectorExpr); ok && field.Sel.Name == "control" {
						continue
					}
				}
				kept = append(kept, stmt)
			}
			block.List = kept
		}
		if literal, ok := n.(*goast.CompositeLit); ok {
			var kept []goast.Expr
			for _, element := range literal.Elts {
				if field, ok := element.(*goast.KeyValueExpr); ok {
					if name, ok := field.Key.(*goast.Ident); ok && name.Name == "control" {
						continue
					}
				}
				kept = append(kept, element)
			}
			literal.Elts = kept
		}
		// goast.Expr fields hold replaceable children. Walk only those exported
		// fields/slice elements; SkipObjectResolution keeps this AST acyclic.
		if n != nil {
			value := reflect.ValueOf(n)
			if value.Kind() == reflect.Pointer && value.Elem().Kind() == reflect.Struct {
				value = value.Elem()
				for i := 0; i < value.NumField(); i++ {
					field := value.Field(i)
					if field.Kind() == reflect.Interface {
						removeControlRead(field)
					}
					if field.Kind() == reflect.Slice {
						for j := 0; j < field.Len(); j++ {
							if field.Index(j).Kind() == reflect.Interface {
								removeControlRead(field.Index(j))
							}
						}
					}
				}
			}
		}
		return true
	})
	if referencesField(file, "control", "context", "prepared") {
		return nil, fmt.Errorf("generated layout reference retains invocation control fields")
	}
	var out bytes.Buffer
	if err := format.Node(&out, fset, file); err != nil {
		return nil, err
	}
	return format.Source(out.Bytes())
}

func removeControlRead(field reflect.Value) {
	if !field.CanSet() || field.IsNil() {
		return
	}
	selector, ok := field.Interface().(*goast.SelectorExpr)
	if !ok || selector.Sel.Name != "control" {
		return
	}
	// The nil expression preserves signatures and ordinary converter mappings,
	// while a false generation constant removes charged execution paths.
	nilControl := &goast.CallExpr{Fun: &goast.ParenExpr{X: &goast.StarExpr{X: goast.NewIdent("workControl")}}, Args: []goast.Expr{goast.NewIdent("nil")}}
	field.Set(reflect.ValueOf(nilControl))
}

func referencesField(n goast.Node, names ...string) bool {
	found := false
	goast.Inspect(n, func(n goast.Node) bool {
		if selector, ok := n.(*goast.SelectorExpr); ok {
			for _, name := range names {
				if selector.Sel.Name == name {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// A cost reference must fail closed when the runtime anchors change. In
// particular, silently retaining an added option field invalidates its layout.
func checkGeneratedWorkAnchors(file *goast.File) error {
	functions := map[string]int{"WithContext": 0, "WithWorkLimits": 0, "prepareOptions": 0, "resolveOptions": 0}
	structures := map[string]*goast.StructType{}
	disabled := 0
	rejection := 0
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *goast.FuncDecl:
			if decl.Recv != nil {
				continue
			}
			if _, ok := functions[decl.Name.Name]; !ok {
				continue
			}
			functions[decl.Name.Name]++
			if decl.Body == nil {
				return fmt.Errorf("generated layout reference: missing %s body", decl.Name.Name)
			}
			if decl.Name.Name == "prepareOptions" && workAnchorText(decl.Body) != workPreparationAnchor {
				return fmt.Errorf("generated layout reference: prepareOptions changed")
			}
			if decl.Name.Name == "resolveOptions" {
				for _, stmt := range decl.Body.List {
					cond, ok := stmt.(*goast.IfStmt)
					if !ok || !referencesField(cond.Cond, "context", "limits") {
						continue
					}
					if workAnchorText(cond) != workRejectionAnchor {
						return fmt.Errorf("generated layout reference: control option rejection changed")
					}
					rejection++
				}
			}
		case *goast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *goast.ValueSpec:
					for i, name := range spec.Names {
						if name.Name != "workControlsEnabled" {
							continue
						}
						if decl.Tok != token.CONST || i >= len(spec.Values) || workAnchorText(spec.Values[i]) != "false" {
							return fmt.Errorf("generated layout reference: controls must be statically disabled")
						}
						disabled++
					}
				case *goast.TypeSpec:
					switch spec.Name.Name {
					case "parser", "parseOptions", "astConv", "tslabs":
						structure, ok := spec.Type.(*goast.StructType)
						if !ok || structures[spec.Name.Name] != nil {
							return fmt.Errorf("generated layout reference: unexpected %s declaration", spec.Name.Name)
						}
						structures[spec.Name.Name] = structure
					}
				}
			}
		}
	}
	for _, name := range []string{"WithContext", "WithWorkLimits", "prepareOptions", "resolveOptions"} {
		if functions[name] != 1 {
			return fmt.Errorf("generated layout reference: expected one %s function", name)
		}
	}
	if disabled != 1 || rejection != 1 {
		return fmt.Errorf("generated layout reference: disabled control/rejection anchors changed")
	}
	for _, name := range []string{"parser", "parseOptions"} {
		if structures[name] == nil {
			return fmt.Errorf("generated layout reference: missing %s declaration", name)
		}
	}
	for name, structure := range structures {
		fields := map[string]string{}
		for _, field := range structure.Fields.List {
			if name == "parseOptions" && len(field.Names) == 0 {
				return fmt.Errorf("generated layout reference: embedded option field")
			}
			for _, fieldName := range field.Names {
				if _, exists := fields[fieldName.Name]; exists {
					return fmt.Errorf("generated layout reference: duplicate %s.%s field", name, fieldName.Name)
				}
				fields[fieldName.Name] = workAnchorText(field.Type)
			}
		}
		if fields["control"] != "*workControl" {
			return fmt.Errorf("generated layout reference: missing or changed %s.control field", name)
		}
		if name == "parseOptions" {
			expected := map[string]string{"unit": "Unit", "maxDepth": "int", "context": "context.Context", "limits": "WorkLimits", "control": "*workControl", "prepared": "bool"}
			if !reflect.DeepEqual(fields, expected) {
				return fmt.Errorf("generated layout reference: option layout changed")
			}
		}
	}
	return nil
}

func workAnchorText(n goast.Node) string {
	var out bytes.Buffer
	if err := format.Node(&out, token.NewFileSet(), n); err != nil {
		return "<invalid>"
	}
	return out.String()
}

const workPreparationAnchor = `{
	if !o.prepared {
		o.prepared = true
		if workControlsEnabled && (o.context != nil || o.limits != (WorkLimits{})) {
			o.control = newWorkControl(o.context, o.limits)
		}
	}
	o.control.check()
	return o
}`
const workRejectionAnchor = `if !workControlsEnabled && (o.context != nil || o.limits != (WorkLimits{})) {
	return o, fmt.Errorf("work controls are disabled in this generated reference parser")
}`
