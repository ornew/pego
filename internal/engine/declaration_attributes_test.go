package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestUnknownDeclarationAttribute(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"@foo\nrule a = \"x\"\n", "1:1: unknown declaration attribute @foo"},
		{"rule a = \"x\"\n\n@foo(1) @bar\nrule b = a\n", "3:1: unknown declaration attribute @foo\n3:9: unknown declaration attribute @bar"},
	} {
		if got := compileError(t, tc.src); got != tc.want {
			t.Errorf("%q\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
	// A declaration attribute is never a # pragma.
	g := mustParse("rule a = \"x\"\n")
	g.Rules()[0].Attrs = []*grammar.Attribute{{Name: "atomic", Pragma: true}}
	if _, err := Compile(g, Options{}); err == nil || err.Error() != "unknown declaration attribute #atomic" {
		t.Errorf("pragma: %v", err)
	}
}
