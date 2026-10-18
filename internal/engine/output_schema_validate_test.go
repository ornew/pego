package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestOutputSchemaDecodeRejectsInvalidGraph(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{
			name: "duplicate type identity",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct"}},{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct"}}]`,
			want: "duplicate",
		},
		{
			name: "unresolved reference",
			body: `"rules":[{"name":"main","origin":"inferred","result":{"kind":"ref","ref":"declared/Missing"}}]`,
			want: "unresolved",
		},
		{
			name: "unknown shape kind",
			body: `"types":[{"id":"inferred/item","name":"item","origin":"inferred","shape":{"kind":"mystery"}}]`,
			want: "unknown",
		},
		{
			name: "unknown scalar",
			body: `"types":[{"id":"inferred/item","name":"item","origin":"inferred","shape":{"kind":"scalar","scalar":"float"}}]`,
			want: "unknown",
		},
		{
			name: "declared type that is not a struct",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"scalar","scalar":"int"}}]`,
			want: "must be a struct",
		},
		{
			name: "invalid type namespace",
			body: `"types":[{"id":"other/Item","name":"Item","origin":"declared","shape":{"kind":"struct"}}]`,
			want: "identity",
		},
		{
			name: "duplicate struct field",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Value","shape":{"kind":"scalar","scalar":"string"}},{"name":"Value","shape":{"kind":"scalar","scalar":"int"}}]}}]`,
			want: "duplicate",
		},
		{
			name: "struct field origin",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Value","origin":"declared","shape":{"kind":"scalar","scalar":"string"}}]}}]`,
			want: "unknown field",
		},
		{
			name: "capture without location",
			body: `"types":[{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"node","value":{"kind":"tuple"},"captures":[{"name":"value","shape":{"kind":"any"}}]}}]`,
			want: "no location",
		},
		{
			name: "duplicate capture",
			body: `"types":[{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"node","value":{"kind":"tuple","elements":[{"kind":"any"}]},"captures":[{"name":"value","paths":[[0]],"shape":{"kind":"scalar","scalar":"string"}},{"name":"value","side":true,"shape":{"kind":"any"}}]}}]`,
			want: "duplicate",
		},
		{
			name: "scalar carrying a node reference",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct"}},{"id":"inferred/text","name":"text","origin":"inferred","shape":{"kind":"scalar","scalar":"string","ref":"declared/Item"}}]`,
			want: "unexpected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.body
			if !strings.Contains(body, `"types":`) {
				body += `,"types":[]`
			}
			if !strings.Contains(body, `"rules":`) {
				body += `,"rules":[]`
			}
			data := []byte(`{"format":"pego-output-schema","version":2,"start":"main","positionUnitIndependent":true,` + body + `}`)
			original := OutputSchema{Format: "sentinel"}
			got := original
			if err := json.Unmarshal(data, &got); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decode error = %v, want %q", err, tt.want)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("failed decode changed receiver: %+v", got)
			}
		})
	}
}

func TestOutputSchemaDecodeRejectsUnknownFieldsAndInvalidEnvelope(t *testing.T) {
	schema := validInMemoryOutputSchema()
	schema.Types[0].Shape.Fields = []SchemaField{{Name: "Value", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}}}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(encoded)
	typesJSON, _ := json.Marshal(schema.Types)
	rulesJSON, _ := json.Marshal(schema.Rules)
	var accepted OutputSchema
	if err := json.Unmarshal([]byte(valid), &accepted); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}
	tests := []struct {
		name, old, replacement, want string
	}{
		{"unknown envelope field", `"start":"main",`, `"start":"main","unexpected":true,`, "unknown field"},
		{"unknown type field", `"id":"declared/Item",`, `"id":"declared/Item","unexpected":true,`, "unknown field"},
		{"unknown struct shape field", `"kind":"struct","fields"`, `"kind":"struct","unexpected":true,"fields"`, "unknown field"},
		{"unknown field entry", `"name":"Value","shape"`, `"name":"Value","unexpected":true,"shape"`, "unknown field"},
		{"unknown scalar shape field", `"scalar":"string"`, `"scalar":"string","unexpected":true`, "unknown field"},
		{"unknown nested element field", `"kind":"scalar","scalar":"string"`, `"kind":"list","element":{"kind":"scalar","scalar":"string","unexpected":true}`, "unknown field"},
		{"unknown rule field", `"rules":[{"name":"main","origin"`, `"rules":[{"name":"main","unexpected":true,"origin"`, "unknown field"},
		{"missing start", `"start":"main",`, ``, "envelope"},
		{"empty start", `"start":"main",`, `"start":"",`, "envelope"},
		{"missing position unit flag", `"positionUnitIndependent":true,`, ``, "envelope"},
		{"false position unit flag", `"positionUnitIndependent":true,`, `"positionUnitIndependent":false,`, "envelope"},
		{"missing types", `"types":` + string(typesJSON) + `,`, ``, "envelope"},
		{"missing rules", `,"rules":` + string(rulesJSON), ``, "envelope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(valid, tt.old) {
				t.Fatalf("fixture does not contain %q", tt.old)
			}
			data := []byte(strings.Replace(valid, tt.old, tt.replacement, 1))
			got := OutputSchema{Format: "sentinel"}
			original := got
			if err := json.Unmarshal(data, &got); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decode error = %v, want %q", err, tt.want)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("failed decode changed receiver: %+v", got)
			}
		})
	}
}

