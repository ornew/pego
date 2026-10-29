package engine

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/schema"
)

func TestOutputSchemaCanonicalContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*schema.Output)
		want string
	}{
		{"missing start", func(s *schema.Output) { s.Start = "absent" }, "start rule"},
		{"missing inferred type", func(s *schema.Output) { s.Types = s.Types[:1] }, "unresolved"},
		{"direct declared result", func(s *schema.Output) { s.Rules[0].Result.Ref = "declared/Item" }, "must reference"},
		{"inline result", func(s *schema.Output) { s.Rules[0].Result = schema.Shape{Kind: "nil"} }, "must reference"},
		{"different rule result", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "inferred/other", Name: "other", Origin: "inferred", Shape: schema.Shape{Kind: "nil"}})
			s.Rules[0].Result.Ref = "inferred/other"
		}, "must reference"},
		{"CST namespace", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "cst/Match", Name: "Match", Origin: "cst", Shape: schema.Shape{Kind: "node", Value: &schema.Shape{Kind: "scalar", Scalar: "string"}}})
		}, "identity"},
		{"declared non-struct", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "declared/Alias", Name: "Alias", Origin: "declared", Shape: schema.Shape{Kind: "top"}})
		}, "must be a struct"},
		{"node without value", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "inferred/other", Name: "other", Origin: "inferred", Shape: schema.Shape{Kind: "node"}})
		}, "node value"},
		{"capture without location", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "inferred/other", Name: "other", Origin: "inferred", Shape: schema.Shape{
				Kind: "node", Value: &schema.Shape{Kind: "nil"}, Captures: []schema.Capture{{Name: "x", Shape: schema.Shape{Kind: "nil"}}},
			}})
		}, "no location"},
		{"duplicate capture", func(s *schema.Output) {
			c := schema.Capture{Name: "x", Side: true, Shape: schema.Shape{Kind: "nil"}}
			s.Types = append(s.Types, schema.Type{ID: "inferred/other", Name: "other", Origin: "inferred", Shape: schema.Shape{
				Kind: "node", Value: &schema.Shape{Kind: "nil"}, Captures: []schema.Capture{c, c},
			}})
		}, "duplicate"},
		{"negative capture index", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "inferred/other", Name: "other", Origin: "inferred", Shape: schema.Shape{
				Kind: "node", Value: &schema.Shape{Kind: "tuple"}, Captures: []schema.Capture{{Name: "x", Paths: [][]int{{-1}}, Shape: schema.Shape{Kind: "nil"}}},
			}})
		}, "negative"},
		{"members of another kind", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "inferred/other", Name: "other", Origin: "inferred", Shape: schema.Shape{
				Kind: "tuple", Value: &schema.Shape{Kind: "nil"},
			}})
		}, "unexpected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validInMemoryOutputSchema()
			tc.edit(&s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v; want %q", err, tc.want)
			}
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			got := schema.Output{Format: "sentinel"}
			if err := json.Unmarshal(data, &got); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decode = %v; want %q", err, tc.want)
			}
			if got.Format != "sentinel" {
				t.Fatal("failed decode changed receiver")
			}
		})
	}

	s := validInMemoryOutputSchema()
	for i, kind := range []string{"nil", "top", "error", "tuple", "join", "struct"} {
		name := fmt.Sprintf("empty%d", i)
		shape := schema.Shape{Kind: kind, Fields: []schema.Field{}, Alternatives: []schema.Shape{}, Elements: []schema.Shape{}, Captures: []schema.Capture{}}
		if kind == "struct" {
			s.Types = append(s.Types, schema.Type{ID: "declared/" + name, Name: name, Origin: "declared", Shape: shape})
			continue
		}
		s.Types = append(s.Types, schema.Type{ID: "inferred/" + name, Name: name, Origin: "inferred", Shape: shape})
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("shapes with empty omitted members: %v", err)
	}
}

