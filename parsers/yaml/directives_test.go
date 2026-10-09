package yaml_test

import (
	"errors"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

func TestMalformedDirectives(t *testing.T) {
	for _, directive := range []string{
		"%TAG", "%YAML", "%TAG !e!", "%YAML # no version",
		"%YAML 1.foo", "%YAML 1.", "%YAML .2", "%YAML 1.2 extra",
		"%TAG notahandle tag:x", "%TAG !bad_handle! tag:x", "%TAG !e! tag:x extra",
	} {
		t.Run(directive, func(t *testing.T) {
			src := directive + "\n--- x\n"
			if _, err := yaml.ParseAST(src); err == nil {
				t.Fatal("ParseAST accepted malformed directive")
			}
			if _, err := yaml.Parse(src); err == nil {
				t.Error("Parse accepted malformed directive")
			}
			if err := yaml.Recognize(src); err == nil {
				t.Error("Recognize accepted malformed directive")
			}
			if yaml.Valid(src) {
				t.Error("Valid accepted malformed directive")
			}
			if _, err := yaml.Events(src); err == nil {
				t.Error("Events accepted malformed directive")
			}
			if _, err := yaml.Load(src); err == nil {
				t.Error("Load accepted malformed directive")
			}
			if _, err := yaml.LoadAll(src); err == nil {
				t.Error("LoadAll accepted malformed directive")
			}
		})
	}
}

func TestDirectiveNameBoundaries(t *testing.T) {
	for _, src := range []string{
		"%YAMLfoo\n--- x\n", "%TAGx ignored parameter\n--- x\n", "%YAML#name\n--- x\n",
		"%YAML 1.1\n--- x\n", "%YAML 1.3\n--- x\n", "%YAML 0001.02\n--- x\n",
		"%TAG !e! notahandle\n--- !e!kind x\n",
		"%TAG ! !local-\n--- !kind x\n", "%TAG !! tag:example:\n--- !!kind x\n",
		"%TAG !a-9! tag:example:\n--- !a-9!kind x\n",
	} {
		t.Run(src, func(t *testing.T) {
			if _, err := yaml.ParseAST(src); err != nil {
				t.Fatal(err)
			}
			if err := yaml.Recognize(src); err != nil {
				t.Error(err)
			}
			if !yaml.Valid(src) {
				t.Error("Valid rejected directive")
			}
			if _, err := yaml.Events(src); err != nil {
				t.Error(err)
			}
			if _, err := yaml.Load(src); err != nil {
				t.Error(err)
			}
			if _, err := yaml.LoadAll(src); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestMalformedDirectiveAST(t *testing.T) {
	word := func(s string) *yaml.Word { return &yaml.Word{Text: s} }
	for _, tc := range []struct {
		name string
		dir  *yaml.Directive
	}{
		{"nil directive", nil},
		{"nil name", &yaml.Directive{}},
		{"missing YAML version", &yaml.Directive{Name: word("YAML")}},
		{"nil YAML version", &yaml.Directive{Name: word("YAML"), Params: []*yaml.Word{nil}}},
		{"extra YAML version", &yaml.Directive{Name: word("YAML"), Params: []*yaml.Word{word("1.2"), word("1.2")}}},
		{"invalid YAML version", &yaml.Directive{Name: word("YAML"), Params: []*yaml.Word{word("1.foo")}}},
		{"unsupported YAML version", &yaml.Directive{Name: word("YAML"), Params: []*yaml.Word{word("2.0")}}},
		{"missing TAG handle", &yaml.Directive{Name: word("TAG")}},
		{"missing TAG prefix", &yaml.Directive{Name: word("TAG"), Params: []*yaml.Word{word("!e!")}}},
		{"nil TAG handle", &yaml.Directive{Name: word("TAG"), Params: []*yaml.Word{nil, word("tag:x")}}},
		{"nil TAG prefix", &yaml.Directive{Name: word("TAG"), Params: []*yaml.Word{word("!e!"), nil}}},
		{"empty TAG prefix", &yaml.Directive{Name: word("TAG"), Params: []*yaml.Word{word("!e!"), word("")}}},
		{"extra TAG parameter", &yaml.Directive{Name: word("TAG"), Params: []*yaml.Word{word("!e!"), word("tag:x"), word("extra")}}},
		{"invalid TAG handle", &yaml.Directive{Name: word("TAG"), Params: []*yaml.Word{word("notahandle"), word("tag:x")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := yaml.ParseAST("x\n")
			if err != nil {
				t.Fatal(err)
			}
			d := s.Documents[0]
			d.Directives = []*yaml.Directive{tc.dir}
			check := func(err error) {
				t.Helper()
				var se *yaml.SemanticError
				if !errors.As(err, &se) {
					t.Errorf("got %v, want SemanticError", err)
				}
			}
			check(s.Check())
			_, err = s.Events()
			check(err)
			_, err = d.Load()
			check(err)
			_, err = d.ResolveTag(&yaml.Tag{Text: "!!str"})
			check(err)
		})
	}
}
