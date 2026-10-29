// Package schema describes the outputs a grammar can produce, independently of a parse: the output
// schema, its JSON form and its validation, and the structural queries that match patterns against
// parse results and check them against a schema.
package schema

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// ErrUnavailable reports missing checked grammar metadata: it is compile.ErrSchemaUnavailable,
// which the runtime returns for a program that has no schema.
var ErrUnavailable = compile.ErrSchemaUnavailable

// version is the version of the output schema format.
const version = 2

// Output is an experimental, grammar-level description of possible values.
// It is independent of position units and generated target-language layouts.
type Output struct {
	Format                  string `json:"format"`
	Version                 int    `json:"version"`
	Start                   string `json:"start"`
	PositionUnitIndependent bool   `json:"positionUnitIndependent"`
	// SourceInfo reports that the grammar was compiled with source information, so that nodes
	// record the rules that produced them.
	SourceInfo bool   `json:"sourceInfo"`
	Types      []Type `json:"types"`
	Rules      []Rule `json:"rules"`
}

// Type is a struct type defined in the grammar (origin "declared") or the value of a rule
// (origin "inferred").
type Type struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Shape  Shape  `json:"shape"`
}

// Rule is a rule. Its result refers to the inferred type of the rule; the origin is
// "declared" when the rule declares its type.
type Rule struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Result Shape  `json:"result"`
}

// Shape describes the values of a type. Kind selects the members:
//
//   - "scalar": Scalar is int, string or bool.
//   - "nil", "top" (every value), "error" (a value of error recovery) and "missing" (a token that
//     recovery inserted, a kind of error) have no members.
//   - "node": Value is the shape of the node's value, and Captures locates its captures.
//   - "tuple": Elements are the shapes of the elements, in order; the unit tuple has none.
//   - "list" and "optional": Element is the shape of the elements, or of the value when present.
//   - "join": Alternatives are the shapes of the alternatives.
//   - "struct": Fields are the fields of a defined struct type.
//   - "ref": Ref is the ID of a type of the schema; recursive types are never expanded.
type Shape struct {
	Kind         string    `json:"kind"`
	Ref          string    `json:"ref,omitempty"`
	Scalar       string    `json:"scalar,omitempty"`
	Element      *Shape    `json:"element,omitempty"`
	Elements     []Shape   `json:"elements,omitempty"`
	Alternatives []Shape   `json:"alternatives,omitempty"`
	Fields       []Field   `json:"fields,omitempty"`
	Value        *Shape    `json:"value,omitempty"`
	Captures     []Capture `json:"captures,omitempty"`
}

// Field is a field of a struct type.
type Field struct {
	Name  string `json:"name"`
	Shape Shape  `json:"shape"`
}

// Capture locates a capture of a node. Each path is a path of tuple indexes inside the
// node's value, one for each place the capture can be; the alternative that matched selects one.
// Side reports that the capture can also be in a positive lookahead, whose value is kept beside
// the tuple. Shape is the shape of the capture's value.
type Capture struct {
	Name  string  `json:"name"`
	Paths [][]int `json:"paths,omitempty"`
	Side  bool    `json:"side,omitempty"`
	Shape Shape   `json:"shape"`
}

// Validate checks the version 2 envelope and shape graph. It also accepts
// schemas constructed directly in memory; named references may be recursive.
func (s *Output) Validate() error {
	if s == nil {
		return fmt.Errorf("nil output schema")
	}
	if s.Format != "pego-output-schema" || s.Version != version {
		return fmt.Errorf("unsupported output schema format %q version %d", s.Format, s.Version)
	}
	if s.Start == "" || !s.PositionUnitIndependent || s.Types == nil || s.Rules == nil {
		return fmt.Errorf("invalid output schema envelope: start, positionUnitIndependent, types and rules are required")
	}
	return validateOutputSchemaGraph(*s)
}

// UnmarshalJSON rejects unsupported envelopes, noncanonical fields, duplicate
// keys and malformed shape graphs.
func (s *Output) UnmarshalJSON(data []byte) error {
	validated, err := decodeOutputSchemaJSON(data)
	if err != nil {
		return err
	}
	if err := validated.Validate(); err != nil {
		return err
	}
	*s = validated
	return nil
}

