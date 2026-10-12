// Package spec_test checks the grammar in syntax.md against the parser of the reference implementation.
package spec_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

var pegoBlock = regexp.MustCompile("(?ms)^([ \t]*)```pego[ \t]*\r?\n(.*?)^[ \t]*```[ \t]*$")

// Markdown removes the fence indentation from list-nested code blocks.
func pegoCodeBlocks(src string) []string {
	var blocks []string
	for _, m := range pegoBlock.FindAllStringSubmatch(src, -1) {
		lines := strings.Split(m[2], "\n")
		for i := range lines {
			lines[i] = strings.TrimPrefix(lines[i], m[1])
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return blocks
}

// notExpressed lists the errors of the grammar reader that syntax.md says its grammar does not express.
var notExpressed = []string{"invalid range", "invalid repetition", "invalid number"}

// TestSyntaxSummary runs the grammar of syntax.md on every .pego file of the repository and on every pego
// code block of the documentation, and requires it to accept exactly what pego.ParseGrammar accepts.
func TestSyntaxSummary(t *testing.T) {
	doc, err := os.ReadFile("syntax.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := pegoCodeBlocks(string(doc))
	if len(blocks) != 1 {
		t.Fatalf("syntax.md has %d pego code blocks, want 1", len(blocks))
	}
	p, err := pego.CompileSource(blocks[0], "file")
	if err != nil {
		t.Fatalf("the grammar of syntax.md does not compile: %v", err)
	}

	check := func(name, src string) {
		t.Helper()
		_, readErr := pego.ParseGrammar(src)
		if readErr != nil {
			for _, s := range notExpressed {
				if strings.Contains(readErr.Error(), s) {
					return
				}
			}
		}
		_, err := p.Parse(src, pego.RecognizeOnly())
		if (readErr == nil) != (err == nil) {
			t.Errorf("%s: the reference parser says %v, the grammar of syntax.md says %v", name, readErr, err)
		}
	}

	files, blocksChecked := 0, 0
	err = filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != ".." && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".pego":
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files++
			check(path, string(src))
		case ".md":
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, m := range pegoCodeBlocks(string(src)) {
				blocksChecked++
				check(fmt.Sprintf("%s block %d", path, i+1), m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 || blocksChecked == 0 {
		t.Fatalf("checked %d files and %d code blocks", files, blocksChecked)
	}
}

// TestSyntaxSummaryRejects checks that the grammar of syntax.md rejects the constructs that the specification
// says are errors.
func TestSyntaxSummaryRejects(t *testing.T) {
	doc, err := os.ReadFile("syntax.md")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(pegoCodeBlocks(string(doc))[0], "file")
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"capture with a space":        `rule a = x :b`,
		"removed repetition spelling": `rule a = "a" {2,3}`,
		"call of a level and group":   `rule a = b(c d)`,
		"attribute and group":         `rule a = "a" @error(message=)`,
		"unterminated string":         `rule a = "a`,
		"unknown escape":              `rule a = "\x"`,
		"newline in string":           "rule a = \"a\n\"",
		"empty class":                 `rule a = []`,
		"unterminated block comment":  `rule a = /* x "a"`,
		"package last":                "rule a = \"a\"\npackage x",
		"chained comparison":          `rule a = "a" => 1 < 2 < 3`,
		"missing action":              `rule a = "a" =>`,
	} {
		if _, err := p.Parse(src, pego.RecognizeOnly()); err == nil {
			t.Errorf("%s: %q is accepted", name, src)
		}
	}
	for name, src := range map[string]string{
		"level call":                `rule a = level(b, c)`,
		"contextual word reference": `rule a = type`,
		"class after a name":        `rule a = b [x]`,
		"comment at the end":        `rule a = "a" // c`,
		"definitions on one line":   `rule a = "a"; rule b = a`,
		"attribute without args":    `rule a = "a" #stream`,
		"stacked postfix operators": `rule a = "a"*?`,
		"carriage return":           "rule a = \"a\" // c\rrule b = a",
	} {
		if _, err := p.Parse(src, pego.RecognizeOnly()); err != nil {
			t.Errorf("%s: %q is rejected: %v", name, src, err)
		}
	}
}
