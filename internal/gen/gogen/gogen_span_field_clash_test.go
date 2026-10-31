package gogen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// TestGeneratedTypedFieldsNamedLikeSpanFields checks that a struct field named Start or End does
// not shadow the fields the struct promotes from the embedded Span: the Go field gets a trailing
// underscore, so Start and End keep reading the span.
func TestGeneratedTypedFieldsNamedLikeSpanFields(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedFieldsNamedLikeSpanFields)
}

func testGeneratedTypedFieldsNamedLikeSpanFields(t *testing.T) {
	g, err := parseTest(`type Thing = { Start int; End int }
rule main Thing = a:"a" => Thing{Start: 7, End: 8}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, line := range strings.Split(string(code), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && (strings.HasPrefix(f[0], "Start") || strings.HasPrefix(f[0], "End")) {
			fields = append(fields, f[0])
		}
	}
	for _, want := range []string{"Start_", "End_"} {
		if !slices.Contains(fields, want) {
			t.Errorf("no field %s in the generated Thing; fields starting with Start or End: %v", want, fields)
		}
	}
	for _, bad := range []string{"Start", "End"} {
		if slices.Contains(fields, bad) {
			t.Errorf("a field %s shadows the promoted field of Span", bad)
		}
	}
}
