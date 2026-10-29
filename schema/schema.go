// Package schema describes the values a grammar can produce, independently of a parse: the output
// schema that pego.Parser.OutputSchema exports, its JSON form and its validation, and Compare,
// which reports how two schemas differ. Package query matches patterns against parse results and
// checks them against a schema.
package schema

import (
	"fmt"
)

// Format and Version identify the output schema format: an Output carries them in its fields of
// the same names.
const (
	Format  = "pego-output-schema"
	Version = 2
)

// Output is an experimental, grammar-level description of possible values.
// It is independent of position units and generated target-language layouts. Validate checks
// its envelope and shape graph, including for a schema built in memory, and decoding it with
// encoding/json performs the same check.
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
	if s.Format != Format || s.Version != Version {
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
