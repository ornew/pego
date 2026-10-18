package engine

import (
	"fmt"
	"iter"
)

// QuerySchema is a validated output schema indexed for compiling structural
// queries. A prepared one owns a deep copy of the schema it was made from, so
// later changes to that schema do not affect it, and it is never modified, so
// it is safe for concurrent use.
type QuerySchema struct {
	s     *OutputSchema
	rules map[string]bool
	byID  map[string]*SchemaType
	// byName holds the first declared or CST type of each name. Where a name
	// has several (only possible for a reserved name), repeated is set.
	byName   map[string]*SchemaType
	repeated bool
}

// PrepareQuerySchema validates schema once and copies it, so that queries
// compiled with the result skip validation.
func PrepareQuerySchema(schema *OutputSchema) (*QuerySchema, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query schema: %w", err)
	}
	return indexQuerySchema(copyOutputSchema(schema)), nil
}

// indexQuerySchema indexes a validated schema without copying it.
func indexQuerySchema(s *OutputSchema) *QuerySchema {
	q := &QuerySchema{
		s:      s,
		rules:  make(map[string]bool, len(s.Rules)),
		byID:   make(map[string]*SchemaType, len(s.Types)),
		byName: make(map[string]*SchemaType, len(s.Types)),
	}
	for i := range s.Rules {
		q.rules[s.Rules[i].Name] = true
	}
	for i := range s.Types {
		t := &s.Types[i]
		q.byID[t.ID] = t
		if t.Origin == "inferred" {
			continue
		}
		if q.byName[t.Name] != nil {
			q.repeated = true
		} else {
			q.byName[t.Name] = t
		}
	}
	return q
}

// named yields the declared and CST types named name in schema order.
func (s *QuerySchema) named(name string) iter.Seq[*SchemaType] {
	return func(yield func(*SchemaType) bool) {
		if !s.repeated {
			if t := s.byName[name]; t != nil {
				yield(t)
			}
			return
		}
		for i := range s.s.Types {
			if t := &s.s.Types[i]; t.Name == name && t.Origin != "inferred" && !yield(t) {
				return
			}
		}
	}
}

// CompileStructuralQuery compiles pattern with the schema. A nil QuerySchema
// defers type and field checks to evaluation, as a nil OutputSchema does.
func (s *QuerySchema) CompileStructuralQuery(pattern QueryPattern) (*StructuralQuery, error) {
	p, err := copyQueryPattern(pattern, s, 0)
	if err != nil {
		return nil, err
	}
	if s != nil {
		if err := checkQueryShapes(&p, s); err != nil {
			return nil, err
		}
	}
	return &StructuralQuery{pattern: p}, nil
}

// copyOutputSchema copies a validated schema. Inline shapes may be shared, as
// a DAG, through Element and Children pointers and through slices; the copy
// shares them in the same way, since copying each path separately can take
// exponential work. Slices are shared by their first element and length, so
// shapes holding different overlapping parts of one array get separate copies:
// n such parts of n alternatives copy to about n²/2 shapes. Exported and
// decoded schemas share no partial slices.
func copyOutputSchema(s *OutputSchema) *OutputSchema {
	c := &schemaCopier{
		shapes: map[*SchemaShape]*SchemaShape{},
		alts:   map[schemaSliceKey[SchemaShape]][]SchemaShape{},
		fields: map[schemaSliceKey[SchemaField]][]SchemaField{},
	}
	out := *s
	out.Types = make([]SchemaType, len(s.Types))
	for i, t := range s.Types {
		t.Shape = c.shape(t.Shape)
		out.Types[i] = t
	}
	out.Rules = make([]SchemaRule, len(s.Rules))
	for i, r := range s.Rules {
		r.Result = c.shape(r.Result)
		out.Rules[i] = r
	}
	return &out
}

type schemaSliceKey[T any] struct {
	first *T
	n     int
}

type schemaCopier struct {
	shapes map[*SchemaShape]*SchemaShape
	alts   map[schemaSliceKey[SchemaShape]][]SchemaShape
	fields map[schemaSliceKey[SchemaField]][]SchemaField
}

func (c *schemaCopier) shape(s SchemaShape) SchemaShape {
	s.Element = c.pointer(s.Element)
	s.Children = c.pointer(s.Children)
	if len(s.Alternatives) > 0 {
		k := schemaSliceKey[SchemaShape]{&s.Alternatives[0], len(s.Alternatives)}
		alts, ok := c.alts[k]
		if !ok {
			alts = make([]SchemaShape, len(s.Alternatives))
			for i, a := range s.Alternatives {
				alts[i] = c.shape(a)
			}
			c.alts[k] = alts
		}
		s.Alternatives = alts
	}
	if len(s.Fields) > 0 {
		k := schemaSliceKey[SchemaField]{&s.Fields[0], len(s.Fields)}
		fields, ok := c.fields[k]
		if !ok {
			fields = make([]SchemaField, len(s.Fields))
			for i, f := range s.Fields {
				f.Shape = c.shape(f.Shape)
				fields[i] = f
			}
			c.fields[k] = fields
		}
		s.Fields = fields
	}
	return s
}

func (c *schemaCopier) pointer(p *SchemaShape) *SchemaShape {
	if p == nil {
		return nil
	}
	if q, ok := c.shapes[p]; ok {
		return q
	}
	q := new(SchemaShape)
	*q = c.shape(*p)
	c.shapes[p] = q
	return q
}
