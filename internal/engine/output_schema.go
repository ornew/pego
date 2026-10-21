package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/ornew/pego/grammar"
)

// ErrSchemaUnavailable reports missing checked grammar metadata.
var ErrSchemaUnavailable = errors.New("output schema unavailable")

// OutputSchemaVersion is the version of the output schema format.
const OutputSchemaVersion = 2

// OutputSchema is an experimental, grammar-level description of possible values.
// It is independent of position units and generated target-language layouts.
type OutputSchema struct {
	Format                  string `json:"format"`
	Version                 int    `json:"version"`
	Start                   string `json:"start"`
	PositionUnitIndependent bool   `json:"positionUnitIndependent"`
	// SourceInfo reports that the grammar was compiled with source information, so that nodes
	// record the rules that produced them.
	SourceInfo bool         `json:"sourceInfo"`
	Types      []SchemaType `json:"types"`
	Rules      []SchemaRule `json:"rules"`
}

// SchemaType is a struct type defined in the grammar (origin "declared") or the value of a rule
// (origin "inferred").
type SchemaType struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Origin string      `json:"origin"`
	Shape  SchemaShape `json:"shape"`
}

// SchemaRule is a rule. Its result refers to the inferred type of the rule; the origin is
// "declared" when the rule declares its type.
type SchemaRule struct {
	Name   string      `json:"name"`
	Origin string      `json:"origin"`
	Result SchemaShape `json:"result"`
}

// SchemaShape describes the values of a type. Kind selects the members:
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
type SchemaShape struct {
	Kind         string          `json:"kind"`
	Ref          string          `json:"ref,omitempty"`
	Scalar       string          `json:"scalar,omitempty"`
	Element      *SchemaShape    `json:"element,omitempty"`
	Elements     []SchemaShape   `json:"elements,omitempty"`
	Alternatives []SchemaShape   `json:"alternatives,omitempty"`
	Fields       []SchemaField   `json:"fields,omitempty"`
	Value        *SchemaShape    `json:"value,omitempty"`
	Captures     []SchemaCapture `json:"captures,omitempty"`
}

// SchemaField is a field of a struct type.
type SchemaField struct {
	Name  string      `json:"name"`
	Shape SchemaShape `json:"shape"`
}

// SchemaCapture locates a capture of a node. Each path is a path of tuple indexes inside the
// node's value, one for each place the capture can be; the alternative that matched selects one.
// Side reports that the capture can also be in a positive lookahead, whose value is kept beside
// the tuple. Shape is the shape of the capture's value.
type SchemaCapture struct {
	Name  string      `json:"name"`
	Paths [][]int     `json:"paths,omitempty"`
	Side  bool        `json:"side,omitempty"`
	Shape SchemaShape `json:"shape"`
}

// Validate checks the version 2 envelope and shape graph. It also accepts
// schemas constructed directly in memory; named references may be recursive.
func (s *OutputSchema) Validate() error {
	if s == nil {
		return fmt.Errorf("nil output schema")
	}
	if s.Format != "pego-output-schema" || s.Version != OutputSchemaVersion {
		return fmt.Errorf("unsupported output schema format %q version %d", s.Format, s.Version)
	}
	if s.Start == "" || !s.PositionUnitIndependent || s.Types == nil || s.Rules == nil {
		return fmt.Errorf("invalid output schema envelope: start, positionUnitIndependent, types and rules are required")
	}
	return validateOutputSchemaGraph(*s)
}

