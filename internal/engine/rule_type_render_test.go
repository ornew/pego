package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestRuleTypeWithPollParity(t *testing.T) {
	record := recordTy{fields: map[string]ty{"z": listTy{unionTy{alts: []ty{tyMatch, tyNil}}}, "a": optTy{tyString}}}
	cases := []ty{tyAny, tyNever, tyNil, tyInt, tyBool, tyString, tyNode, tyTerminal, tyMatch, listTy{tyMatch}, optTy{tyMatch}, unionTy{}, unionTy{alts: []ty{tyMatch, tySeq, tyMissing}}, record, recordTy{fields: map[string]ty{}}, listTy{record}}
	for _, typ := range cases {
		p := &Program{typed: &typeInfo{rules: map[string]ty{"main": typ}}}
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
	for _, p := range []*Program{{}, {typed: &typeInfo{rules: map[string]ty{}}}} {
		for _, poll := range []func(){nil, func() {}} {
			if got, ok := p.RuleTypeWithPoll("missing", poll); got != "" || ok {
				t.Fatalf("missing=%q/%v", got, ok)
			}
		}
	}
}

func TestRuleTypeWithPollCancelsWithinCollections(t *testing.T) {
	var union unionTy
	record := recordTy{fields: map[string]ty{}}
	for i := range 1024 {
		union.alts = append(union.alts, tyMatch)
		record.fields[fmt.Sprintf("f%d", i)] = tyMatch
	}
	for _, typ := range []ty{union, record, unionTy{alts: []ty{namedTy{name: strings.Repeat("x", 1<<20)}, tyMatch}}} {
		func() {
			marker := &struct{}{}
			calls := 0
			defer func() {
				if got := recover(); got != marker {
					t.Fatalf("%T did not preserve cancellation callback: %v", typ, got)
				}
			}()
			p := &Program{typed: &typeInfo{rules: map[string]ty{"main": typ}}}
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
	var typ ty = renderProbeTy{done: &done}
	for range 1024 {
		typ = listTy{elem: typ}
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
	renderRuleType(listTy{elem: renderProbeTy{done: &done, failure: marker}}, func() {
		if done {
			panic(replacement)
		}
	})
}
