package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	ischema "github.com/ornew/pego/internal/schema"
	"github.com/ornew/pego/internal/value"
)

func TestStructuralQueryRuleNeedsSourceInfo(t *testing.T) {
	g, err := parseTest(`rule main = item ("," item)*
rule item = [a-z]`)
	if err != nil {
		t.Fatal(err)
	}
	pattern := ischema.QueryPattern{Rule: "item", Capture: "item"}
	for _, sourceInfo := range []bool{false, true} {
		p, err := Compile(g, compile.Options{SourceInfo: sourceInfo})
		if err != nil {
			t.Fatal(err)
		}
		s, err := outputSchemaPrototype(p, "main", false)
		if err != nil {
			t.Fatal(err)
		}
		if s.SourceInfo != sourceInfo {
			t.Fatalf("schema sourceInfo %v, want %v", s.SourceInfo, sourceInfo)
		}
		_, err = ischema.CompileStructuralQuery(pattern, s)
		if sourceInfo != (err == nil) || !sourceInfo && !strings.Contains(err.Error(), "source information") {
			t.Fatalf("sourceInfo %v: compile error %v", sourceInfo, err)
		}
		q, err := ischema.CompileStructuralQuery(pattern, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			root, err := p.ParseWith("main", "a,b,c", ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			got, err := q.Matches(root, ischema.QueryOptions{})
			if sourceInfo && (err != nil || len(got) != 3) || !sourceInfo && (got != nil || !errors.Is(err, ischema.ErrQueryNoSourceInfo)) {
				t.Fatalf("%s sourceInfo %v: %+v %v", backend, sourceInfo, got, err)
			}
		}
	}
	// Values without nodes cannot record rules, and a rule pattern simply does not match them.
	q, err := ischema.CompileStructuralQuery(ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{{Name: "X", Pattern: ischema.QueryPattern{Rule: "item"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := q.Matches(value.Ptr(value.Data{Kind: value.KindOf("S", ""), Fields: value.Fields{{Name: "X", Value: 1}}}), ischema.QueryOptions{}); err != nil || len(got) != 0 {
		t.Fatalf("struct without nodes: %+v %v", got, err)
	}
}
