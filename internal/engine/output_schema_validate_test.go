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
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"mystery"}}]`,
			want: "unknown",
		},
		{
			name: "unknown scalar",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"scalar","scalar":"float"}}]`,
			want: "unknown",
		},
		{
			name: "invalid type namespace",
			body: `"types":[{"id":"other/Item","name":"Item","origin":"declared","shape":{"kind":"struct"}}]`,
			want: "identity",
		},
		{
			name: "duplicate struct field",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Value","origin":"declared","shape":{"kind":"scalar","scalar":"string"}},{"name":"Value","origin":"declared","shape":{"kind":"scalar","scalar":"int"}}]}}]`,
			want: "duplicate",
		},
		{
			name: "wrong struct field origin",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Value","origin":"capture","shape":{"kind":"scalar","scalar":"string"}}]}}]`,
			want: "field origin",
		},
		{
			name: "wrong capture field origin",
			body: `"types":[{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"cst","fields":[{"name":"value","origin":"declared","shape":{"kind":"any"}}]}}]`,
			want: "field origin",
		},
		{
			name: "duplicate capture field",
			body: `"types":[{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"cst","fields":[{"name":"value","origin":"capture","shape":{"kind":"scalar","scalar":"string"}},{"name":"value","origin":"capture","shape":{"kind":"any"}}]}}]`,
			want: "duplicate",
		},
		{
			name: "scalar carrying a node reference",
			body: `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"terminal"}},{"id":"declared/Text","name":"Text","origin":"declared","shape":{"kind":"scalar","scalar":"string","ref":"declared/Item"}}]`,
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
			data := []byte(`{"format":"pego-output-schema","version":1,"start":"main","positionUnitIndependent":true,` + body + `}`)
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
	const valid = `{"format":"pego-output-schema","version":1,"start":"main","positionUnitIndependent":true,"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Value","origin":"declared","shape":{"kind":"scalar","scalar":"string"}}]}}],"rules":[{"name":"main","origin":"inferred","result":{"kind":"ref","ref":"declared/Item"}}]}`
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
		{"unknown field entry", `"name":"Value","origin"`, `"name":"Value","unexpected":true,"origin"`, "unknown field"},
		{"unknown scalar shape field", `"scalar":"string"`, `"scalar":"string","unexpected":true`, "unknown field"},
		{"unknown nested element field", `"kind":"scalar","scalar":"string"`, `"kind":"list","element":{"kind":"scalar","scalar":"string","unexpected":true}`, "unknown field"},
		{"unknown rule field", `"name":"main","origin"`, `"name":"main","unexpected":true,"origin"`, "unknown field"},
		{"missing start", `"start":"main",`, ``, "envelope"},
		{"empty start", `"start":"main",`, `"start":"",`, "envelope"},
		{"missing position unit flag", `"positionUnitIndependent":true,`, ``, "envelope"},
		{"false position unit flag", `"positionUnitIndependent":true,`, `"positionUnitIndependent":false,`, "envelope"},
		{"missing types", `"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Value","origin":"declared","shape":{"kind":"scalar","scalar":"string"}}]}}],`, ``, "envelope"},
		{"missing rules", `,"rules":[{"name":"main","origin":"inferred","result":{"kind":"ref","ref":"declared/Item"}}]`, ``, "envelope"},
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

func TestOutputSchemaDecodeAcceptsCSTCaptureShape(t *testing.T) {
	data := []byte(`{"format":"pego-output-schema","version":1,"start":"main","positionUnitIndependent":true,"types":[{"id":"cst/Seq","name":"Seq","origin":"cst","shape":{"kind":"cst","children":{"kind":"list","element":{"kind":"optional","element":{"kind":"node"}}}}},{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"cst","ref":"cst/Seq","children":{"kind":"list","element":{"kind":"optional","element":{"kind":"node"}}},"fields":[{"name":"value","origin":"capture","shape":{"kind":"scalar","scalar":"string"}}]}}],"rules":[{"name":"main","origin":"inferred","result":{"kind":"ref","ref":"inferred/main"}}]}`)
	var got OutputSchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Types[1].Shape.Ref != "cst/Seq" || len(got.Types[1].Shape.Fields) != 1 {
		t.Fatalf("CST capture shape: %+v", got.Types[1].Shape)
	}
}

func TestOutputSchemaDecodeAcceptsNamedRecursiveReference(t *testing.T) {
	data := []byte(`{"format":"pego-output-schema","version":1,"start":"main","positionUnitIndependent":true,"types":[{"id":"declared/Item","name":"Item","origin":"declared","shape":{"kind":"struct","fields":[{"name":"Next","origin":"declared","shape":{"kind":"optional","element":{"kind":"ref","ref":"declared/Item"}}}]}},{"id":"inferred/main","name":"main","origin":"inferred","shape":{"kind":"ref","ref":"declared/Item"}}],"rules":[{"name":"main","origin":"declared","result":{"kind":"ref","ref":"inferred/main"}}]}`)
	var got OutputSchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Types) != 2 || len(got.Rules) != 1 {
		t.Fatalf("recursive schema: %+v", got)
	}
}
