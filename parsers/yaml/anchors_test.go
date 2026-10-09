package yaml_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

func TestLoadNestedDuplicateAnchors(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        any
	}{
		{"scalar", "- &a [&a x]\n- *a\n", []any{[]any{"x"}, "x"}},
		{"mapping", "- &a {inner: &a {k: v}}\n- *a\n", []any{map[string]any{"inner": map[string]any{"k": "v"}}, map[string]any{"k": "v"}}},
		{"deeper", "- &a [&a [&a x], *a]\n- *a\n", []any{[]any{[]any{"x"}, "x"}, "x"}},
		{"inside outer", "- &a [&a x, *a]\n- *a\n", []any{[]any{"x", "x"}, "x"}},
		{"null", "- &a [&a null]\n- *a\n", []any{[]any{nil}, nil}},
		{"later scalar", "- &a [&a x]\n- &a y\n- *a\n", []any{[]any{"x"}, "y", "y"}},
		{"distinct", "- &a [&b x]\n- *a\n- *b\n", []any{[]any{"x"}, []any{"x"}, "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := yaml.Load(tc.input)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Load = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"&a [*a]", "&a {self: *a}", "&a [&a [*a]]"} {
		if _, err := yaml.Load(input); err == nil || !strings.Contains(err.Error(), "inside the node it refers to") {
			t.Errorf("self-reference %q: %v", input, err)
		}
	}
}

func TestLoadNestedAnchorSharing(t *testing.T) {
	v, err := yaml.Load("- &a {inner: &a {k: v}}\n- *a\n")
	if err != nil {
		t.Fatal(err)
	}
	items := v.([]any)
	inner := items[0].(map[string]any)["inner"].(map[string]any)
	inner["k"] = "changed"
	if items[1].(map[string]any)["k"] != "changed" {
		t.Fatal("alias does not share the latest nested mapping")
	}
}

func TestLoadSharedAnchorAST(t *testing.T) {
	s, err := yaml.ParseAST("- &a [&a x]\n- *a\n")
	if err != nil {
		t.Fatal(err)
	}
	outer := s.Documents[0].Root.(*yaml.Sequence).Items[0].(*yaml.Sequence)
	inner := outer.Items[0].(*yaml.Scalar)
	// Public ASTs can share an Anchor pointer; binding identity is per visit.
	inner.Props.Anchor = outer.Props.Anchor
	got, err := s.Documents[0].Load()
	if want := []any{[]any{"x"}, "x"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load shared AST = %#v, %v; want %#v", got, err, want)
	}
}

func TestNestedAnchorEvents(t *testing.T) {
	got, err := yaml.Events("- &a [&a x]\n- *a\n")
	want := "+STR\n+DOC\n+SEQ\n+SEQ [] &a\n=VAL &a :x\n-SEQ\n=ALI *a\n-SEQ\n-DOC\n-STR\n"
	if err != nil || got != want {
		t.Fatalf("Events = %q, %v; want %q", got, err, want)
	}
}

// Parse once to isolate composition; the baseline can run even when its nested
// duplicate-anchor result is incorrect, without a false equivalent-output claim.
func BenchmarkAnchorComposition(b *testing.B) {
	for _, mode := range []string{"none", "unique", "nested duplicate", "nested distinct"} {
		b.Run(mode, func(b *testing.B) {
			var input strings.Builder
			for i := range 64 {
				switch mode {
				case "none":
					input.WriteString("- [x]\n")
				case "unique":
					fmt.Fprintf(&input, "- &a%d [x]\n- *a%d\n", i, i)
				case "nested duplicate":
					input.WriteString("- &a [&a x]\n- *a\n")
				case "nested distinct":
					input.WriteString("- &a [&b x]\n- *a\n- *b\n")
				}
			}
			s, err := yaml.ParseAST(input.String())
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Documents[0].Load(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
