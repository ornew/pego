package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

func TestRuleTypeWithPollParity(t *testing.T) {
	record := compile.RecordTy{Fields: map[string]compile.Ty{"z": compile.ListTy{Elem: compile.JoinTy{Alts: []compile.Ty{compile.TyMatch, compile.TyNil}}}, "a": compile.OptTy{Elem: compile.TyString}}}
	cases := []compile.Ty{compile.TyAny, compile.TyNever, compile.TyNil, compile.TyInt, compile.TyBool, compile.TyString, compile.TyNode, compile.TyMatch, compile.ListTy{Elem: compile.TyMatch}, compile.OptTy{Elem: compile.TyMatch}, compile.JoinTy{}, compile.JoinTy{Alts: []compile.Ty{compile.TyMatch, compile.TySeq, compile.TyMissing}}, record, compile.RecordTy{Fields: map[string]compile.Ty{}}, compile.ListTy{Elem: record},
		// Value-model types
		compile.TyTop, compile.TyBottom, compile.RuleTy{Name: "expr"}, compile.NodeTy{Value: compile.TyTop}, compile.NodeTy{Value: compile.ListTy{Elem: compile.TyStringNode}}, compile.TupleTy{Elems: []compile.Ty{compile.TyStringNode}},
		compile.NodeTy{Value: compile.TupleTy{Elems: []compile.Ty{compile.OptTy{Elem: compile.TyStringNode}, compile.RuleTy{Name: "x"}}}, Caps: []compile.CapIndex{{Name: "a", Paths: [][]int{{0}, {1, 0}}}, {Name: "b", Side: true}}}}
	for _, typ := range cases {
		p := &Program{staticProgram: &compile.Program{Typed: &compile.TypeInfo{Shown: map[string]compile.Ty{"main": typ}}}}
		want, known := p.RuleType("main")
		if !known {
			t.Fatal("missing test rule")
		}
		calls := 0
		for _, poll := range []func(){nil, func() { calls++ }} {
			got, ok := p.RuleTypeWithPoll("main", poll)
			if !ok || got != want {
				t.Fatalf("%T: %q/%v want %q", typ, got, ok, want)
			}
		}
		if calls < 2 {
			t.Fatal("type traversal did not poll")
		}
		if typ.String() != want {
			t.Fatal("type traversal changed its input")
		}
	}
	for _, p := range []*Program{{staticProgram: &compile.Program{}}, {staticProgram: &compile.Program{Typed: &compile.TypeInfo{Shown: map[string]compile.Ty{}}}}} {
		for _, poll := range []func(){nil, func() {}} {
			if got, ok := p.RuleTypeWithPoll("missing", poll); got != "" || ok {
				t.Fatalf("missing=%q/%v", got, ok)
			}
		}
	}
}

func TestRuleTypeWithPollCancelsWithinCollections(t *testing.T) {
	var union compile.JoinTy
	record := compile.RecordTy{Fields: map[string]compile.Ty{}}
	for i := range 1024 {
		union.Alts = append(union.Alts, compile.TyMatch)
		record.Fields[fmt.Sprintf("f%d", i)] = compile.TyMatch
	}
	for _, typ := range []compile.Ty{union, record, compile.JoinTy{Alts: []compile.Ty{compile.NamedTy{Name: strings.Repeat("x", 1<<20)}, compile.TyMatch}}} {
		func() {
			marker := &struct{}{}
			calls := 0
			defer func() {
				if got := recover(); got != marker {
					t.Fatalf("%T did not preserve cancellation callback: %v", typ, got)
				}
			}()
			p := &Program{staticProgram: &compile.Program{Typed: &compile.TypeInfo{Shown: map[string]compile.Ty{"main": typ}}}}
			p.RuleTypeWithPoll("main", func() {
				calls++
				if calls == 32 {
					panic(marker)
				}
			})
			t.Fatal("large type completed without cancellation callback")
		}()
	}
}

type renderProbeTy struct {
	done    *bool
	failure any
}

func (t renderProbeTy) String() string {
	*t.done = true
	if t.failure != nil {
		panic(t.failure)
	}
	return "T"
}
func TestRuleTypeWithPollRecursiveReturn(t *testing.T) {
	done := false
	var typ compile.Ty = renderProbeTy{done: &done}
	for range 1024 {
		typ = compile.ListTy{Elem: typ}
	}
	marker := &struct{}{}
	returns := 0
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("recursive return did not stop: %v", got)
		}
	}()
	compile.RenderRuleType(typ, func() {
		if done {
			returns++
			if returns == 256 {
				panic(marker)
			}
		}
	})
	t.Fatal("recursive returns completed without polling")
}
func TestRuleTypeWithPollPreservesForeignPanic(t *testing.T) {
	done := false
	marker := &struct{}{}
	replacement := &struct{ n int }{1}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("foreign panic changed: %v", got)
		}
	}()
	compile.RenderRuleType(compile.ListTy{Elem: renderProbeTy{done: &done, failure: marker}}, func() {
		if done {
			panic(replacement)
		}
	})
}