func TestOutputSchemaDecodeAcceptsNodeCaptureShape(t *testing.T) {
	data := []byte(`{"format":"pego-output-schema","version":2,"start":"main","positionUnitIndependent":true,"sourceInfo":true,"types":[{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"node","value":{"kind":"tuple","elements":[{"kind":"node","value":{"kind":"scalar","scalar":"string"}},{"kind":"list","element":{"kind":"any"}}]},"captures":[{"name":"value","paths":[[0],[1,2]],"shape":{"kind":"node","value":{"kind":"scalar","scalar":"string"}}},{"name":"look","side":true,"shape":{"kind":"error"}}]}}],"rules":[{"name":"main","origin":"inferred","result":{"kind":"ref","ref":"inferred/main"}}]}`)
	var got OutputSchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	shape := got.Types[0].Shape
	if !got.SourceInfo || len(shape.Captures) != 2 || !reflect.DeepEqual(shape.Captures[0].Paths, [][]int{{0}, {1, 2}}) || !shape.Captures[1].Side || len(shape.Value.Elements) != 2 {
		t.Fatalf("node capture shape: %+v", shape)
	}
}

func TestOutputSchemaDecodeAcceptsNamedRecursiveReference(t *testing.T) {
	data := []byte(`{"format":"pego-output-schema","version":2,"start":"main","positionUnitIndependent":true,"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Next","shape":{"kind":"optional","element":{"kind":"ref","ref":"declared/Item"}}}]}},{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"ref","ref":"declared/Item"}}],"rules":[{"name":"main","origin":"declared","result":{"kind":"ref","ref":"inferred/main"}}]}`)
	var got OutputSchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Types) != 2 || len(got.Rules) != 1 {
		t.Fatalf("recursive schema: %+v", got)
	}
}

func validInMemoryOutputSchema() OutputSchema {
	return OutputSchema{
		Format: "pego-output-schema", Version: 2, Start: "main", PositionUnitIndependent: true,
		Types: []SchemaType{
			{ID: "declared/Item", Name: "Item", Origin: "declared", Shape: SchemaShape{Kind: "struct"}},
			{ID: "inferred/main", Name: "main", Origin: "inferred", Shape: SchemaShape{Kind: "ref", Ref: "declared/Item"}},
		},
		Rules: []SchemaRule{{Name: "main", Origin: "declared", Result: SchemaShape{Kind: "ref", Ref: "inferred/main"}}},
	}
}

func TestOutputSchemaValidateInMemory(t *testing.T) {
	if err := (*OutputSchema)(nil).Validate(); err == nil || !strings.Contains(err.Error(), "nil output schema") {
		t.Fatalf("nil schema error = %v", err)
	}
	if err := new(OutputSchema).Validate(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("zero schema error = %v", err)
	}
	tests := []struct {
		name string
		edit func(*OutputSchema)
		want string
	}{
		{"missing envelope flag", func(s *OutputSchema) { s.PositionUnitIndependent = false }, "envelope"},
		{"missing arrays", func(s *OutputSchema) { s.Types = nil }, "envelope"},
		{"duplicate type", func(s *OutputSchema) { s.Types = append(s.Types, s.Types[0]) }, "duplicate"},
		{"unresolved reference", func(s *OutputSchema) { s.Types[1].Shape.Ref = "declared/Missing" }, "unresolved"},
		{"inline cycle", func(s *OutputSchema) {
			shape := &SchemaShape{Kind: "optional"}
			shape.Element = shape
			s.Types[1].Shape = *shape
		}, "cyclic inline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validInMemoryOutputSchema()
			tt.edit(&s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validation error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOutputSchemaValidateAllowsNamedRecursionAndSharedInlineShape(t *testing.T) {
	s := validInMemoryOutputSchema()
	shared := &SchemaShape{Kind: "ref", Ref: "declared/Item"}
	s.Types[0].Shape = SchemaShape{Kind: "struct", Fields: []SchemaField{
		{Name: "Left", Shape: SchemaShape{Kind: "optional", Element: shared}},
		{Name: "Right", Shape: SchemaShape{Kind: "optional", Element: shared}},
	}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOutputSchemaValidateLayeredSharedDAG(t *testing.T) {
	s := validInMemoryOutputSchema()
	shared := &SchemaShape{Kind: "scalar", Scalar: "string"}
	for range 28 {
		shared = &SchemaShape{Kind: "union", Alternatives: []SchemaShape{
			{Kind: "list", Element: shared},
			{Kind: "optional", Element: shared},
		}}
	}
	s.Types[1].Shape = *shared
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
