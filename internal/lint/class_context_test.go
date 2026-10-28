package lint

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestClassRangeContextCancellation(t *testing.T) {
	rs := make([]grammar.CharRange, 4096)
	for i := range rs {
		rs[i] = grammar.CharRange{Lo: rune(256 + (len(rs)-i)*2), Hi: rune(256 + (len(rs)-i)*2)}
	}
	before := append([]grammar.CharRange(nil), rs...)
	cl := &grammar.CharClass{Ranges: rs}
	for _, p := range []struct {
		name string
		at   int
		run  func(*lintControl)
	}{
		{"membership", 1, func(c *lintControl) { classAcceptsControlled(cl, 'a', c) }},
		{"normalization-copy", 1, func(c *lintControl) { normalizeControlled(rs, c) }},
		{"normalization-sort", 17, func(c *lintControl) { normalizeControlled(rs, c) }},
		{"normalization-merge", 1, func(c *lintControl) { mergeRanges(rs, c) }},
		{"comparison", 1, func(c *lintControl) { sameRangesControlled(rs, rs, c) }},
		{"structure-comparison", 1, func(c *lintControl) { same(cl, cl, false, c) }},
		{"prefix", 1, func(c *lintControl) { (&analysis{control: c}).matchPrefix(cl, "a") }},
		{"failure", 1, func(c *lintControl) { (&analysis{control: c}).fails(cl, "a") }},
		{"any-character", 1, func(c *lintControl) { consumesAny(&grammar.CharClass{Negated: true, Ranges: rs}, c) }},
		{"class-lint", 1, func(c *lintControl) { (&linter{control: c}).charClass(cl) }},
	} {
		t.Run(p.name, func(t *testing.T) {
			c := &lintControl{ctx: lintTestContext(t, p.at)}
			defer func() {
				failure := recover()
				stop, ok := failure.(lintCancellation)
				if !ok || !errors.Is(stop.err, context.Canceled) {
					t.Fatalf("range phase did not cancel internally: %v", failure)
				}
				if c.steps != uint32(p.at*256) {
					t.Fatalf("unexpected poll %d", c.steps)
				}
				if !reflect.DeepEqual(rs, before) {
					t.Fatal("range normalization changed source slice")
				}
			}()
			p.run(c)
		})
	}
}

func TestClassRangeContextFacts(t *testing.T) {
	cases := []struct{ in, want []grammar.CharRange }{
		{nil, nil},
		{[]grammar.CharRange{{Lo: 'z', Hi: 'z'}, {Lo: 'a', Hi: 'f'}, {Lo: 'd', Hi: 'j'}, {Lo: 'k', Hi: 'k'}, {Lo: 'a', Hi: 'f'}}, []grammar.CharRange{{Lo: 'a', Hi: 'k'}, {Lo: 'z', Hi: 'z'}}},
		{[]grammar.CharRange{{Lo: 'β', Hi: 'β'}, {Lo: 'a', Hi: 'a'}}, []grammar.CharRange{{Lo: 'a', Hi: 'a'}, {Lo: 'β', Hi: 'β'}}},
	}
	for _, tc := range cases {
		c := &lintControl{ctx: context.Background()}
		before := append([]grammar.CharRange(nil), tc.in...)
		for _, control := range []*lintControl{nil, c} {
			got := normalizeControlled(tc.in, control)
			if !reflect.DeepEqual(got, tc.want) || !sameRangesControlled(got, normalize(tc.in), control) {
				t.Fatalf("normalize=%v want=%v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.in, before) {
				t.Fatal("normalization mutated input")
			}
			if len(got) > 0 {
				got[0] = grammar.CharRange{}
				if !reflect.DeepEqual(tc.in, before) {
					t.Fatal("normalization exposed input storage")
				}
			}
			if sameRangesControlled(tc.want, append(tc.want, grammar.CharRange{}), control) {
				t.Fatal("comparison ignored length")
			}
		}
	}
}

func TestClassRangeRunContextParity(t *testing.T) {
	for _, source := range []string{
		`rule main = [a-fd-j]`,
		`rule main = [zA-z] / "a"`,
		`rule main = [a-f] / [a-cd-f]`,
		`rule main = &[^abβ]* "a"`,
	} {
		g := lintContextGrammar(t, source)
		before, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		want := Run(g, "main", Options{})
		ctx := lintTestContext(t, 0)
		got, err := RunContext(ctx, g, "main", Options{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("range findings changed: %v/%v/%v", got, want, err)
		}
		if len(want) == 0 {
			t.Fatalf("fixture did not exercise findings: %s", source)
		}
		for _, at := range []int{1, ctx.calls - 1} {
			out, err := RunContext(lintTestContext(t, at), g, "main", Options{})
			if out != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("partial findings=%v err=%v", out, err)
			}
		}
		after, err := json.Marshal(g)
		if err != nil || string(before) != string(after) {
			t.Fatal("lint changed source grammar")
		}
	}
}
