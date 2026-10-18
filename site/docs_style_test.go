package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Headings follow the capitalization of the style sheet (docs/documentation-style.md):
// every word starts with a capital letter, except articles, coordinating conjunctions
// and prepositions of up to three letters, which stay lowercase unless they are the first
// or last word or follow a colon. Prepositions of four letters or more ("With", "From",
// "Into") are capitalized, and so is the adverb "Up" in "Up to Date".
var titleLowercase = map[string]bool{
	"a": true, "an": true, "the": true,
	"and": true, "but": true, "or": true, "nor": true, "for": true, "so": true, "yet": true,
	"as": true, "at": true, "by": true, "in": true, "of": true, "off": true, "on": true,
	"per": true, "to": true, "via": true, "vs": true, "if": true,
}

var (
	codeSpanRe    = regexp.MustCompile("`[^`]*`")
	titleWordRe   = regexp.MustCompile(`^([^A-Za-z]*)([A-Za-z]+)(.*)$`)
	numberedRe    = regexp.MustCompile(`^\d+\.$`)
	headingLineRe = regexp.MustCompile(`^(#{1,6}) (.*)$`)
)

// titleWord capitalizes one word. Identifiers and words with their own capitals (JSON,
// TypeScript) stay as written; edge is true for the first and last word and the word
// after a colon, which are always capitalized.
func titleWord(w string, edge bool) string {
	m := titleWordRe.FindStringSubmatch(w)
	if m == nil || strings.ContainsAny(m[3], "._/@=$#(") {
		return w
	}
	pre, word, post := m[1], m[2], m[3]
	lower := strings.ToLower(word)
	if word != lower && word != strings.ToUpper(word[:1])+lower[1:] {
		return w
	}
	if titleLowercase[lower] && !edge {
		return pre + lower + post
	}
	return pre + strings.ToUpper(lower[:1]) + lower[1:] + post
}

// titleCase returns heading text in the capitalization of the style sheet. Code spans
// stay as written, and every part of a hyphenated word is capitalized (Built-In).
func titleCase(text string) string {
	spans := codeSpanRe.FindAllString(text, -1)
	words := strings.Split(codeSpanRe.ReplaceAllString(text, "\x00"), " ")
	start := 0
	if len(words) > 0 && numberedRe.MatchString(words[0]) {
		start = 1
	}
	for n := start; n < len(words); n++ {
		w := words[n]
		if strings.Contains(w, "\x00") {
			continue
		}
		edge := n == start || n == len(words)-1 || (n > 0 && strings.HasSuffix(words[n-1], ":"))
		if strings.Contains(w, "-") {
			parts := strings.Split(w, "-")
			for i, p := range parts {
				parts[i] = titleWord(p, true)
			}
			words[n] = strings.Join(parts, "-")
			continue
		}
		words[n] = titleWord(w, edge)
	}
	out := strings.Join(words, " ")
	for _, s := range spans {
		out = strings.Replace(out, "\x00", s, 1)
	}
	return out
}

func TestTitleCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Recovering from Errors", "Recovering From Errors"},
		{"Using the Errors from Go", "Using the Errors From Go"},
		{"Keeping Generated Code up to Date", "Keeping Generated Code Up to Date"},
		{"Generating Parsers with `pego gen`", "Generating Parsers With `pego gen`"},
		{"Labels on Predicate Actions and On `_|_`", "Labels on Predicate Actions and on `_|_`"},
		{"Why generate", "Why Generate"},
		{"Cancellation and work limits", "Cancellation and Work Limits"},
		{"A Larger Example", "A Larger Example"},
		{"What it is for", "What It Is For"},
		{"Step: a thing", "Step: A Thing"},
		{"2. The Same Language with Left Recursion", "2. The Same Language With Left Recursion"},
		{"Built-In Functions and Lambdas", "Built-In Functions and Lambdas"},
		{"Source-only AST nodes", "Source-Only AST Nodes"},
		{"Context-aware Formatting", "Context-Aware Formatting"},
		{"Typed Values (`-types`)", "Typed Values (`-types`)"},
		{"Parsing JSON and TypeScript", "Parsing JSON and TypeScript"},
		{"What Is Inside the File", "What Is Inside the File"},
	}
	for _, c := range cases {
		if got := titleCase(c.in); got != c.want {
			t.Errorf("titleCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// styledDocs returns the Markdown files whose headings the style sheet governs: the
// tutorial, the guides, the cookbook, the specification and the project documents of
// docs/ and the repository root. The generated benchmark report and the reference
// notes on other projects are not covered.
func styledDocs(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"docs/tutorial", "docs/guide", "docs/cookbook", "spec"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Ext(p) == ".md" {
				files = append(files, p)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	top, err := filepath.Glob(filepath.Join("..", "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range top {
		switch filepath.Base(p) {
		case "benchmarks.md", "ref-cpython-pegen-packrat-parsing.md":
		default:
			files = append(files, p)
		}
	}
	return append(files, filepath.Join("..", "README.md"))
}

// TestHeadingTitleCase checks the capitalization of every heading of the styled documents.
func TestHeadingTitleCase(t *testing.T) {
	for _, p := range styledDocs(t) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel("..", p)
		for i, line := range maskCode(strings.Split(string(data), "\n")) {
			m := headingLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if want := titleCase(m[2]); want != m[2] {
				t.Errorf("%s:%d: heading %q should be %q", filepath.ToSlash(rel), i+1, m[2], want)
			}
		}
	}
}

var (
	fenceLineRe  = regexp.MustCompile("^\\s*```(.*)$")
	fenceCloseRe = regexp.MustCompile("^\\s*```\\s*$")
	promptRe     = regexp.MustCompile(`^\s*\$ `)
)

// forEachFence calls f with the language tag, the line number of the opening fence and
// the lines of every fenced code block of the styled documents.
func forEachFence(t *testing.T, f func(file, lang string, line int, body []string)) {
	t.Helper()
	for _, p := range styledDocs(t) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel("..", p)
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			m := fenceLineRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			j := i + 1
			for j < len(lines) && !fenceCloseRe.MatchString(lines[j]) {
				j++
			}
			f(filepath.ToSlash(rel), strings.TrimSpace(m[1]), i+1, lines[i+1:min(j, len(lines))])
			i = j
		}
	}
}

// TestCodeFenceLanguages checks that every fenced code block names its language, and
// that shell commands are tagged bash (not sh, shell or console).
func TestCodeFenceLanguages(t *testing.T) {
	forEachFence(t, func(file, lang string, line int, _ []string) {
		switch lang {
		case "":
			t.Errorf("%s:%d: code block without a language; use pego, go, ts, json, bash or text", file, line)
		case "sh", "shell", "zsh", "console", "terminal":
			t.Errorf("%s:%d: code block tagged %q; use bash for commands and text for output", file, line, lang)
		}
	})
}

// TestCookbookRecipes checks that every recipe follows docs/cookbook/TEMPLATE.md: the
// title is followed by a paragraph that starts with "Problem.", and the sections are,
// in order, an optional Expected Output, How It Works, and Variations and Limits.
func TestCookbookRecipes(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "docs", "cookbook", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		name := filepath.Base(p)
		if name == "README.md" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		lines := maskCode(strings.Split(string(data), "\n"))
		if len(lines) < 3 || !strings.HasPrefix(lines[0], "# ") || lines[1] != "" || !strings.HasPrefix(lines[2], "**Problem.** ") {
			t.Errorf("%s: the title must be followed by a paragraph that starts with \"**Problem.** \"", name)
		}
		var sections []string
		for _, l := range lines {
			if strings.HasPrefix(l, "## ") {
				sections = append(sections, l[3:])
			}
		}
		last := len(sections) - 1
		if last < 1 || sections[last] != "Variations and Limits" {
			t.Errorf("%s: the last section must be \"Variations and Limits\", got %q", name, sections)
			continue
		}
		how := -1
		for i, s := range sections {
			if s == "How It Works" {
				how = i
			}
		}
		if how < 0 || how > last {
			t.Errorf("%s: missing the section \"How It Works\" before \"Variations and Limits\"", name)
			continue
		}
		for i, s := range sections {
			if s == "Expected Output" && i > how {
				t.Errorf("%s: \"Expected Output\" must come before \"How It Works\"", name)
			}
		}
	}
}

// TestNoCommandPrompts checks that command blocks hold commands only, without a "$ "
// prompt: the commands are a bash block and their output a separate text block.
func TestNoCommandPrompts(t *testing.T) {
	forEachFence(t, func(file, lang string, line int, body []string) {
		if lang != "bash" && lang != "text" {
			return
		}
		for n, l := range body {
			if promptRe.MatchString(l) {
				t.Errorf("%s:%d: %q starts with a shell prompt; show the command in a bash block and its output in a text block", file, line+1+n, l)
			}
		}
	})
}
