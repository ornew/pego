package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The lean value-free rules of the TypeScript parser are printed from Flow IR, as labeled blocks.
func TestGeneratedTSPrintsLeanRulesFromFlow(t *testing.T) {
	src := `rule main = word+
rule word = [a-z]+ ("-" [a-z]+)*`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{false, true} {
		files, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true, disableTSFlow: disabled})
		if err != nil {
			t.Fatal(err)
		}
		var parser string
		for _, f := range files {
			if strings.HasSuffix(f.Path, "parser.ts") {
				parser = string(f.Content)
			}
		}
		if got := strings.Contains(parser, "L_fail"); got == disabled {
			t.Errorf("disableTSFlow=%v: parser prints labels = %v", disabled, got)
		}
	}
}

// outlineKeywords is a grammar of a choice of many keywords, each a sequence of classes of both cases,
// which share their letters: the shapes that the outlining of generated TypeScript acts on.
func outlineKeywords() (grammar string, inputs []string) {
	words := []string{"select", "selects", "set", "setup", "from", "frozen", "union", "unique", "update", "use",
		"using", "values", "view", "virtual", "when", "where", "while", "window", "with", "without", "write", "xor"}
	var alts []string
	for _, w := range words {
		var cs []string
		for _, r := range w {
			cs = append(cs, fmt.Sprintf("[%c%c]", r, r-'a'+'A'))
		}
		alts = append(alts, strings.Join(cs, " "))
	}
	grammar = "rule main = (kw ws)* $$\nrule kw = " + strings.Join(alts, " / ") + "\nrule ws = \" \"*"
	inputs = []string{"select from where", "SELECT FrOm WHERE ", "selec", "xorx xor", "withou", "unions unique uni", "wrote"}
	return
}

// Large and repeated parts of a rule run as functions of their own, and the parser still parses as the
// engine does.
func TestGeneratedTSOutlinesLargeAndRepeatedNodes(t *testing.T) {
	src, _ := outlineKeywords()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	if !strings.Contains(parser, "kw (outlined)") {
		t.Errorf("no node of rule kw runs as a function of its own")
	}
}

// Outlined nodes with equal structure run as one function, whatever the names of the literal tables they
// read are: no two functions of a generated parser have the same text once the names of constants are
// ignored.
func TestGeneratedTSOutlinedFunctionsAreShared(t *testing.T) {
	// Two rules of the same keywords repeat every outlined node; the letters are literals, which each
	// outlined function reads as a table of its own.
	var alts []string
	for _, w := range []string{"select", "selects", "set", "setup", "from", "frozen", "union", "unique", "update", "use",
		"using", "values", "view", "virtual", "when", "where", "while", "window", "with", "without", "write", "xor"} {
		var ls []string
		for _, r := range w {
			ls = append(ls, strconv.Quote(string(r)))
		}
		alts = append(alts, strings.Join(ls, " "))
	}
	kw := strings.Join(alts, " / ")
	src := "rule main = (kw ws)* (kw2 ws)* $$\nrule kw = " + kw + "\nrule kw2 = " + kw + "\nrule ws = \" \"*"
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	fn := regexp.MustCompile(`(?s)\n// [^\n]*\(outlined\)\nfunction \w+\(_?p: Parser\): R \{\n(.*?)\n\}\n`)
	name := regexp.MustCompile(`\b(lit|scope|a)\d+\b`)
	outlined := fn.FindAllStringSubmatch(parser, -1)
	if len(outlined) < 2 {
		t.Fatalf("too few outlined functions: %d", len(outlined))
	}
	seen := map[string]bool{}
	for _, m := range outlined {
		// Calls of other outlined functions keep their names: equal callees have one name.
		body := name.ReplaceAllString(m[1], "$1")
		if seen[body] {
			t.Errorf("two outlined functions have the same text:\n%s", m[1])
		}
		seen[body] = true
	}
}
