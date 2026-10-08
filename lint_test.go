package pego_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestLint(t *testing.T) {
	g, err := pego.ParseGrammar("def main = \"a\" / \"ab\"\ndef unused = \"x\"")
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
		"1:18: error: alternative 2 (`\"ab\"`) can never match: alternative 1 (`\"a\"` at 1:12) matches first wherever it could [shadowed-alternative]",
		"2:1: warning: rule unused is never used: main does not call it, directly or indirectly [unreachable-rule]",
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
	// With another start rule, main is the unused one.
	fs, err = pego.Lint(g, "unused", pego.DisableChecks("shadowed-alternative"))
	if err != nil || len(fs) != 1 || fs[0].Rule != "main" {
		t.Errorf("start unused: %v %v", fs, err)
	}

	if _, err := pego.Lint(g, "main", pego.DisableChecks("no-such-check")); err == nil || !strings.Contains(err.Error(), `unknown check "no-such-check"`) {
		t.Errorf("unknown check: %v", err)
	}
	if _, err := pego.Lint(g, "start"); err == nil || !strings.Contains(err.Error(), "start rule start is not defined") {
		t.Errorf("unknown start: %v", err)
	}
	bad, err := pego.ParseGrammar("def main = undefined")
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
