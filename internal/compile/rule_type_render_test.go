package compile

import (
	"fmt"
	"strings"
	"testing"
)

func TestRuleTypeWithPollParity(t *testing.T) {
	record := RecordTy{fields: map[string]Ty{"z": ListTy{Elem: JoinTy{Alts: []Ty{TyMatch, TyNil}}}, "a": OptTy{Elem: TyString}}}
	cases := []Ty{TyAny, tyNever, TyNil, TyInt, TyBool, TyString, TyNode, TyMatch, ListTy{Elem: TyMatch}, OptTy{Elem: TyMatch}, JoinTy{}, JoinTy{Alts: []Ty{TyMatch, tySeq, TyMissing}}, record, RecordTy{fields: map[string]Ty{}}, ListTy{Elem: record},
		// Value-model types
		TyTop, TyBottom, RuleTy{Name: "expr"}, NodeTy{Value: TyTop}, NodeTy{Value: ListTy{Elem: TyStringNode}}, TupleTy{Elems: []Ty{TyStringNode}},
		NodeTy{Value: TupleTy{Elems: []Ty{OptTy{Elem: TyStringNode}, RuleTy{Name: "x"}}}, Caps: []CapIndex{{Name: "a", Paths: [][]int{{0}, {1, 0}}}, {Name: "b", Side: true}}}}
	for _, typ := range cases {
		p := &Program{Typed: &TypeInfo{shown: map[string]Ty{"main": typ}}}
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
	for _, p := range []*Program{{}, {Typed: &TypeInfo{shown: map[string]Ty{}}}} {
		for _, poll := range []func(){nil, func() {}} {
			if got, ok := p.RuleTypeWithPoll("missing", poll); got != "" || ok {
				t.Fatalf("missing=%q/%v", got, ok)
			}
		}
	}
}

func TestRuleTypeWithPollCancelsWithinCollections(t *testing.T) {
	var union JoinTy
	record := RecordTy{fields: map[string]Ty{}}
	for i := range 1024 {
		union.Alts = append(union.Alts, TyMatch)
		record.fields[fmt.Sprintf("f%d", i)] = TyMatch
	}
	for _, typ := range []Ty{union, record, JoinTy{Alts: []Ty{NamedTy{Name: strings.Repeat("x", 1<<20)}, TyMatch}}} {
		func() {
			marker := &struct{}{}
			calls := 0
			defer func() {
				if got := recover(); got != marker {
					t.Fatalf("%T did not preserve cancellation callback: %v", typ, got)
				}
			}()
			p := &Program{Typed: &TypeInfo{shown: map[string]Ty{"main": typ}}}
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
	var typ Ty = renderProbeTy{done: &done}
	for range 1024 {
		typ = ListTy{Elem: typ}
	}
	marker := &struct{}{}
	returns := 0
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("recursive return did not stop: %v", got)
		}
	}()
	renderRuleType(typ, func() {
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
	renderRuleType(ListTy{Elem: renderProbeTy{done: &done, failure: marker}}, func() {
		if done {
			panic(replacement)
		}
	})
}