// Export exports the schema of the checked program p for the start rule start: the rule, the rules
// it calls or whose values its values contain, and the struct types they refer to; or every rule
// and type with allRules. engine.Program.SchemaProgram checks the start rule first.
func Export(p *compile.Program, start string, allRules bool) (*Output, error) {
	if p.Grammar == nil || p.Typed == nil {
		return nil, ErrUnavailable
	}
	if p.ByName[start] == nil {
		return nil, fmt.Errorf("unknown schema start rule %q", start)
	}
	// Without all rules, the export is the start rule's reachable part, which is stored per start rule.
	if !allRules && !slices.Contains(p.StartRules, start) {
		return nil, fmt.Errorf("unknown schema start rule %q", start)
	}
	values := p.ValueTypes()
	if values == nil {
		return nil, ErrUnavailable
	}
	all := map[string]Type{}
	for _, td := range p.Grammar.Types() {
		spec, ok := td.(*grammar.TypeDef)
		if !ok {
			continue // aliases name other types
		}
		id := "declared/" + td.DeclName()
		if spec.Struct() == nil {
			// A named node type has the shape of its underlying node type.
			n, ok := p.NodeTypeOf(td.DeclName())
			if !ok {
				return nil, fmt.Errorf("schema type %q: unresolved node type", td.DeclName())
			}
			shape, err := shapeOf(n.Underlying())
			if err != nil {
				return nil, fmt.Errorf("schema type %q: %w", td.DeclName(), err)
			}
			all[id] = Type{id, td.DeclName(), "declared", shape}
			continue
		}
		shape := Shape{Kind: "struct"}
		for _, f := range compile.DefFields(spec) {
			fieldShape, err := declared(p, f.Type)
			if err != nil {
				return nil, fmt.Errorf("schema type %q field %q: %w", td.DeclName(), f.Name, err)
			}
			shape.Fields = append(shape.Fields, Field{f.Name, fieldShape})
		}
		sort.Slice(shape.Fields, func(i, j int) bool { return shape.Fields[i].Name < shape.Fields[j].Name })
		all[id] = Type{id, td.DeclName(), "declared", shape}
	}
	s := &Output{Format: "pego-output-schema", Version: version, Start: start, PositionUnitIndependent: true, SourceInfo: p.RecordsSourceInfo, Types: []Type{}, Rules: []Rule{}}
	selected := map[string]bool{}
	noType := map[string]bool{}
	wanted := map[string]bool{}
	var todo []string
	var collect func(Shape)
	collect = func(shape Shape) {
		if shape.Ref != "" && !wanted[shape.Ref] {
			wanted[shape.Ref] = true
			if t, ok := all[shape.Ref]; ok {
				collect(t.Shape)
			} else if name, ok := cutPrefix(shape.Ref, "inferred/"); ok {
				todo = append(todo, name)
			}
		}
		for _, sub := range []*Shape{shape.Element, shape.Value} {
			if sub != nil {
				collect(*sub)
			}
		}
		for _, list := range [][]Shape{shape.Elements, shape.Alternatives} {
			for _, sub := range list {
				collect(sub)
			}
		}
		for _, f := range shape.Fields {
			collect(f.Shape)
		}
		for _, c := range shape.Captures {
			collect(c.Shape)
		}
	}
	todo = append(todo, start)
	if allRules {
		for _, r := range p.Grammar.Rules() {
			todo = append(todo, r.Name)
		}
	}
	for len(todo) > 0 {
		name := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if selected[name] {
			continue
		}
		selected[name] = true
		rd := p.ByName[name]
		if rd == nil {
			return nil, fmt.Errorf("unresolved schema rule %q", name)
		}
		for _, ref := range compile.AllCalls(rd.Def.Expr, nil) {
			if !selected[ref] {
				todo = append(todo, ref)
			}
		}
		t, ok := values[name]
		if _, splice := p.SpliceBody(name); splice && !ok {
			noType[name] = true // a rule that makes no node has a type only where another type refers to it
			continue
		}
		if !ok || t == nil {
			return nil, ErrUnavailable
		}
		shape, err := shapeOf(t)
		if err != nil {
			return nil, fmt.Errorf("schema rule %q: %w", name, err)
		}
		id := "inferred/" + name
		all[id] = Type{id, name, "inferred", shape}
		wanted[id] = true
		collect(shape)
	}
	for _, rd := range p.Grammar.Rules() {
		if !selected[rd.Name] || noType[rd.Name] {
			continue
		}
		origin := "inferred"
		if rd.Type != nil {
			origin = "declared"
		}
		s.Rules = append(s.Rules, Rule{rd.Name, origin, Shape{Kind: "ref", Ref: "inferred/" + rd.Name}})
	}
	if allRules {
		for id := range all {
			wanted[id] = true
		}
	}
	for id := range wanted {
		t, ok := all[id]
		if !ok {
			return nil, fmt.Errorf("unresolved schema reference %q", id)
		}
		s.Types = append(s.Types, t)
	}
	sort.Slice(s.Types, func(i, j int) bool { return s.Types[i].ID < s.Types[j].ID })
	sort.Slice(s.Rules, func(i, j int) bool { return s.Rules[i].Name < s.Rules[j].Name })
	return s, nil
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

// declared describes the type a field declares.
func declared(p *compile.Program, t grammar.TypeExpr) (Shape, error) {
	k := &compile.TypeResolver{Prog: p}
	return shapeOf(k.Resolve(t))
}

// shapeOf describes a type of the value model.
func shapeOf(t compile.Ty) (Shape, error) {
	switch t := t.(type) {
	case compile.BasicTy:
		switch t {
		case compile.TyBottom:
			return Shape{Kind: "join"}, nil // no values
		case compile.TyInt, compile.TyString, compile.TyBool:
			return Shape{Kind: "scalar", Scalar: string(t)}, nil
		case compile.TyNil:
			return Shape{Kind: "nil"}, nil
		case compile.TyErr:
			return Shape{Kind: "error"}, nil
		case compile.TyTop, compile.TyAny:
			return Shape{Kind: "top"}, nil
		}
		return Shape{}, fmt.Errorf("unsupported schema type %q", t)
	case compile.NamedTy:
		if t == compile.TyMissing {
			return Shape{Kind: "missing"}, nil
		}
		if t.Kind != 's' {
			v := compile.FromLegacy(t)
			if n, ok := v.(compile.NamedTy); !ok || n.Kind == 's' {
				return shapeOf(v)
			}
			return Shape{}, fmt.Errorf("unresolved schema type %s", t.Name)
		}
		return Shape{Kind: "ref", Ref: "declared/" + t.Name}, nil
	case compile.RuleTy:
		return Shape{Kind: "ref", Ref: "inferred/" + t.Name}, nil
	case compile.NodeTy:
		if t.Name != "" {
			return Shape{Kind: "ref", Ref: "declared/" + t.Name}, nil
		}
		v, err := shapeOf(t.Value)
		if err != nil {
			return Shape{}, err
		}
		s := Shape{Kind: "node", Value: &v}
		// A name captured in several places (an alternative, or a lookahead beside the tuple)
		// has one entry: its locations are the union of the places, and its type is the join of
		// the types of the places, joined once so that the result is flat and sorted.
		type capGroup struct {
			name  string
			paths [][]int
			side  bool
			types []compile.Ty
		}
		var groups []*capGroup
		index := map[string]*capGroup{}
		for _, c := range t.Caps {
			g, ok := index[c.Name]
			if !ok {
				g = &capGroup{name: c.Name}
				index[c.Name] = g
				groups = append(groups, g)
			}
			var paths [][]int
			for _, path := range c.Paths {
				paths = append(paths, append([]int{}, path...))
			}
			g.paths = mergeCapturePaths(g.paths, paths)
			g.side = g.side || c.Side
			g.types = append(g.types, c.T)
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].name < groups[j].name })
		for _, g := range groups {
			cs, err := shapeOf(compile.Vjoin(g.types...))
			if err != nil {
				return Shape{}, fmt.Errorf("capture %s: %w", g.name, err)
			}
			s.Captures = append(s.Captures, Capture{Name: g.name, Paths: g.paths, Side: g.side, Shape: cs})
		}
		return s, nil
	case compile.TupleTy:
		s := Shape{Kind: "tuple"}
		for _, e := range t.Elems {
			es, err := shapeOf(e)
			if err != nil {
				return Shape{}, err
			}
			s.Elements = append(s.Elements, es)
		}
		return s, nil
	case compile.ListTy:
		e, err := shapeOf(t.Elem)
		return Shape{Kind: "list", Element: &e}, err
	case compile.OptTy:
		e, err := shapeOf(t.Elem)
		return Shape{Kind: "optional", Element: &e}, err
	case compile.JoinTy:
		s := Shape{Kind: "join"}
		for _, a := range t.Alts {
			v, err := shapeOf(a)
			if err != nil {
				return s, err
			}
			s.Alternatives = append(s.Alternatives, v)
		}
		sortSchemaAlternatives(s.Alternatives)
		return s, nil
	case compile.RecordTy:
		return shapeOf(compile.FromLegacy(t))
	}
	return Shape{}, fmt.Errorf("unresolved schema type %T", t)
}

func sortSchemaAlternatives(alts []Shape) {
	sort.Slice(alts, func(i, j int) bool {
		a, _ := json.Marshal(alts[i])
		b, _ := json.Marshal(alts[j])
		return string(a) < string(b)
	})
}

// mergeCapturePaths returns the sorted union of two sets of capture paths, without duplicates.
func mergeCapturePaths(a, b [][]int) [][]int {
	out := append(append([][]int{}, a...), b...)
	sort.Slice(out, func(i, j int) bool { return slices.Compare(out[i], out[j]) < 0 })
	return slices.CompactFunc(out, func(x, y []int) bool { return slices.Equal(x, y) })
}
