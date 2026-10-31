package query_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/value"
	"github.com/ornew/zeroparse/query"
)

func TestStructuralQueryRuleNeedsSourceInfo(t *testing.T) {
	const src = `rule main = item ("," item)*
rule item = [a-z]`
	pattern := query.Pattern{Rule: "item", Capture: "item"}
	for _, sourceInfo := range []bool{false, true} {
		var opts []zp.CompileOption
		if sourceInfo {
			opts = append(opts, zp.WithSourceInfo())
		}
		p := mustCompile(t, src, opts...)
		s := mustSchema(t, p)
		if s.SourceInfo != sourceInfo {
			t.Fatalf("schema sourceInfo %v, want %v", s.SourceInfo, sourceInfo)
		}
		_, err := query.Compile(pattern, s)
		if sourceInfo != (err == nil) || !sourceInfo && !strings.Contains(err.Error(), "source information") {
			t.Fatalf("sourceInfo %v: compile error %v", sourceInfo, err)
		}
		q, err := query.Compile(pattern, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
			root, err := p.Parse("a,b,c", zp.WithBackend(backend))
			if err != nil {
				t.Fatal(err)
			}
			got, err := q.Matches(root)
			if sourceInfo && (err != nil || len(got) != 3) || !sourceInfo && (got != nil || !errors.Is(err, query.ErrNoSourceInfo)) {
				t.Fatalf("%s sourceInfo %v: %+v %v", backend, sourceInfo, got, err)
			}
		}
	}
	// Values without nodes cannot record rules, and a rule pattern simply does not match them.
	q, err := query.Compile(query.Pattern{Fields: []query.FieldPattern{{Name: "X", Pattern: query.Pattern{Rule: "item"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := q.Matches(value.Ptr(value.Data{Kind: value.KindOf("S", ""), Fields: value.Fields{{Name: "X", Value: 1}}})); err != nil || len(got) != 0 {
		t.Fatalf("struct without nodes: %+v %v", got, err)
	}
}
