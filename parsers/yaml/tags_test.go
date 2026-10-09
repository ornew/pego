package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

func TestTagValidity(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"!<!> foo", ""}, {"!<$:?> bar", ""}, {"!<relative> x", ""},
		{"!<1tag:x> x", ""}, {"!<tag:foo[bar]> x", ""}, {"!<tag:x#one#two> x", ""},
		{"!<http://[not-an-ip]/x> x", ""}, {"!<http://host:bad/x> x", ""},
		{"%TAG !e! $:\n--- !e!? x\n", ""},
		{"%TAG !e! notahandle\n--- !e!kind x\n", ""},
		{"%TAG !e! tag:foo[\n--- !e!bar x\n", ""},
		{"!<tag:yaml.org,2002:str> x", "tag:yaml.org,2002:str"},
		{"!<!bar> x", "!bar"}, {"!<![bar]> x", "![bar]"}, {"!<!%21> x", "!%21"},
		{"!<tag:example:a%20b> x", "tag:example:a%20b"},
		{"!<tag:example:%FF> x", "tag:example:%FF"},
		{"!<http://user:pass@host:80/path?x=y#part> x", "http://user:pass@host:80/path?x=y#part"},
		{"!<http://[::1]/x> x", "http://[::1]/x"},
		{"!<http://[v1.a:b]/x> x", "http://[v1.a:b]/x"},
		{"!<custom://%41/x> x", "custom://%41/x"},
		{"!<tag:> x", "tag:"}, {"!local x", "!local"}, {"! x", "!"},
		{"%TAG !e! notahandle\n--- !e!:kind x\n", "notahandle:kind"},
		{"%TAG !e! tag:example:\n--- !e!caf%C3%A9 x\n", "tag:example:café"},
		{"%TAG !e! tag:example:\n--- !e!a%20b%25 x\n", "tag:example:a b%"},
		{"%TAG !e! !local-\n--- !e!kind x\n", "!local-kind"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			s, err := yaml.ParseAST(tc.input)
			if err != nil {
				t.Fatalf("syntax-only ParseAST: %v", err)
			}
			if _, err := yaml.Parse(tc.input); err != nil {
				t.Fatal(err)
			}
			if err := yaml.Recognize(tc.input); err != nil {
				t.Fatal(err)
			}
			valid := tc.want != ""
			check := func(err error) {
				t.Helper()
				var se *yaml.SemanticError
				if valid && err != nil || !valid && !errors.As(err, &se) {
					t.Errorf("error = %v; want valid=%v", err, valid)
				}
			}
			check(s.Check())
			_, err = s.Events()
			check(err)
			d := s.Documents[0]
			_, err = d.Load()
			check(err)
			got, err := d.ResolveTag(yaml.PropertiesOf(d.Root).Tag)
			check(err)
			if valid && got != tc.want {
				t.Errorf("ResolveTag = %q; want %q", got, tc.want)
			}
			if yaml.Valid(tc.input) != valid {
				t.Errorf("Valid did not return %v", valid)
			}
			_, err = yaml.Events(tc.input)
			check(err)
			_, err = yaml.Load(tc.input)
			check(err)
			_, err = yaml.LoadAll(tc.input)
			check(err)
		})
	}
	// A scheme-less prefix is allowed when it is not used for an invalid tag.
	if !yaml.Valid("%TAG !e! notahandle\n--- x\n") {
		t.Fatal("unused scheme-less prefix was rejected")
	}
}

func TestMalformedTagAST(t *testing.T) {
	for _, text := range []string{"", "str", "!<", "!<>", "!<tag:x", "!bad!kind", "!x%", "!x%0", "!x%GG", "!x%FF", "!x y"} {
		t.Run(text, func(t *testing.T) {
			s, err := yaml.ParseAST("x\n")
			if err != nil {
				t.Fatal(err)
			}
			d := s.Documents[0]
			tag := &yaml.Tag{Text: text}
			d.Root.(*yaml.Scalar).Props = &yaml.Properties{Tag: tag}
			for _, f := range []func() error{
				s.Check,
				func() error { _, err := s.Events(); return err },
				func() error { _, err := d.Load(); return err },
				func() error { _, err := d.ResolveTag(tag); return err },
			} {
				var se *yaml.SemanticError
				if err := f(); !errors.As(err, &se) {
					t.Errorf("got %v; want SemanticError", err)
				}
			}
		})
	}
	var se *yaml.SemanticError
	if _, err := (&yaml.Document{}).ResolveTag(nil); !errors.As(err, &se) {
		t.Fatalf("nil Tag error = %v; want SemanticError", err)
	}
}

func BenchmarkTagCheck(b *testing.B) {
	for _, mode := range []string{"none", "core", "local", "verbatim", "expanded"} {
		b.Run(mode, func(b *testing.B) {
			var input strings.Builder
			if mode == "expanded" {
				input.WriteString("%TAG !e! tag:example:\n---\n")
			}
			for range 64 {
				switch mode {
				case "none":
					input.WriteString("- x\n")
				case "core":
					input.WriteString("- !!str x\n")
				case "local":
					input.WriteString("- !local x\n")
				case "verbatim":
					input.WriteString("- !<tag:example:kind> x\n")
				case "expanded":
					input.WriteString("- !e!kind x\n")
				}
			}
			s, err := yaml.ParseAST(input.String())
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := s.Check(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