func TestOutputSchemaJSONExactKeysAndDuplicates(t *testing.T) {
	s := validInMemoryOutputSchema()
	s.Types[0].Shape.Fields = []schema.Field{{Name: "Value", Shape: schema.Shape{Kind: "join", Alternatives: []schema.Shape{
		{Kind: "scalar", Scalar: "string"}, {Kind: "optional", Element: &schema.Shape{Kind: "scalar", Scalar: "bool"}},
	}}}}
	s.Types = append(s.Types, schema.Type{ID: "inferred/pair", Name: "pair", Origin: "inferred", Shape: schema.Shape{
		Kind:     "node",
		Value:    &schema.Shape{Kind: "tuple", Elements: []schema.Shape{{Kind: "scalar", Scalar: "int"}, {Kind: "nil"}}},
		Captures: []schema.Capture{{Name: "x", Paths: [][]int{{0}}, Shape: schema.Shape{Kind: "scalar", Scalar: "int"}}},
	}})
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip schema.Output
	if err := json.Unmarshal(data, &roundTrip); err != nil || !reflect.DeepEqual(s, roundTrip) {
		t.Fatalf("fixture round trip: %v", err)
	}
	for _, spot := range []struct{ name, prefix, key, value string }{
		{"envelope", `"format":"pego-output-schema"`, "format", `"pego-output-schema"`},
		{"type", `"id":"declared/Item"`, "id", `"declared/Item"`},
		{"rule", `"rules":[{"name":"main"`, "name", `"main"`},
		{"shape", `"shape":{"kind":"struct"`, "kind", `"struct"`},
		{"field", `"fields":[{"name":"Value"`, "name", `"Value"`},
		{"alternative", `"alternatives":[{"kind":"scalar"`, "kind", `"scalar"`},
		{"element", `"element":{"kind":"scalar"`, "kind", `"scalar"`},
		{"source info", `"sourceInfo":false`, "sourceInfo", `false`},
		{"node value", `"value":{"kind":"tuple"`, "kind", `"tuple"`},
		{"elements", `"elements":[{"kind":"scalar"`, "kind", `"scalar"`},
		{"capture", `"captures":[{"name":"x"`, "name", `"x"`},
		{"paths", `"paths":[[0]]`, "paths", `[[0]]`},
	} {
		key := `"` + spot.key + `"`
		escaped := fmt.Sprintf(`"\u%04x%s"`, spot.key[0], spot.key[1:])
		for _, mutation := range []struct{ name, replacement, want string }{
			{"case", strings.Replace(spot.prefix, key, `"`+strings.ToUpper(spot.key)+`"`, 1), "unknown field"},
			{"duplicate", strings.Replace(spot.prefix, key+":"+spot.value, key+":"+spot.value+","+key+":"+spot.value, 1), "duplicate"},
			{"escaped duplicate", strings.Replace(spot.prefix, key+":"+spot.value, key+":"+spot.value+","+escaped+":"+spot.value, 1), "duplicate"},
		} {
			t.Run(spot.name+"/"+mutation.name, func(t *testing.T) {
				if !strings.Contains(string(data), spot.prefix) || mutation.replacement == spot.prefix {
					t.Fatal("mutation does not match fixture")
				}
				input := strings.Replace(string(data), spot.prefix, mutation.replacement, 1)
				got := schema.Output{Format: "sentinel"}
				if err := json.Unmarshal([]byte(input), &got); err == nil || !strings.Contains(err.Error(), mutation.want) {
					t.Fatalf("decode = %v; want %s", err, mutation.want)
				}
				if got.Format != "sentinel" {
					t.Fatal("failed decode changed receiver")
				}
			})
		}
	}
}

func TestOutputSchemaJSONMalformedAndDeepInput(t *testing.T) {
	for _, input := range []string{`[]`, `null`, `{`, `{"types":{}}`, `{"types":[1]}`, `{"types":[}`, `{} {}`, `{"types":null,"types":[]}`} {
		got := schema.Output{Format: "sentinel"}
		if err := got.UnmarshalJSON([]byte(input)); err == nil || got.Format != "sentinel" {
			t.Fatalf("direct decode %q: schema=%+v err=%v", input, got, err)
		}
	}
	deep := strings.Repeat(`{"kind":"optional","element":`, 10001) + `{"kind":"nil"}` + strings.Repeat("}", 10001)
	input := `{"types":[{"shape":` + deep + `}]}`
	got := schema.Output{Format: "sentinel"}
	if err := got.UnmarshalJSON([]byte(input)); err == nil || !strings.Contains(err.Error(), "depth") || got.Format != "sentinel" {
		t.Fatalf("deep input: schema=%+v err=%v", got, err)
	}
}

func FuzzOutputSchemaJSONIntake(f *testing.F) {
	seed, err := json.Marshal(validInMemoryOutputSchema())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	for _, input := range []string{`{}`, `null`, `[]`, `{"format":"x","format":"y"}`, `{"Format":"x"}`, `{"format":"x","\u0066ormat":"y"}`} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got := schema.Output{Format: "sentinel"}
		if err := got.UnmarshalJSON(data); err != nil {
			if got.Format != "sentinel" {
				t.Fatal("failed decode changed receiver")
			}
			return
		}
		if !json.Valid(data) {
			t.Fatal("accepted invalid JSON")
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("accepted invalid schema: %v", err)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var again schema.Output
		if err := again.UnmarshalJSON(encoded); err != nil {
			t.Fatalf("round trip: %v", err)
		}
	})
}
