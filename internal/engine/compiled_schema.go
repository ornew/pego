package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The schema section of a compiled grammar without the AST. It is a JSON document, separate from
// the bytecode and the AST, and holds the complete output schema of the grammar (every rule and
// declared type) together with, for each start rule, the names of the rules and types of the
// schema that start rule's export contains. An export of a start rule is therefore the complete
// schema restricted to those names: the entries keep the order of the complete schema, which is
// the order that the export from the checked grammar produces.
//
//	{"schema": <complete schema>,
//	 "starts": [{"start": "main", "rules": ["main", ...], "types": ["inferred/main", ...]}, ...]}
//
// The starts are the start rules of the module in declaration order. The document is written
// compact; the bytes are kept as they were loaded, so that saving the file again reproduces them.

// errSchemaOmitted is the error of a parser whose compiled grammar omits the AST and holds no
// schema section.
var errSchemaOmitted = fmt.Errorf("%w: the compiled grammar omits the AST and has no schema section; save it with the schema (pego compile -no-ast -schema, or pego.WithSchema with pego.WithoutAST)", ErrSchemaUnavailable)

// storedSchema is the schema section of a loaded compiled grammar.
type storedSchema struct {
	raw    []byte // the section as loaded, or as encoded
	all    OutputSchema
	starts []storedSchemaStart
}

type storedSchemaStart struct {
	Start string   `json:"start"`
	Rules []string `json:"rules"`
	Types []string `json:"types"`
}

type storedSchemaDocument struct {
	Schema json.RawMessage     `json:"schema"`
	Starts []storedSchemaStart `json:"starts"`
}

// encodeSchemaSection encodes the output schema of a checked program for the schema section.
func (p *Program) encodeSchemaSection() ([]byte, error) {
	all, err := p.OutputSchemaPrototype(p.DefaultStart(), true)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(all)
	if err != nil {
		return nil, err
	}
	doc := storedSchemaDocument{Schema: raw, Starts: []storedSchemaStart{}}
	for _, start := range p.starts {
		exp, err := p.OutputSchemaPrototype(start, false)
		if err != nil {
			return nil, fmt.Errorf("schema of start rule %s: %w", start, err)
		}
		entry := storedSchemaStart{Start: start, Rules: []string{}, Types: []string{}}
		for _, r := range exp.Rules {
			entry.Rules = append(entry.Rules, r.Name)
		}
		for _, t := range exp.Types {
			entry.Types = append(entry.Types, t.ID)
		}
		doc.Starts = append(doc.Starts, entry)
	}
	return json.Marshal(doc)
}

// decodeSchemaSection decodes and checks a schema section against the start rules and the default
// start rule of its module. The complete schema is validated like any decoded output schema.
func decodeSchemaSection(data []byte, starts []string, defaultStart string) (*storedSchema, error) {
	var doc storedSchemaDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid schema section: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid schema section: trailing data")
	}
	var all OutputSchema
	if err := all.UnmarshalJSON(doc.Schema); err != nil {
		return nil, fmt.Errorf("invalid schema section: %w", err)
	}
	if all.Start != defaultStart {
		return nil, fmt.Errorf("invalid schema section: the schema starts at %q, the default start rule is %q", all.Start, defaultStart)
	}
	if len(doc.Starts) != len(starts) {
		return nil, fmt.Errorf("invalid schema section: %d start rules in the schema for %d start rules", len(doc.Starts), len(starts))
	}
	rules := map[string]bool{}
	for _, r := range all.Rules {
		rules[r.Name] = true
	}
	types := map[string]bool{}
	for _, t := range all.Types {
		types[t.ID] = true
	}
	for i, s := range doc.Starts {
		if s.Start != starts[i] {
			return nil, fmt.Errorf("invalid schema section: start rule %q where %q is expected", s.Start, starts[i])
		}
		for _, name := range s.Rules {
			if !rules[name] {
				return nil, fmt.Errorf("invalid schema section: start rule %s refers to the unknown rule %q", s.Start, name)
			}
		}
		for _, id := range s.Types {
			if !types[id] {
				return nil, fmt.Errorf("invalid schema section: start rule %s refers to the unknown type %q", s.Start, id)
			}
		}
	}
	return &storedSchema{raw: append([]byte(nil), data...), all: all, starts: doc.Starts}, nil
}

// export returns the schema of start, or of every rule and declared type when allRules is set. It
// returns a copy that shares no shapes with the stored schema.
func (s *storedSchema) export(start string, allRules bool) (*OutputSchema, error) {
	if allRules {
		// Like the source path, any rule name is a valid start for the complete schema.
		known := false
		for _, r := range s.all.Rules {
			known = known || r.Name == start
		}
		if !known {
			return nil, fmt.Errorf("unknown schema start rule %q", start)
		}
		out := s.all
		out.Start = start
		return copyOutputSchema(&out), nil
	}
	var entry *storedSchemaStart
	for i := range s.starts {
		if s.starts[i].Start == start {
			entry = &s.starts[i]
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("unknown schema start rule %q", start)
	}
	rules := make(map[string]bool, len(entry.Rules))
	for _, name := range entry.Rules {
		rules[name] = true
	}
	types := make(map[string]bool, len(entry.Types))
	for _, id := range entry.Types {
		types[id] = true
	}
	out := s.all
	out.Start = start
	out.Types = []SchemaType{}
	out.Rules = []SchemaRule{}
	for _, t := range s.all.Types {
		if types[t.ID] {
			out.Types = append(out.Types, t)
		}
	}
	for _, r := range s.all.Rules {
		if rules[r.Name] {
			out.Rules = append(out.Rules, r)
		}
	}
	return copyOutputSchema(&out), nil
}
