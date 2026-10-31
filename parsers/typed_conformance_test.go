package parsers_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
)

// The typed conformance checks compare the generated typed outputs with the output schema of their
// grammar. The schema is the one that OutputSchema exports with every rule and type. Each declared
// type of the schema must be a generated type of the same name, with a field for each schema field
// of the matching shape; each generated field must be a schema field. The Go types are read from the
// generated files, and the TypeScript declarations from the generated TypeScript of the same grammar.
//
// The mapping from schema shapes to Go is:
//
//	scalar string   string
//	ref declared X  *X
//	ref inferred Y  the shape of Y
//	list of S       []S
//	optional S      the Go type of S; an optional adds no pointer
//	join of X..     an interface that the types X.. implement (all of them, and only them)
//
// and to TypeScript, where every reference is nullable (`X | null`), a list is `readonly (...)[] | null`
// and a join is the union of its alternatives. A shape outside these mappings is reported as unmapped
// rather than checked by guess. The nullability of list elements is not checked in either language: an
// optional element adds no Go pointer, and a TypeScript list element always carries `| null`.

// confSchema is the part of the output schema JSON that the checks read.
type confSchema struct {
	Types []confType `json:"types"`
}

type confType struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Origin string     `json:"origin"`
	Shape  *confShape `json:"shape"`
}

type confShape struct {
	Kind         string       `json:"kind"`
	Scalar       string       `json:"scalar"`
	Ref          string       `json:"ref"`
	Element      *confShape   `json:"element"`
	Value        *confShape   `json:"value"`
	Alternatives []*confShape `json:"alternatives"`
	Fields       []confField  `json:"fields"`
}

type confField struct {
	Name  string     `json:"name"`
	Shape *confShape `json:"shape"`
}

