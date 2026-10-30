package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const lintGrammar = `@start(default)
rule main = "a" / "ab" / x
@start
rule x = k:"x" v:"y" => $k
`

func TestLint(t *testing.T) {
	g := writeFile(t, "g.pego", lintGrammar)
	out, err := runCLI(t, "", "lint", "-g", g)
	want := g + `:2:19: error: alternative 2 (` + "`\"ab\"`" + `) can never match: alternative 1 (` + "`\"a\"`" + ` at 2:13) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
` + g + `:4:16: warning: capture v is never used: the action does not refer to it [unused-capture]
	fix: remove v:, or use $v
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if err == nil || err.Error() != "lint: 1 error, 1 warning, 0 hints" {
		t.Errorf("an error fails: %v", err)
	}

	// Warnings fail only with -strict.
	if _, err := runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative"); err != nil {
		t.Errorf("warnings: %v", err)
	}
	if _, err := runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative", "-strict"); err == nil {
		t.Error("warnings with -strict do not fail")
	}
	out, err = runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative,unused-capture")
	if out != "" || err != nil {
		t.Errorf("all disabled: %q %v", out, err)
	}
	out, err = runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative", "-disable", "unused-capture")
	if out != "" || err != nil {
		t.Errorf("repeated -disable: %q %v", out, err)
	}
	if _, err := runCLI(t, "", "lint", "-g", g, "-disable", "shadowing"); err == nil || !strings.Contains(err.Error(), `unknown check "shadowing"`) {
		t.Errorf("unknown check: %v", err)
	}

	// Every start rule is reached, whichever one -s selects; the rules none of them reach are not.
	out, _ = runCLI(t, "", "lint", "-g", g, "-s", "x")
	if strings.Contains(out, "unreachable-rule") {
		t.Errorf("-s x:\n%s", out)
	}
	several := writeFile(t, "several.pego", "@start(default)\nrule main = \"a\"\n@start\nrule x = \"b\"\nrule y = \"c\"")
	out, _ = runCLI(t, "", "lint", "-g", several, "-s", "x")
	if !strings.Contains(out, ":5:1: warning: rule y is never used: no start rule calls it") {
		t.Errorf("-s x, several start rules:\n%s", out)
	}
}

func TestLintJSON(t *testing.T) {
	g := writeFile(t, "g.pego", lintGrammar)
	out, err := runCLI(t, "", "lint", "-g", g, "-f", "json")
	if err == nil {
		t.Error("an error does not fail with -f json")
	}
	var doc lintOutput
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if doc.Grammar != g || doc.Start != "main" || len(doc.Findings) != 2 {
		t.Fatalf("got %+v", doc)
	}
	if !strings.Contains(out, `"severity": "error"`) || !strings.Contains(out, `"check": "shadowed-alternative"`) ||
		!strings.Contains(out, `"line": 2`) || !strings.Contains(out, `"col": 19`) || !strings.Contains(out, `"rule": "main"`) ||
		!strings.Contains(out, `"fix": "move it before alternative 1"`) {
		t.Errorf("got\n%s", out)
	}

	clean := writeFile(t, "clean.pego", "@start\nrule main = \"a\"")
	out, err = runCLI(t, "", "lint", "-g", clean, "-f", "json")
	if err != nil || !strings.Contains(out, `"findings": []`) {
		t.Errorf("no findings: %v\n%s", err, out)
	}
}

func TestLintOtherGrammars(t *testing.T) {
	// Source-derived JSON retains coordinates from the original grammar source.
	g := writeFile(t, "g.pego", "@start\nrule main = \"a\"\nrule x = \"b\"")
	j := filepath.Join(t.TempDir(), "g.json")
	if _, err := runCLI(t, "", "convert", "-o", j, g); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "", "lint", "-g", j)
	if err != nil {
		t.Fatal(err)
	}
	if want := j + ":3:1: warning: rule x is never used: main does not call it, directly or indirectly [unreachable-rule]\n"; !strings.HasPrefix(out, want) {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	// A compiled grammar holds no grammar to lint: lint needs the source, which it lints with the
	// start rules of the grammar.
	several := writeFile(t, "several.pego", "@start(default)\nrule main = \"a\"\n@start\nrule x = \"b\"\nrule y = \"c\"")
	c := filepath.Join(t.TempDir(), "g.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", several, "-s", "x", "-o", c); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "", "lint", "-g", c, "-f", "json"); err == nil || !strings.Contains(err.Error(), "lint needs the grammar source") {
		t.Errorf("compiled: %v", err)
	}
	out, err = runCLI(t, "", "lint", "-g", several, "-s", "x", "-f", "json")
	if err != nil || !strings.Contains(out, `"start": "x"`) || !strings.Contains(out, "rule y is never used: no start rule calls it") || strings.Contains(out, "rule main is never used") {
		t.Errorf("source: %v\n%s", err, out)
	}

	// A grammar that does not compile is an error, with the compile errors.
	bad := writeFile(t, "bad.pego", "@start\nrule main = y")
	if _, err := runCLI(t, "", "lint", "-g", bad); err == nil || !strings.Contains(err.Error(), bad+":2:13: undefined rule y") {
		t.Errorf("bad grammar: %v", err)
	}
	if _, err := runCLI(t, "", "lint"); err == nil || !strings.Contains(err.Error(), "-g is required") {
		t.Errorf("no grammar: %v", err)
	}
	if _, err := runCLI(t, "", "lint", "-g", g, "-f", "xml"); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestLintList(t *testing.T) {
	out, err := runCLI(t, "", "lint", "-list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shadowed-alternative      error", "unused-capture", "right-recursion", "hint"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestLintClassify(t *testing.T) {
	quadratic := writeFile(t, "q.pego", "@start\nrule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n")
	// Without -classify there is no classification and no super-linear finding.
	out, err := runCLI(t, "", "lint", "-g", quadratic)
	if out != "" || err != nil {
		t.Errorf("without -classify: %q %v", out, err)
	}
	out, err = runCLI(t, "", "lint", "-g", quadratic, "-classify")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, quadratic+":3:10: hint: the cost can grow as n^2") ||
		!strings.Contains(out, " [super-linear]\n"+quadratic+": the cost from start rule main is polynomial (n^2)\n\twitness: "+quadratic+":3:10: the cost can grow as n^2") {
		t.Errorf("got\n%s", out)
	}

	var doc lintOutput
	out, err = runCLI(t, "", "lint", "-g", quadratic, "-classify", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if c := doc.Classification; c == nil || c.Class != "polynomial" || c.Degree != 2 || len(c.Witnesses) != 1 ||
		c.Witnesses[0].Line != 3 || c.Witnesses[0].Col != 10 || c.Witnesses[0].Rule != "a" || len(c.Reasons) != 0 {
		t.Errorf("got %+v\n%s", doc.Classification, out)
	}
	out, _ = runCLI(t, "", "lint", "-g", quadratic, "-f", "json")
	if strings.Contains(out, "classification") {
		t.Errorf("classification without -classify:\n%s", out)
	}

	// A linear grammar shows only its class; the class unknown shows its first reasons and how
	// many more there are, and no finding.
	linear := writeFile(t, "l.pego", "@start\nrule main = (a / \"a\")* $$\nrule a = \"a\" \"c\"\n")
	out, err = runCLI(t, "", "lint", "-g", linear, "-classify")
	if want := linear + ": the cost from start rule main is linear\n"; out != want || err != nil {
		t.Errorf("linear: got %q %v, want %q", out, err, want)
	}
	unknown := writeFile(t, "u.pego", "@start\nrule main = c $$\nrule a = c \"+\" / \"n\"\nrule c = \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n")
	out, err = runCLI(t, "", "lint", "-g", unknown, "-classify")
	if err != nil || !strings.HasPrefix(out, unknown+": the cost from start rule main is unknown\n\treason: ") || strings.Contains(out, "[super-linear]") {
		t.Errorf("unknown: %v\n%s", err, out)
	}
	// Of more reasons, the first three are shown.
	out, err = runCLI(t, "", "lint", "-g", "../../examples/minilang/minilang.pego", "-classify")
	if err != nil || strings.Count(out, "\treason: ") != 3 || !regexp.MustCompile(`\n\t\.\.\. and \d+ more\n$`).MatchString(out) {
		t.Errorf("minilang: %v\n%s", err, out)
	}

	// An exponential class has no degree, even with polynomial witnesses; with super-linear
	// disabled, the classification is still shown.
	exp := writeFile(t, "e.pego", "var s string = \"a\"\n@start\nrule main = r (a / \"a\")* $$\nrule r = \"x\" {s := s + s} r / {len(s) > 0}\nrule a = \"a\"* \"c\"\n")
	out, _ = runCLI(t, "", "lint", "-g", exp, "-classify", "-disable", "super-linear,growing-variable")
	if !strings.HasPrefix(out, exp+": the cost from start rule main is exponential\n\twitness: ") {
		t.Errorf("exponential: got\n%s", out)
	}
	out, _ = runCLI(t, "", "lint", "-g", exp, "-classify", "-f", "json")
	doc = lintOutput{}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if c := doc.Classification; c == nil || c.Class != "exponential" || strings.Contains(out, "\"degree\"") {
		t.Errorf("exponential: got %+v\n%s", doc.Classification, out)
	}
}
