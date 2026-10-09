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

var pegoBlock = regexp.MustCompile("(?s)```pego\n(.*?)```")

// notExpressed lists the errors of the grammar reader that syntax.md says its grammar does not express.
var notExpressed = []string{"invalid range", "invalid repetition", "invalid number"}

// TestSyntaxSummary runs the grammar of syntax.md on every .pego file of the repository and on every pego
// code block of the documentation, and requires it to accept exactly what pego.ParseGrammar accepts.
func TestSyntaxSummary(t *testing.T) {
	doc, err := os.ReadFile("syntax.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := pegoBlock.FindAllStringSubmatch(string(doc), -1)
	if len(blocks) != 1 {
		t.Fatalf("syntax.md has %d pego code blocks, want 1", len(blocks))
	}
	p, err := pego.CompileSource(blocks[0][1], "file")
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
			for i, m := range pegoBlock.FindAllStringSubmatch(string(src), -1) {
				blocksChecked++
				check(fmt.Sprintf("%s block %d", path, i+1), m[1])
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
	p, err := pego.CompileSource(pegoBlock.FindStringSubmatch(string(doc))[1], "file")
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"capture with a space":      `def a = x :b`,
		"repetition with a space":   `def a = "a" {2,3}`,
		"call of a level and group": `def a = b(c d)`,
		"attribute and group":       `def a = "a" #error(m "x")`,
		"unterminated string":       `def a = "a`,
		"unknown escape":            `def a = "\x"`,
		"newline in string":         "def a = \"a\n\"",
		"empty class":               `def a = (?)`,
		"block comment":             `def a = /* x */ "a"`,
		"package last":              "def a = \"a\"\npackage x",
		"keyword as reference":      `def a = type`,
		"chained comparison":        `def a = "a" -> 1 < 2 < 3`,
		"missing action":            `def a = "a" ->`,
	} {
		if _, err := p.Parse(src, pego.RecognizeOnly()); err == nil {
			t.Errorf("%s: %q is accepted", name, src)
		}
	}
	for name, src := range map[string]string{
		"level call":                `def a = b(c)`,
		"class after a name":        `def a = b(?x)`,
		"comment at the end":        `def a = "a" // c`,
		"definitions on one line":   `def a = "a" def b = a`,
		"attribute without args":    `def a = "a" #stream`,
		"stacked postfix operators": `def a = "a"*?`,
		"carriage return":           "def a = \"a\" // c\r\ndef b = a",
	} {
		if _, err := p.Parse(src, pego.RecognizeOnly()); err != nil {
			t.Errorf("%s: %q is rejected: %v", name, src, err)
		}
	}
}
