package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ornew/pego/grammar"
)

// ErrSchemaUnavailable reports missing checked grammar metadata.
var ErrSchemaUnavailable = errors.New("output schema unavailable")

// OutputSchema is an experimental, grammar-level description of possible values.
// It is independent of position units and generated target-language layouts.
type OutputSchema struct {
	Format                  string       `json:"format"`
	Version                 int          `json:"version"`
	Start                   string       `json:"start"`
	PositionUnitIndependent bool         `json:"positionUnitIndependent"`
	Types                   []SchemaType `json:"types"`
	Rules                   []SchemaRule `json:"rules"`
}
type SchemaType struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Origin string      `json:"origin"`
	Shape  SchemaShape `json:"shape"`
}
type SchemaRule struct {
	Name   string      `json:"name"`
	Origin string      `json:"origin"`
	Result SchemaShape `json:"result"`
}

// SchemaShape preserves cardinality and scalar/node categories. References use
// namespace-qualified identities; recursive named types are never expanded.
type SchemaShape struct {
	Kind         string        `json:"kind"`
	Ref          string        `json:"ref,omitempty"`
	Scalar       string        `json:"scalar,omitempty"`
	Element      *SchemaShape  `json:"element,omitempty"`
	Alternatives []SchemaShape `json:"alternatives,omitempty"`
	Fields       []SchemaField `json:"fields,omitempty"`
	Children     *SchemaShape  `json:"children,omitempty"`
}
type SchemaField struct {
	Name   string      `json:"name"`
	Origin string      `json:"origin"`
	Shape  SchemaShape `json:"shape"`
}

// UnmarshalJSON rejects envelopes from unsupported schema formats.
func (s *OutputSchema) UnmarshalJSON(data []byte) error {
	type plain OutputSchema
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Format != "pego-output-schema" || v.Version != 1 {
		return fmt.Errorf("unsupported output schema format %q version %d", v.Format, v.Version)
	}
	*s = OutputSchema(v)
	return nil
}

