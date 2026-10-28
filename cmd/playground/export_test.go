package main

import (
	"strings"
	"testing"
)

func TestGenerateLanguage(t *testing.T) {
	for _, language := range []string{"", "go", "typescript"} {
		r := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Language: language, Package: "pairs", Types: true, Recognize: true})
		if len(r.Diagnostics) != 0 {
			t.Fatalf("%q: %+v", language, r.Diagnostics)
		}
		markers := []string{"package pairs", "func ParseAST(", "func Recognize("}
		if language == "typescript" {
			markers = []string{"export function parse(", "export function parseAST(", "export function recognize("}
		}
		for _, marker := range markers {
			if !strings.Contains(r.Code, marker) {
				t.Errorf("%q missing %q", language, marker)
			}
		}
	}
}

func TestGenerateLanguageDiagnostics(t *testing.T) {
	for _, req := range []generateRequest{
		{Grammar: pairs, Language: "unsupported"},
		{Grammar: pairs, Language: "typescript", Start: "missing"},
		{Grammar: "rule main = (", Language: "typescript"},
	} {
		r := call[generateResult](t, "generate", req)
		if r.Code != "" || len(r.Diagnostics) == 0 {
			t.Errorf("%+v: %+v", req, r)
		}
	}
	// A Go-only package name must not become a TypeScript module constraint.
	r := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Language: "typescript", Package: "not a Go identifier"})
	if r.Code == "" || len(r.Diagnostics) != 0 {
		t.Errorf("TypeScript package: %+v", r.Diagnostics)
	}
}
