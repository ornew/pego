package lint_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/lint"
)

func TestRun(t *testing.T) {
	g, err := pego.ParseGrammar("@start\nrule main = \"a\" / \"ab\"\nrule unused = \"x\"")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := (lint.Config{}).Run(context.Background(), g, "main")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fs {
		got = append(got, f.String())
	}
	want := []string{
		"2:19: error: alternative 2 (`\"ab\"`) can never match: alternative 1 (`\"a\"` at 2:13) matches first wherever it could [shadowed-alternative]",
		"3:1: warning: rule unused is never used: main does not call it, directly or indirectly [unreachable-rule]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if fs[0].Severity != lint.Error || fs[0].Rule != "main" || fs[0].Fix != "move it before alternative 1" {
		t.Errorf("finding %+v", fs[0])
	}

	fs, err = (lint.Config{Disable: []string{"unreachable-rule"}}).Run(context.Background(), g, "main")
	if err != nil || len(fs) != 1 || fs[0].Check != "shadowed-alternative" {
		t.Errorf("disabled: %v %v", fs, err)
	}
	// Every start rule is used, whichever one is linted from.
	several, err := pego.ParseGrammar("@start(default)\nrule main = \"a\"\n@start\nrule other = \"x\"\nrule unused = \"y\"")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{"", "main", "other"} {
		fs, err = (lint.Config{}).Run(context.Background(), several, start)
		if err != nil || len(fs) != 1 || fs[0].Rule != "unused" || !strings.Contains(fs[0].Message, "no start rule calls it") {
			t.Errorf("several start rules, start %q: %v %v", start, fs, err)
		}
	}

	if _, err := (lint.Config{Disable: []string{"no-such-check"}}).Run(context.Background(), g, "main"); err == nil || !strings.Contains(err.Error(), `unknown check "no-such-check"`) {
		t.Errorf("unknown check: %v", err)
	}
	if _, err := (lint.Config{}).Run(context.Background(), g, "start"); err == nil || !strings.Contains(err.Error(), "rule start is not defined") {
		t.Errorf("unknown start: %v", err)
	}
	// An empty start selects the default start rule, and only start rules can be selected.
	if fs, err := (lint.Config{Disable: []string{"shadowed-alternative"}}).Run(context.Background(), g, ""); err != nil || len(fs) != 1 || fs[0].Rule != "unused" || !strings.Contains(fs[0].Message, "main does not call it") {
		t.Errorf("default start: %v %v", fs, err)
	}
	inner, err := pego.ParseGrammar("@start\nrule main = inner\nrule inner = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (lint.Config{}).Run(context.Background(), inner, "inner"); err == nil || !strings.Contains(err.Error(), "rule inner is not a start rule") {
		t.Errorf("not a start rule: %v", err)
	}
	bad, err := pego.ParseGrammar("@start\nrule main = undefined")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (lint.Config{}).Run(context.Background(), bad, "main"); err == nil || !strings.Contains(err.Error(), "undefined rule undefined") {
		t.Errorf("invalid grammar: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if fs, err := (lint.Config{}).Run(ctx, g, "main"); !errors.Is(err, context.Canceled) || fs != nil {
		t.Errorf("cancelled: %v %v", fs, err)
	}
	for _, c := range lint.Checks() {
		if c.Name == "" || c.Summary == "" || c.Severity < lint.Hint || c.Severity > lint.Error {
			t.Errorf("check %+v", c)
		}
	}
}

func TestRunUnchecked(t *testing.T) {
	g, err := pego.ParseGrammar("@start\nrule main = \"a\" / \"ab\"\nrule unused = \"x\"")
	if err != nil {
		t.Fatal(err)
	}
	checked, err := (lint.Config{}).Run(context.Background(), g, "main")
	if err != nil {
		t.Fatal(err)
	}
	unchecked, err := (lint.Config{}).RunUnchecked(context.Background(), g, "main")
	if err != nil || !slices.Equal(unchecked, checked) {
		t.Errorf("RunUnchecked = %v, %v; Run = %v", unchecked, err, checked)
	}
	disabled, err := (lint.Config{Disable: []string{lint.CheckShadowedAlt}}).RunUnchecked(context.Background(), g, "main")
	if err != nil || len(disabled) != 1 || disabled[0].Check != lint.CheckUnreachableRule {
		t.Errorf("disabled: %v %v", disabled, err)
	}
	if _, err := (lint.Config{Disable: []string{"no-such-check"}}).RunUnchecked(context.Background(), g, "main"); err == nil || !strings.Contains(err.Error(), `unknown check "no-such-check"`) {
		t.Errorf("unknown check: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if fs, err := (lint.Config{}).RunUnchecked(ctx, g, "main"); err != context.Canceled || fs != nil {
		t.Errorf("cancelled: %v %v", fs, err)
	}
}

// TestRunClassify checks the checks that come from the classification of the cost, through Run and
// RunUnchecked, with and without a context that can end.
func TestRunClassify(t *testing.T) {
	g, err := pego.ParseGrammar("var s string = \"a\"\n@start\nrule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\" / \"x\" {s := s + s} a\n")
	if err != nil {
		t.Fatal(err)
	}
	live, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, ctx := range []context.Context{context.Background(), live} {
		for _, tc := range []struct {
			config lint.Config
			want   []string
		}{
			{lint.Config{}, []string{lint.CheckGrowingVariable}},
			// The growing binding is also an exponential cause, at the same position.
			{lint.Config{Classify: true}, []string{lint.CheckSuperLinear, lint.CheckGrowingVariable, lint.CheckSuperLinear}},
			{lint.Config{Classify: true, Disable: []string{lint.CheckGrowingVariable}}, []string{lint.CheckSuperLinear, lint.CheckSuperLinear}},
		} {
			checked, err := tc.config.Run(ctx, g, "main")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range checked {
				got = append(got, f.Check)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("%+v: got %q, want %q", tc.config, got, tc.want)
			}
			unchecked, err := tc.config.RunUnchecked(ctx, g, "main")
			if err != nil || !slices.Equal(unchecked, checked) {
				t.Errorf("%+v: RunUnchecked = %v, %v; Run = %v", tc.config, unchecked, err, checked)
			}
		}
	}
}

// TestRunClassifySink checks that a run with the classification requested classifies the grammar
// once, and passes that classification, whose causes are the super-linear findings, to the sink
// of its context.
func TestRunClassifySink(t *testing.T) {
	g, err := pego.ParseGrammar("@start\nrule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n")
	if err != nil {
		t.Fatal(err)
	}
	var got []*engine.Complexity
	ctx := engine.WithComplexitySink(context.Background(), func(c *engine.Complexity) { got = append(got, c) })
	findings, err := (lint.Config{Classify: true}).Run(ctx, g, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Class != engine.ClassPoly || len(got[0].Causes) != len(findings) {
		t.Fatalf("sink got %+v, findings %v", got, findings)
	}
	got = nil
	if _, err := (lint.Config{}).Run(ctx, g, "main"); err != nil || len(got) != 0 {
		t.Errorf("without Classify: sink got %+v, %v", got, err)
	}
}

func TestChecksReturnsACopy(t *testing.T) {
	checks := lint.Checks()
	checks[0].Name = "changed"
	if lint.Checks()[0].Name == "changed" {
		t.Error("Checks shares its slice with the package")
	}
}
