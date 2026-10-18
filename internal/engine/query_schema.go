package engine

import "fmt"

// QuerySchema is a validated output schema indexed for compiling structural
// queries. A prepared one owns a deep copy of the schema it was made from, so
// later changes to that schema do not affect it, and it is never modified, so
// it is safe for concurrent use.
type QuerySchema struct {
	s     *OutputSchema
	rules map[string]bool
	byID  map[string]*SchemaType
	// byName holds the declared struct types by name.
	byName map[string]*SchemaType
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
		if t.Origin == "declared" {
			q.byName[t.Name] = t
		}
	}
	return q
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
// a DAG, through Element and Value pointers and through slices; the copy
// shares them in the same way, since copying each path separately can take
// exponential work. Slices are shared by their first element and length, so
// shapes holding different overlapping parts of one array get separate copies:
// n such parts of n alternatives copy to about n²/2 shapes. Exported and
// decoded schemas share no partial slices.
func copyOutputSchema(s *OutputSchema) *OutputSchema {
	c := &schemaCopier{
		shapes:   map[*SchemaShape]*SchemaShape{},
		alts:     map[schemaSliceKey[SchemaShape]][]SchemaShape{},
		fields:   map[schemaSliceKey[SchemaField]][]SchemaField{},
		captures: map[schemaSliceKey[SchemaCapture]][]SchemaCapture{},
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
	shapes   map[*SchemaShape]*SchemaShape
	alts     map[schemaSliceKey[SchemaShape]][]SchemaShape
	fields   map[schemaSliceKey[SchemaField]][]SchemaField
	captures map[schemaSliceKey[SchemaCapture]][]SchemaCapture
}

func (c *schemaCopier) shape(s SchemaShape) SchemaShape {
	s.Element = c.pointer(s.Element)
	s.Value = c.pointer(s.Value)
	s.Alternatives = c.slice(s.Alternatives)
	s.Elements = c.slice(s.Elements)
	if len(s.Captures) > 0 {
		k := schemaSliceKey[SchemaCapture]{&s.Captures[0], len(s.Captures)}
		captures, ok := c.captures[k]
		if !ok {
			captures = make([]SchemaCapture, len(s.Captures))
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

// slice copies a slice of shapes, sharing it where the source shares it.
func (c *schemaCopier) slice(shapes []SchemaShape) []SchemaShape {
	if len(shapes) == 0 {
		return shapes
	}
	k := schemaSliceKey[SchemaShape]{&shapes[0], len(shapes)}
	out, ok := c.alts[k]
	if !ok {
		out = make([]SchemaShape, len(shapes))
		for i, a := range shapes {
			out[i] = c.shape(a)
		}
		c.alts[k] = out
	}
	return out
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