// conformanceSchema exports the schema of the grammar at path, with all rules and declared types.
func conformanceSchema(t *testing.T, path string) confSchema {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := zeroparse.CompileSource(string(src), "")
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	s, err := p.OutputSchema(zeroparse.WithAllSchemaRules())
	if err != nil {
		t.Fatalf("%s: schema: %v", path, err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var sc confSchema
	if err := json.Unmarshal(data, &sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

// schemaTypes indexes the types of a schema by ID.
func (sc confSchema) byID(id string) *confType {
	for i := range sc.Types {
		if sc.Types[i].ID == id {
			return &sc.Types[i]
		}
	}
	return nil
}

// resolve follows the references to inferred types to the shape they name.
func (sc confSchema) resolve(sh *confShape) (*confShape, error) {
	for depth := 0; sh != nil && sh.Kind == "ref" && strings.HasPrefix(sh.Ref, "inferred/"); depth++ {
		if depth > 32 {
			return nil, fmt.Errorf("reference cycle through %s", sh.Ref)
		}
		typ := sc.byID(sh.Ref)
		if typ == nil {
			return nil, fmt.Errorf("unknown type %s", sh.Ref)
		}
		sh = typ.Shape
	}
	return sh, nil
}

// goPackage is the Go declaration set of a generated types package.
type goPackage struct {
	types   map[string]ast.Expr        // type name -> its type expression
	ifaces  map[string][]string        // interface name -> its method names
	methods map[string]map[string]bool // receiver type name -> method names
}

// parseGoTypes reads the generated Go types package file at path.
func parseGoTypes(t *testing.T, path string) goPackage {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg := goPackage{types: map[string]ast.Expr{}, ifaces: map[string][]string{}, methods: map[string]map[string]bool{}}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				pkg.types[ts.Name.Name] = ts.Type
				if it, ok := ts.Type.(*ast.InterfaceType); ok {
					var names []string
					for _, m := range it.Methods.List {
						for _, n := range m.Names {
							names = append(names, n.Name)
						}
					}
					pkg.ifaces[ts.Name.Name] = names
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) != 1 {
				continue
			}
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if idx, ok := recv.(*ast.IndexExpr); ok {
				recv = idx.X
			}
			id, ok := recv.(*ast.Ident)
			if !ok {
				continue
			}
			if pkg.methods[id.Name] == nil {
				pkg.methods[id.Name] = map[string]bool{}
			}
			pkg.methods[id.Name][d.Name.Name] = true
		}
	}
	return pkg
}

// implementers returns the sorted names of the types that have method m. The recovery type Error
// implements every join: error recovery can put it in any position, so the schema does not list it.
func (pkg goPackage) implementers(m string) []string {
	var out []string
	for name, ms := range pkg.methods {
		if ms[m] && name != "Error" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// checkGoType returns the violations of the generated Go declaration of a declared type.
func checkGoType(sc confSchema, pkg goPackage, typ confType) []string {
	expr, ok := pkg.types[typ.Name]
	if !ok {
		return []string{"no generated Go type " + typ.Name}
	}
	shape := typ.Shape
	switch shape.Kind {
	case "node":
		st, ok := expr.(*ast.StructType)
		if !ok || len(st.Fields.List) != 1 || len(st.Fields.List[0].Names) != 0 || types.ExprString(st.Fields.List[0].Type) != "TNode[string]" {
			return []string{fmt.Sprintf("Go type %s is not a node struct embedding TNode[string]", typ.Name)}
		}
		return nil
	case "struct":
		st, ok := expr.(*ast.StructType)
		if !ok {
			return []string{fmt.Sprintf("Go type %s is not a struct", typ.Name)}
		}
		goFields := map[string]string{}
		for _, f := range st.Fields.List {
			for _, n := range f.Names {
				goFields[strings.TrimSuffix(n.Name, "_")] = types.ExprString(f.Type)
			}
		}
		var out []string
		schemaNames := map[string]bool{}
		for _, f := range shape.Fields {
			schemaNames[f.Name] = true
			got, ok := goFields[f.Name]
			if !ok {
				out = append(out, fmt.Sprintf("%s.%s: schema field has no generated Go field", typ.Name, f.Name))
				continue
			}
			if v := matchGo(sc, pkg, f.Shape, got); v != "" {
				out = append(out, fmt.Sprintf("%s.%s: %s", typ.Name, f.Name, v))
			}
		}
		for name := range goFields {
			if !schemaNames[name] {
				out = append(out, fmt.Sprintf("%s.%s: generated Go field is not in the schema", typ.Name, name))
			}
		}
		sort.Strings(out)
		return out
	}
	return []string{fmt.Sprintf("%s: unmapped schema shape %s for a declared type", typ.Name, shape.Kind)}
}

// matchGo returns a violation when the Go type expression got does not have the schema shape sh, or
// "" when it has it.
func matchGo(sc confSchema, pkg goPackage, sh *confShape, got string) string {
	sh, err := sc.resolve(sh)
	if err != nil {
		return err.Error()
	}
	switch sh.Kind {
	case "scalar":
		if want := goScalars[sh.Scalar]; want != "" && got == want {
			return ""
		}
		return fmt.Sprintf("unmapped scalar %s, got %s", sh.Scalar, got)
	case "node":
		// A node of a string is a pointer to the TNode the generator embeds, required or not.
		if sh.Value != nil && sh.Value.Kind == "scalar" && sh.Value.Scalar == "string" {
			if got == "*TNode[string]" {
				return ""
			}
			return fmt.Sprintf("want *TNode[string] for a node of a string, got %s", got)
		}
	case "ref":
		if strings.HasPrefix(sh.Ref, "declared/") {
			if want := "*" + strings.TrimPrefix(sh.Ref, "declared/"); got == want {
				return ""
			}
			return fmt.Sprintf("want %s for %s, got %s", "*"+strings.TrimPrefix(sh.Ref, "declared/"), sh.Ref, got)
		}
	case "list":
		if rest, ok := strings.CutPrefix(got, "[]"); ok {
			return matchGo(sc, pkg, sh.Element, rest)
		}
		return fmt.Sprintf("want a slice for a list, got %s", got)
	case "optional":
		// Optional does not change the Go type: references and nodes are already pointers, and a
		// scalar stays a value.
		return matchGo(sc, pkg, sh.Element, got)
	case "join":
		methods, ok := pkg.ifaces[got]
		if !ok {
			return fmt.Sprintf("want an interface for a join, got %s", got)
		}
		var alts []string
		for _, a := range sh.Alternatives {
			a, err := sc.resolve(a)
			if err != nil {
				return err.Error()
			}
			if a.Kind == "error" {
				continue // error recovery, which the schema does not list
			}
			if a.Kind != "ref" || !strings.HasPrefix(a.Ref, "declared/") {
				return fmt.Sprintf("unmapped alternative kind %s in a join", a.Kind)
			}
			alts = append(alts, strings.TrimPrefix(a.Ref, "declared/"))
		}
		if len(methods) != 1 {
			return fmt.Sprintf("interface %s has %d methods, want the one marker method", got, len(methods))
		}
		impl := pkg.implementers(methods[0])
		sort.Strings(alts)
		if strings.Join(impl, ",") != strings.Join(alts, ",") {
			return fmt.Sprintf("interface %s is implemented by %v, the join has %v", got, impl, alts)
		}
		return ""
	}
	return fmt.Sprintf("unmapped schema shape %s", sh.Kind)
}

// tsDecl is a TypeScript interface or alias of the generated types module.
type tsDecl struct {
	extends string
	fields  map[string]string
}

var (
	tsInterface = regexp.MustCompile(`^export interface (\w+)(?: extends ([^{]*))?\{$`)
	tsField     = regexp.MustCompile(`^\s*readonly "(\w+)": (.+);$`)
)

// parseTS reads the interfaces of a generated TypeScript types module.
func parseTS(src string) map[string]tsDecl {
	decls := map[string]tsDecl{}
	var cur string
	for _, line := range strings.Split(src, "\n") {
		if m := tsInterface.FindStringSubmatch(line); m != nil {
			cur = m[1]
			decls[cur] = tsDecl{extends: strings.TrimSpace(m[2]), fields: map[string]string{}}
			continue
		}
		if line == "}" {
			cur = ""
			continue
		}
		if m := tsField.FindStringSubmatch(line); m != nil && cur != "" {
			decls[cur].fields[m[1]] = m[2]
		}
	}
	return decls
}

// tsNames returns the names of a schema shape as TypeScript writes them in a union or a list: a
// reference is its name, a node of a string is TNode<string>, an optional is its element, and a join
// is the names of its alternatives.
func tsNames(sc confSchema, sh *confShape) ([]string, error) {
	sh, err := sc.resolve(sh)
	if err != nil {
		return nil, err
	}
	switch sh.Kind {
	case "ref":
		if strings.HasPrefix(sh.Ref, "declared/") {
			return []string{strings.TrimPrefix(sh.Ref, "declared/")}, nil
		}
	case "node":
		if sh.Value != nil && sh.Value.Kind == "scalar" && sh.Value.Scalar == "string" {
			return []string{"TNode<string>"}, nil
		}
	case "optional":
		return tsNames(sc, sh.Element)
	case "error":
		return nil, nil
	case "join":
		var out []string
		for _, a := range sh.Alternatives {
			names, err := tsNames(sc, a)
			if err != nil {
				return nil, err
			}
			out = append(out, names...)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unmapped schema shape %s", sh.Kind)
}

var tsList = regexp.MustCompile(`^readonly \(?(.*?)\)?\[\] \| null$`)

// goScalars and tsScalars are the Go and TypeScript types of the scalars of the schema.
var (
	goScalars = map[string]string{"string": "string", "bool": "bool", "int": "int"}
	tsScalars = map[string]string{"string": "string", "bool": "boolean", "int": "number | bigint"}
)

// matchTS returns a violation when the TypeScript field type text does not have the schema shape sh.
func matchTS(sc confSchema, sh *confShape, text string) string {
	sh, err := sc.resolve(sh)
	if err != nil {
		return err.Error()
	}
	switch sh.Kind {
	case "ref":
		if strings.HasPrefix(sh.Ref, "declared/") {
			want := strings.TrimPrefix(sh.Ref, "declared/") + " | null"
			if text == want {
				return ""
			}
			return fmt.Sprintf("want %q for %s, got %q", want, sh.Ref, text)
		}
	case "optional":
		// A reference or node is nullable; a scalar is not.
		return matchTS(sc, sh.Element, text)
	case "scalar":
		if want := tsScalars[sh.Scalar]; want != "" && text == want {
			return ""
		}
		return fmt.Sprintf("want %q for scalar %s, got %q", tsScalars[sh.Scalar], sh.Scalar, text)
	case "node":
		if sh.Value != nil && sh.Value.Kind == "scalar" && sh.Value.Scalar == "string" {
			if text == "TNode<string> | null" {
				return ""
			}
			return fmt.Sprintf("want %q for a node of a string, got %q", "TNode<string> | null", text)
		}
	case "list":
		m := tsList.FindStringSubmatch(text)
		if m == nil {
			return fmt.Sprintf("want a readonly array for a list, got %q", text)
		}
		want, err := tsNames(sc, sh.Element)
		if err != nil {
			return err.Error()
		}
		return sameNames(want, splitUnion(m[1]))
	case "join":
		want, err := tsNames(sc, sh)
		if err != nil {
			return err.Error()
		}
		return sameNames(want, splitUnion(text))
	}
	return fmt.Sprintf("unmapped schema shape %s", sh.Kind)
}

// splitUnion returns the members of a TypeScript union other than null and the recovery error.
func splitUnion(text string) []string {
	var out []string
	for _, m := range strings.Split(text, " | ") {
		if m == "null" || m == "RecoveredError" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// sameNames compares two sets of names.
func sameNames(want, got []string) string {
	w := append([]string(nil), want...)
	g := append([]string(nil), got...)
	sort.Strings(w)
	sort.Strings(g)
	if strings.Join(w, ",") != strings.Join(g, ",") {
		return fmt.Sprintf("want members %v, got %v", w, g)
	}
	return ""
}

// checkTS returns the violations of the generated TypeScript declarations of the schema.
func checkTS(sc confSchema, src string) []string {
	decls := parseTS(src)
	var out []string
	for _, typ := range sc.Types {
		if typ.Origin != "declared" {
			continue
		}
		decl, ok := decls[typ.Name]
		if !ok {
			out = append(out, "no generated TypeScript interface "+typ.Name)
			continue
		}
		switch typ.Shape.Kind {
		case "node":
			if decl.extends != "TNode<string>" {
				out = append(out, fmt.Sprintf("%s: interface extends %q, want TNode<string>", typ.Name, decl.extends))
			}
		case "struct":
			if decl.extends != "Span" {
				out = append(out, fmt.Sprintf("%s: interface extends %q, want Span", typ.Name, decl.extends))
			}
			schemaNames := map[string]bool{}
			for _, f := range typ.Shape.Fields {
				schemaNames[f.Name] = true
				text, ok := decl.fields[f.Name]
				if !ok {
					out = append(out, fmt.Sprintf("%s.%s: schema field has no TypeScript field", typ.Name, f.Name))
					continue
				}
				if v := matchTS(sc, f.Shape, text); v != "" {
					out = append(out, fmt.Sprintf("%s.%s: %s", typ.Name, f.Name, v))
				}
			}
			for name := range decl.fields {
				if !schemaNames[name] {
					out = append(out, fmt.Sprintf("%s.%s: TypeScript field is not in the schema", typ.Name, name))
				}
			}
		default:
			out = append(out, fmt.Sprintf("%s: unmapped schema shape %s for a declared type", typ.Name, typ.Shape.Kind))
		}
	}
	sort.Strings(out)
	return out
}

// typedGeneration is one generated typed package: its grammar, its Go types file and its TypeScript.
type typedGeneration struct {
	name    string
	grammar string
	goTypes string
}

func typedGenerations(t *testing.T) []typedGeneration {
	t.Helper()
	var out []typedGeneration
	for _, dir := range []string{".", "../bench"} {
		for _, g := range generationsIn(t, dir) {
			if !g.hasTypes {
				continue
			}
			out = append(out, typedGeneration{
				name:    filepath.ToSlash(g.out),
				grammar: g.grammar,
				goTypes: filepath.Join(g.out, "types", "types.go"),
			})
		}
	}
	if len(out) == 0 {
		t.Fatal("no generated typed packages")
	}
	return out
}

// checkTypedGeneration returns the violations of one generated package against a schema.
func checkTypedGeneration(t *testing.T, sc confSchema, g typedGeneration, src string) []string {
	t.Helper()
	pkg := parseGoTypes(t, g.goTypes)
	var out []string
	for _, typ := range sc.Types {
		if typ.Origin != "declared" {
			continue
		}
		for _, v := range checkGoType(sc, pkg, typ) {
			out = append(out, "Go: "+v)
		}
	}
	gr, err := zeroparse.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := gen.TypeScript{Types: true}.Generate(gr, "")
	if err != nil {
		t.Fatal(err)
	}
	var typesTS string
	for _, f := range ts {
		if f.Path == "types.ts" {
			typesTS = string(f.Content)
		}
	}
	if typesTS == "" {
		t.Fatal("no generated types.ts")
	}
	for _, v := range checkTS(sc, typesTS) {
		out = append(out, "TypeScript: "+v)
	}
	return out
}

// typedDivergence lists the violations that are known generator divergences: the mapping above decides
// the shape, and the generated type is not what it decides. Each entry is reported as known rather than
// hidden, and an entry that no longer has a violation fails the test, so the list stays current. A
// divergence is a generator change to make, not a case of the mapping.
var typedDivergence = map[string][]string{}

// TestTypedConformance checks that every generated typed package of parsers and internal/bench/gen conforms to
// the output schema of its grammar, in Go and in TypeScript.
func TestTypedConformance(t *testing.T) {
	for _, g := range typedGenerations(t) {
		t.Run(g.name, func(t *testing.T) {
			src, err := os.ReadFile(g.grammar)
			if err != nil {
				t.Fatal(err)
			}
			sc := conformanceSchema(t, g.grammar)
			divergence := typedDivergence[g.name]
			seen := make([]bool, len(divergence))
			for _, v := range checkTypedGeneration(t, sc, g, string(src)) {
				matched := false
				for i, prefix := range divergence {
					if strings.HasPrefix(v, prefix) {
						seen[i], matched = true, true
					}
				}
				if matched {
					t.Logf("known generator divergence: %s", v)
					continue
				}
				t.Error(v)
			}
			for i, prefix := range divergence {
				if !seen[i] {
					t.Errorf("stale known divergence %q: no violation has it", prefix)
				}
			}
		})
	}
}

// TestTypedConformanceDetectsCardinality is the negative control of TestTypedConformance: a schema
// whose list field is changed to a single reference must be reported in both languages.
func TestTypedConformanceDetectsCardinality(t *testing.T) {
	var g typedGeneration
	for _, gen := range typedGenerations(t) {
		if gen.name == "json" {
			g = gen
		}
	}
	if g.grammar == "" {
		t.Fatal("no json typed package")
	}
	src, err := os.ReadFile(g.grammar)
	if err != nil {
		t.Fatal(err)
	}
	sc := conformanceSchema(t, g.grammar)
	mutated := false
	for i := range sc.Types {
		if sc.Types[i].ID != "declared/Object" {
			continue
		}
		for j := range sc.Types[i].Shape.Fields {
			f := &sc.Types[i].Shape.Fields[j]
			if f.Name == "Members" && f.Shape.Kind == "list" {
				f.Shape = &confShape{Kind: "ref", Ref: "declared/Member"}
				mutated = true
			}
		}
	}
	if !mutated {
		t.Fatal("the JSON schema has no list field Object.Members to mutate")
	}
	violations := checkTypedGeneration(t, sc, g, string(src))
	var goHit, tsHit bool
	for _, v := range violations {
		goHit = goHit || strings.Contains(v, "Go: Object.Members")
		tsHit = tsHit || strings.Contains(v, "TypeScript: Object.Members")
	}
	if !goHit || !tsHit {
		t.Fatalf("mutated cardinality not reported (Go %t, TypeScript %t): %v", goHit, tsHit, violations)
	}
}
