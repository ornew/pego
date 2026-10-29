// Package schema exports the output schema of a checked program as a schema.Output.
package schema

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/schema"
)

// ErrUnavailable reports missing checked grammar metadata: it is compile.ErrSchemaUnavailable,
// which the runtime returns for a program that has no schema.
var ErrUnavailable = compile.ErrSchemaUnavailable

// Export exports the schema of the checked program p for the start rule start: the rule, the rules
// it calls or whose values its values contain, and the struct types they refer to; or every rule
// and type with allRules. engine.Program.SchemaProgram checks the start rule first.
func Export(p *compile.Program, start string, allRules bool) (*schema.Output, error) {
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
	all := map[string]schema.Type{}
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
			all[id] = schema.Type{ID: id, Name: td.DeclName(), Origin: "declared", Shape: shape}
			continue
		}
		shape := schema.Shape{Kind: "struct"}
		for _, f := range compile.DefFields(spec) {
			fieldShape, err := declared(p, f.Type)
			if err != nil {
				return nil, fmt.Errorf("schema type %q field %q: %w", td.DeclName(), f.Name, err)
			}
			shape.Fields = append(shape.Fields, schema.Field{Name: f.Name, Shape: fieldShape})
		}
		sort.Slice(shape.Fields, func(i, j int) bool { return shape.Fields[i].Name < shape.Fields[j].Name })
		all[id] = schema.Type{ID: id, Name: td.DeclName(), Origin: "declared", Shape: shape}
	}
	s := &schema.Output{Format: schema.Format, Version: schema.Version, Start: start, PositionUnitIndependent: true, SourceInfo: p.RecordsSourceInfo, Types: []schema.Type{}, Rules: []schema.Rule{}}
	selected := map[string]bool{}
	noType := map[string]bool{}
	wanted := map[string]bool{}
	var todo []string
	var collect func(schema.Shape)
	collect = func(shape schema.Shape) {
		if shape.Ref != "" && !wanted[shape.Ref] {
			wanted[shape.Ref] = true
			if t, ok := all[shape.Ref]; ok {
				collect(t.Shape)
			} else if name, ok := cutPrefix(shape.Ref, "inferred/"); ok {
				todo = append(todo, name)
			}
		}
		for _, sub := range []*schema.Shape{shape.Element, shape.Value} {
			if sub != nil {
				collect(*sub)
			}
		}
		for _, list := range [][]schema.Shape{shape.Elements, shape.Alternatives} {
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
		all[id] = schema.Type{ID: id, Name: name, Origin: "inferred", Shape: shape}
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
		s.Rules = append(s.Rules, schema.Rule{Name: rd.Name, Origin: origin, Result: schema.Shape{Kind: "ref", Ref: "inferred/" + rd.Name}})
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
func declared(p *compile.Program, t grammar.TypeExpr) (schema.Shape, error) {
	k := &compile.TypeResolver{Prog: p}
	return shapeOf(k.Resolve(t))
}

// shapeOf describes a type of the value model.
func shapeOf(t compile.Ty) (schema.Shape, error) {
	switch t := t.(type) {
	case compile.BasicTy:
		switch t {
		case compile.TyBottom:
			return schema.Shape{Kind: "join"}, nil // no values
		case compile.TyInt, compile.TyString, compile.TyBool:
			return schema.Shape{Kind: "scalar", Scalar: string(t)}, nil
		case compile.TyNil:
			return schema.Shape{Kind: "nil"}, nil
		case compile.TyErr:
			return schema.Shape{Kind: "error"}, nil
		case compile.TyTop, compile.TyAny:
			return schema.Shape{Kind: "top"}, nil
		}
		return schema.Shape{}, fmt.Errorf("unsupported schema type %q", t)
	case compile.NamedTy:
		if t == compile.TyMissing {
			return schema.Shape{Kind: "missing"}, nil
		}
		if t.Kind != 's' {
			v := compile.FromLegacy(t)
			if n, ok := v.(compile.NamedTy); !ok || n.Kind == 's' {
				return shapeOf(v)
			}
			return schema.Shape{}, fmt.Errorf("unresolved schema type %s", t.Name)
		}
		return schema.Shape{Kind: "ref", Ref: "declared/" + t.Name}, nil
	case compile.RuleTy:
		return schema.Shape{Kind: "ref", Ref: "inferred/" + t.Name}, nil
	case compile.NodeTy:
		if t.Name != "" {
			return schema.Shape{Kind: "ref", Ref: "declared/" + t.Name}, nil
		}
		v, err := shapeOf(t.Value)
		if err != nil {
			return schema.Shape{}, err
		}
		s := schema.Shape{Kind: "node", Value: &v}
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
				return schema.Shape{}, fmt.Errorf("capture %s: %w", g.name, err)
			}
			s.Captures = append(s.Captures, schema.Capture{Name: g.name, Paths: g.paths, Side: g.side, Shape: cs})
		}
		return s, nil
	case compile.TupleTy:
		s := schema.Shape{Kind: "tuple"}
		for _, e := range t.Elems {
			es, err := shapeOf(e)
			if err != nil {
				return schema.Shape{}, err
			}
			s.Elements = append(s.Elements, es)
		}
		return s, nil
	case compile.ListTy:
		e, err := shapeOf(t.Elem)
		return schema.Shape{Kind: "list", Element: &e}, err
	case compile.OptTy:
		e, err := shapeOf(t.Elem)
		return schema.Shape{Kind: "optional", Element: &e}, err
	case compile.JoinTy:
		s := schema.Shape{Kind: "join"}
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
	return schema.Shape{}, fmt.Errorf("unresolved schema type %T", t)
}

func sortSchemaAlternatives(alts []schema.Shape) {
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