// UnmarshalJSON rejects unsupported envelopes, noncanonical fields, duplicate
// keys and malformed shape graphs.
func (s *OutputSchema) UnmarshalJSON(data []byte) error {
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

// OutputSchemaPrototype exports the start rule, the rules it calls or whose values its values
// contain, and the struct types they refer to; or every rule and type with allRules. It has no
// effect on parsing or generation.
func (p *Program) OutputSchemaPrototype(start string, allRules bool) (*OutputSchema, error) {
	if p == nil || p.Grammar == nil || p.typed == nil {
		return nil, ErrSchemaUnavailable
	}
	if err := EntryError(start); err != nil {
		return nil, err
	}
	if p.byName[start] == nil {
		return nil, fmt.Errorf("unknown schema start rule %q", start)
	}
	values := p.valueTypes()
	if values == nil {
		return nil, ErrSchemaUnavailable
	}
	all := map[string]SchemaType{}
	for _, td := range p.Grammar.Types() {
		spec, ok := td.(*grammar.TypeDef)
		if !ok {
			continue // aliases name other types
		}
		id := "declared/" + td.DeclName()
		if spec.Struct() == nil {
			// A named node type has the shape of its underlying node type.
			n, ok := p.nodeTypeOf(td.DeclName())
			if !ok {
				return nil, fmt.Errorf("schema type %q: unresolved node type", td.DeclName())
			}
			shape, err := schemaValue(n.underlying())
			if err != nil {
				return nil, fmt.Errorf("schema type %q: %w", td.DeclName(), err)
			}
			all[id] = SchemaType{id, td.DeclName(), "declared", shape}
			continue
		}
		shape := SchemaShape{Kind: "struct"}
		for _, f := range defFields(spec) {
			fieldShape, err := p.schemaDeclared(f.Type)
			if err != nil {
				return nil, fmt.Errorf("schema type %q field %q: %w", td.DeclName(), f.Name, err)
			}
			shape.Fields = append(shape.Fields, SchemaField{f.Name, fieldShape})
		}
		sort.Slice(shape.Fields, func(i, j int) bool { return shape.Fields[i].Name < shape.Fields[j].Name })
		all[id] = SchemaType{id, td.DeclName(), "declared", shape}
	}
	s := &OutputSchema{Format: "pego-output-schema", Version: OutputSchemaVersion, Start: start, PositionUnitIndependent: true, SourceInfo: p.sourceInfo, Types: []SchemaType{}, Rules: []SchemaRule{}}
	selected := map[string]bool{}
	wanted := map[string]bool{}
	var todo []string
	var collect func(SchemaShape)
	collect = func(shape SchemaShape) {
		if shape.Ref != "" && !wanted[shape.Ref] {
			wanted[shape.Ref] = true
			if t, ok := all[shape.Ref]; ok {
				collect(t.Shape)
			} else if name, ok := cutPrefix(shape.Ref, "inferred/"); ok {
				todo = append(todo, name)
			}
		}
		for _, sub := range []*SchemaShape{shape.Element, shape.Value} {
			if sub != nil {
				collect(*sub)
			}
		}
		for _, list := range [][]SchemaShape{shape.Elements, shape.Alternatives} {
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
		rd := p.byName[name]
		if rd == nil {
			return nil, fmt.Errorf("unresolved schema rule %q", name)
		}
		for _, ref := range allCalls(rd.def.Expr, nil) {
			if !selected[ref] {
				todo = append(todo, ref)
			}
		}
		t, ok := values[name]
		if !ok || t == nil {
			return nil, ErrSchemaUnavailable
		}
		shape, err := schemaValue(t)
		if err != nil {
			return nil, fmt.Errorf("schema rule %q: %w", name, err)
		}
		id := "inferred/" + name
		all[id] = SchemaType{id, name, "inferred", shape}
		wanted[id] = true
		collect(shape)
	}
	for _, rd := range p.Grammar.Rules() {
		if !selected[rd.Name] {
			continue
		}
		origin := "inferred"
		if rd.Type != nil {
			origin = "declared"
		}
		s.Rules = append(s.Rules, SchemaRule{rd.Name, origin, SchemaShape{Kind: "ref", Ref: "inferred/" + rd.Name}})
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

// schemaDeclared describes the type a field declares.
func (p *Program) schemaDeclared(t grammar.TypeExpr) (SchemaShape, error) {
	k := &typeResolver{prog: p}
	return schemaValue(k.resolve(t))
}

// schemaValue describes a type of the value model.
func schemaValue(t ty) (SchemaShape, error) {
	switch t := t.(type) {
	case basicTy:
		switch t {
		case tyBottom:
			return SchemaShape{Kind: "join"}, nil // no values
		case tyInt, tyString, tyBool:
			return SchemaShape{Kind: "scalar", Scalar: string(t)}, nil
		case tyNil:
			return SchemaShape{Kind: "nil"}, nil
		case tyErr:
			return SchemaShape{Kind: "error"}, nil
		case tyTop, tyAny:
			return SchemaShape{Kind: "top"}, nil
		}
		return SchemaShape{}, fmt.Errorf("unsupported schema type %q", t)
	case namedTy:
		if t == tyMissing {
			return SchemaShape{Kind: "missing"}, nil
		}
		if t.kind != 's' {
			v := fromLegacy(t)
			if n, ok := v.(namedTy); !ok || n.kind == 's' {
				return schemaValue(v)
			}
			return SchemaShape{}, fmt.Errorf("unresolved schema type %s", t.name)
		}
		return SchemaShape{Kind: "ref", Ref: "declared/" + t.name}, nil
	case ruleTy:
		return SchemaShape{Kind: "ref", Ref: "inferred/" + t.name}, nil
	case nodeTy:
		if t.name != "" {
			return SchemaShape{Kind: "ref", Ref: "declared/" + t.name}, nil
		}
		v, err := schemaValue(t.value)
		if err != nil {
			return SchemaShape{}, err
		}
		s := SchemaShape{Kind: "node", Value: &v}
		for _, c := range t.caps {
			cs, err := schemaValue(c.t)
			if err != nil {
				return SchemaShape{}, fmt.Errorf("capture %s: %w", c.name, err)
			}
			var paths [][]int
			for _, path := range c.paths {
				paths = append(paths, append([]int{}, path...))
			}
			sort.Slice(paths, func(i, j int) bool { return slices.Compare(paths[i], paths[j]) < 0 })
			s.Captures = append(s.Captures, SchemaCapture{Name: c.name, Paths: paths, Side: c.side, Shape: cs})
		}
		return s, nil
	case tupleTy:
		s := SchemaShape{Kind: "tuple"}
		for _, e := range t.elems {
			es, err := schemaValue(e)
			if err != nil {
				return SchemaShape{}, err
			}
			s.Elements = append(s.Elements, es)
		}
		return s, nil
	case listTy:
		e, err := schemaValue(t.elem)
		return SchemaShape{Kind: "list", Element: &e}, err
	case optTy:
		e, err := schemaValue(t.elem)
		return SchemaShape{Kind: "optional", Element: &e}, err
	case joinTy:
		s := SchemaShape{Kind: "join"}
		for _, a := range t.alts {
			v, err := schemaValue(a)
			if err != nil {
				return s, err
			}
			s.Alternatives = append(s.Alternatives, v)
		}
		sortSchemaAlternatives(s.Alternatives)
		return s, nil
	case recordTy:
		return schemaValue(fromLegacy(t))
	}
	return SchemaShape{}, fmt.Errorf("unresolved schema type %T", t)
}

func sortSchemaAlternatives(alts []SchemaShape) {
	sort.Slice(alts, func(i, j int) bool {
		a, _ := json.Marshal(alts[i])
		b, _ := json.Marshal(alts[j])
		return string(a) < string(b)
	})
}