// OutputSchemaPrototype exports start-reachable rules and type references, or
// every rule/type with allRules. It has no effect on parsing or generation.
func (p *Program) OutputSchemaPrototype(start string, allRules bool) (*OutputSchema, error) {
	if p == nil || p.Grammar == nil || p.typed == nil {
		return nil, ErrSchemaUnavailable
	}
	if p.byName[start] == nil {
		return nil, fmt.Errorf("unknown schema start rule %q", start)
	}
	all := map[string]SchemaType{}
	for _, name := range reservedTypes {
		shape := SchemaShape{Kind: "terminal"}
		if name == TypeSeq || name == TypeList || name == TypeOperator {
			element := SchemaShape{Kind: "optional", Element: &SchemaShape{Kind: "node"}}
			shape = SchemaShape{Kind: "cst", Children: &SchemaShape{Kind: "list", Element: &element}}
		}
		all["cst/"+name] = SchemaType{ID: "cst/" + name, Name: name, Origin: "cst", Shape: shape}
	}
	for _, td := range p.Grammar.Types() {
		var shape SchemaShape
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			shape.Kind = "struct"
			for _, f := range spec.Fields {
				fieldShape, err := p.schemaDeclared(f.Type)
				if err != nil {
					return nil, fmt.Errorf("schema type %q field %q: %w", td.Name, f.Name, err)
				}
				shape.Fields = append(shape.Fields, SchemaField{f.Name, "declared", fieldShape})
			}
			sort.Slice(shape.Fields, func(i, j int) bool { return shape.Fields[i].Name < shape.Fields[j].Name })
		case *grammar.TerminalSpec:
			shape.Kind = "terminal"
		case *grammar.AliasSpec:
			var err error
			shape, err = p.schemaDeclared(spec.Type)
			if err != nil {
				return nil, fmt.Errorf("schema type %q: %w", td.Name, err)
			}
		default:
			return nil, fmt.Errorf("unsupported schema type %q", td.Name)
		}
		id := "declared/" + td.Name
		all[id] = SchemaType{id, td.Name, "declared", shape}
	}
	selected := map[string]bool{}
	todo := []string{start}
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
		for _, ref := range allCalls(p.byName[name].def.Expr, nil) {
			if !selected[ref] {
				todo = append(todo, ref)
			}
		}
	}
	s := &OutputSchema{Format: "pego-output-schema", Version: 1, Start: start, PositionUnitIndependent: true, Types: []SchemaType{}, Rules: []SchemaRule{}}
	for _, rd := range p.Grammar.Rules() {
		if !selected[rd.Name] {
			continue
		}
		t, ok := p.typed.rules[rd.Name]
		if !ok || t == nil {
			return nil, ErrSchemaUnavailable
		}
		shape, err := schemaInferred(t)
		if err != nil {
			return nil, err
		}
		origin := "inferred"
		if rd.Type != nil {
			origin = "declared"
			shape, err = p.schemaDeclared(rd.Type)
			if err != nil {
				return nil, fmt.Errorf("schema rule %q: %w", rd.Name, err)
			}
		}
		id := "inferred/" + rd.Name
		all[id] = SchemaType{id, rd.Name, "inferred", shape}
		s.Rules = append(s.Rules, SchemaRule{rd.Name, origin, SchemaShape{Kind: "ref", Ref: id}})
	}
	wanted := map[string]bool{}
	var collect func(SchemaShape)
	collect = func(shape SchemaShape) {
		// A generic node child can be any reserved CST kind. Keeping those
		// identities in the export lets schema-checked queries name a Match
		// below a Seq even when no capture or rule refers to Match directly.
		if shape.Kind == "node" {
			for _, name := range reservedTypes {
				collect(SchemaShape{Kind: "ref", Ref: "cst/" + name})
			}
		}
		if shape.Ref != "" && !wanted[shape.Ref] {
			wanted[shape.Ref] = true
			if t, ok := all[shape.Ref]; ok {
				collect(t.Shape)
			}
		}
		if shape.Element != nil {
			collect(*shape.Element)
		}
		if shape.Children != nil {
			collect(*shape.Children)
		}
		for _, a := range shape.Alternatives {
			collect(a)
		}
		for _, f := range shape.Fields {
			collect(f.Shape)
		}
	}
	for _, r := range s.Rules {
		collect(r.Result)
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
func (p *Program) schemaDeclared(t grammar.TypeExpr) (SchemaShape, error) {
	switch t := t.(type) {
	case *grammar.TypeRef:
		if _, ok := p.types[t.Name]; ok {
			return SchemaShape{Kind: "ref", Ref: "declared/" + t.Name}, nil
		}
		for _, name := range reservedTypes {
			if t.Name == name {
				return SchemaShape{Kind: "ref", Ref: "cst/" + name}, nil
			}
		}
		switch t.Name {
		case "int", "string", "bool":
			return SchemaShape{Kind: "scalar", Scalar: t.Name}, nil
		case "any", "node", "terminal":
			return SchemaShape{Kind: t.Name}, nil
		}
		return SchemaShape{}, fmt.Errorf("unresolved schema type reference %q", t.Name)
	case *grammar.ListType:
		e, err := p.schemaDeclared(t.Elem)
		return SchemaShape{Kind: "list", Element: &e}, err
	case *grammar.OptionalType:
		e, err := p.schemaDeclared(t.Elem)
		return SchemaShape{Kind: "optional", Element: &e}, err
	case *grammar.UnionType:
		s := SchemaShape{Kind: "union"}
		for _, a := range t.Types {
			alternative, err := p.schemaDeclared(a)
			if err != nil {
				return SchemaShape{}, err
			}
			s.Alternatives = append(s.Alternatives, alternative)
		}
		sortSchemaAlternatives(s.Alternatives)
		return s, nil
	case *grammar.TypeGroup:
		return p.schemaDeclared(t.Type)
	}
	return SchemaShape{}, fmt.Errorf("unsupported schema type expression %T", t)
}
func schemaInferred(t ty) (SchemaShape, error) {
	switch t := t.(type) {
	case basicTy:
		switch t {
		case tyNever:
			return SchemaShape{}, fmt.Errorf("unresolved inferred schema type %q", t)
		case tyInt, tyString, tyBool:
			return SchemaShape{Kind: "scalar", Scalar: string(t)}, nil
		case tyAny, tyNil, tyNode, tyTerminal:
			return SchemaShape{Kind: string(t)}, nil
		}
		return SchemaShape{}, fmt.Errorf("unsupported inferred schema type %q", t)
	case namedTy:
		var ns string
		switch t.kind {
		case 's', 't':
			ns = "declared/"
		case 'c':
			ns = "cst/"
		default:
			return SchemaShape{}, fmt.Errorf("unsupported inferred schema named kind %q", t.kind)
		}
		return SchemaShape{Kind: "ref", Ref: ns + t.name}, nil
	case listTy:
		if elem, ok := t.elem.(basicTy); ok && elem == tyNever {
			// Repetition retains nil elements for value-free expressions.
			return SchemaShape{Kind: "list", Element: &SchemaShape{Kind: "nil"}}, nil
		}
		e, err := schemaInferred(t.elem)
		return SchemaShape{Kind: "list", Element: &e}, err
	case optTy:
		e, err := schemaInferred(t.elem)
		return SchemaShape{Kind: "optional", Element: &e}, err
	case unionTy:
		s := SchemaShape{Kind: "union"}
		for _, a := range t.alts {
			v, err := schemaInferred(a)
			if err != nil {
				return s, err
			}
			s.Alternatives = append(s.Alternatives, v)
		}
		sortSchemaAlternatives(s.Alternatives)
		return s, nil
	case recordTy:
		s := SchemaShape{Kind: "cst", Ref: "cst/Seq", Children: &SchemaShape{Kind: "list", Element: &SchemaShape{Kind: "optional", Element: &SchemaShape{Kind: "node"}}}}
		for name, t := range t.fields {
			v, err := schemaInferred(t)
			if err != nil {
				return s, err
			}
			s.Fields = append(s.Fields, SchemaField{name, "capture", v})
		}
		sort.Slice(s.Fields, func(i, j int) bool { return s.Fields[i].Name < s.Fields[j].Name })
		return s, nil
	}
	return SchemaShape{}, fmt.Errorf("unresolved inferred schema type %T", t)
}
func sortSchemaAlternatives(alts []SchemaShape) {
	sort.Slice(alts, func(i, j int) bool {
		a, _ := json.Marshal(alts[i])
		b, _ := json.Marshal(alts[j])
		return string(a) < string(b)
	})
}
