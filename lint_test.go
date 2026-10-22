package pego_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestLint(t *testing.T) {
	g, err := pego.ParseGrammar("@start\nrule main = \"a\" / \"ab\"\nrule unused = \"x\"")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := pego.Lint(g, "main")
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
	if fs[0].Severity != pego.SeverityError || fs[0].Rule != "main" || fs[0].Fix != "move it before alternative 1" {
		t.Errorf("finding %+v", fs[0])
	}

	fs, err = pego.Lint(g, "main", pego.DisableChecks("unreachable-rule"))
	if err != nil || len(fs) != 1 || fs[0].Check != "shadowed-alternative" {
		t.Errorf("disabled: %v %v", fs, err)
	}
	// Every start rule is used, whichever one is linted from.
	several, err := pego.ParseGrammar("@start(default)\nrule main = \"a\"\n@start\nrule other = \"x\"\nrule unused = \"y\"")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{"", "main", "other"} {
		fs, err = pego.Lint(several, start)
		if err != nil || len(fs) != 1 || fs[0].Rule != "unused" || !strings.Contains(fs[0].Message, "no start rule calls it") {
			t.Errorf("several start rules, start %q: %v %v", start, fs, err)
		}
	}

	if _, err := pego.Lint(g, "main", pego.DisableChecks("no-such-check")); err == nil || !strings.Contains(err.Error(), `unknown check "no-such-check"`) {
		t.Errorf("unknown check: %v", err)
	}
	if _, err := pego.Lint(g, "start"); err == nil || !strings.Contains(err.Error(), "rule start is not defined") {
		t.Errorf("unknown start: %v", err)
	}
	// An empty start selects the default start rule, and only start rules can be selected.
	if fs, err := pego.Lint(g, "", pego.DisableChecks("shadowed-alternative")); err != nil || len(fs) != 1 || fs[0].Rule != "unused" || !strings.Contains(fs[0].Message, "main does not call it") {
		t.Errorf("default start: %v %v", fs, err)
	}
	inner, err := pego.ParseGrammar("@start\nrule main = inner\nrule inner = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pego.Lint(inner, "inner"); err == nil || !strings.Contains(err.Error(), "rule inner is not a start rule") {
		t.Errorf("not a start rule: %v", err)
	}
	bad, err := pego.ParseGrammar("@start\nrule main = undefined")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pego.Lint(bad, "main"); err == nil || !strings.Contains(err.Error(), "undefined rule undefined") {
		t.Errorf("invalid grammar: %v", err)
	}
	for _, c := range pego.LintChecks() {
		if c.Name == "" || c.Summary == "" || c.Severity < pego.SeverityHint || c.Severity > pego.SeverityError {
			t.Errorf("check %+v", c)
		}
	}
}
