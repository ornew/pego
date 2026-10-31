package query

import (
	"fmt"

	"github.com/ornew/zeroparse/schema"
)

// Schema is an output schema validated once for compiling many queries. It owns a deep copy of
// the schema it was prepared from, so later changes to that schema do not affect it. It is
// immutable and safe for concurrent use. Obtain one from Prepare: a nil or zero Schema holds no
// schema and defers type and field checks until evaluation.
type Schema struct {
	s     *schema.Output
	rules map[string]bool
	byID  map[string]*schema.Type
	// byName holds the declared struct types by name.
	byName map[string]*schema.Type
}

// Prepare validates and copies the schema. Compiling a pattern with the result performs the same
// checks as Compile with the schema, but does not validate the schema again.
func Prepare(out *schema.Output) (*Schema, error) {
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query schema: %w", err)
	}
	return indexSchema(copyOutputSchema(out)), nil
}

// indexSchema indexes a validated schema without copying it.
func indexSchema(s *schema.Output) *Schema {
	q := &Schema{
		s:      s,
		rules:  make(map[string]bool, len(s.Rules)),
		byID:   make(map[string]*schema.Type, len(s.Types)),
		byName: make(map[string]*schema.Type, len(s.Types)),
	}
	for i := range s.Rules {
		q.rules[s.Rules[i].Name] = true
	}
	for i := range s.Types {
		t := &s.Types[i]
		q.byID[t.ID] = t
		if t.Origin == "declared" {
			q.byName[t.Name] = t
		}
	}
	return q
}

// Compile validates and copies a pattern, rejecting known impossibilities in the prepared
// schema. A nil or zero Schema defers type and field checks until evaluation, as a nil
// *schema.Output does.
func (s *Schema) Compile(pattern Pattern) (*Query, error) {
	if s != nil && s.s == nil {
		s = nil
	}
	p, err := copyPattern(pattern, s, 0)
	if err != nil {
		return nil, err
	}
	if s != nil {
		if err := checkShapes(&p, s); err != nil {
			return nil, err
		}
	}
	return &Query{pattern: p}, nil
}

// copyOutputSchema copies a validated schema. Inline shapes may be shared, as
// a DAG, through Element and Value pointers and through slices; the copy
// shares them in the same way, since copying each path separately can take
// exponential work. Slices are shared by their first element and length, so
// shapes holding different overlapping parts of one array get separate copies:
// n such parts of n alternatives copy to about n²/2 shapes. Exported and
// decoded schemas share no partial slices.
func copyOutputSchema(s *schema.Output) *schema.Output {
	c := &schemaCopier{
		shapes:   map[*schema.Shape]*schema.Shape{},
		alts:     map[schemaSliceKey[schema.Shape]][]schema.Shape{},
		fields:   map[schemaSliceKey[schema.Field]][]schema.Field{},
		captures: map[schemaSliceKey[schema.Capture]][]schema.Capture{},
	}
	out := *s
	out.Types = make([]schema.Type, len(s.Types))
	for i, t := range s.Types {
		t.Shape = c.shape(t.Shape)
		out.Types[i] = t
	}
	out.Rules = make([]schema.Rule, len(s.Rules))
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
	shapes   map[*schema.Shape]*schema.Shape
	alts     map[schemaSliceKey[schema.Shape]][]schema.Shape
	fields   map[schemaSliceKey[schema.Field]][]schema.Field
	captures map[schemaSliceKey[schema.Capture]][]schema.Capture
}

func (c *schemaCopier) shape(s schema.Shape) schema.Shape {
	s.Element = c.pointer(s.Element)
	s.Value = c.pointer(s.Value)
	s.Alternatives = c.slice(s.Alternatives)
	s.Elements = c.slice(s.Elements)
	if len(s.Captures) > 0 {
		k := schemaSliceKey[schema.Capture]{&s.Captures[0], len(s.Captures)}
		captures, ok := c.captures[k]
		if !ok {
			captures = make([]schema.Capture, len(s.Captures))
			for i, capture := range s.Captures {
				capture.Paths = make([][]int, len(capture.Paths))
				for j, path := range s.Captures[i].Paths {
					capture.Paths[j] = append([]int(nil), path...)
				}
				capture.Shape = c.shape(capture.Shape)
				captures[i] = capture
			}
			c.captures[k] = captures
		}
		s.Captures = captures
	}
	if len(s.Fields) > 0 {
		k := schemaSliceKey[schema.Field]{&s.Fields[0], len(s.Fields)}
		fields, ok := c.fields[k]
		if !ok {
			fields = make([]schema.Field, len(s.Fields))
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

// slice copies a slice of shapes, sharing it where the source shares it.
func (c *schemaCopier) slice(shapes []schema.Shape) []schema.Shape {
	if len(shapes) == 0 {
		return shapes
	}
	k := schemaSliceKey[schema.Shape]{&shapes[0], len(shapes)}
	out, ok := c.alts[k]
	if !ok {
		out = make([]schema.Shape, len(shapes))
		for i, a := range shapes {
			out[i] = c.shape(a)
		}
		c.alts[k] = out
	}
	return out
}

func (c *schemaCopier) pointer(p *schema.Shape) *schema.Shape {
	if p == nil {
		return nil
	}
	if q, ok := c.shapes[p]; ok {
		return q
	}
	q := new(schema.Shape)
	*q = c.shape(*p)
	c.shapes[p] = q
	return q
}
