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

// titleCase returns heading text in the capitalization of the style sheet. Code spans,
// compounds with a hyphen, identifiers and words with their own capitals (JSON,
// TypeScript) stay as written.
func titleCase(text string) string {
	spans := codeSpanRe.FindAllString(text, -1)
	words := strings.Split(codeSpanRe.ReplaceAllString(text, "\x00"), " ")
	start := 0
	if len(words) > 0 && numberedRe.MatchString(words[0]) {
		start = 1
	}
	for n := start; n < len(words); n++ {
		w := words[n]
		if strings.Contains(w, "\x00") || strings.Contains(w, "-") {
			continue
		}
		m := titleWordRe.FindStringSubmatch(w)
		if m == nil || strings.ContainsAny(m[3], "._/@=$#(") {
			continue
		}
		pre, word, post := m[1], m[2], m[3]
		lower := strings.ToLower(word)
		if word != lower && word != strings.ToUpper(word[:1])+lower[1:] {
			continue
		}
		edge := n == start || n == len(words)-1 || (n > 0 && strings.HasSuffix(words[n-1], ":"))
		if titleLowercase[lower] && !edge {
			word = lower
		} else {
			word = strings.ToUpper(lower[:1]) + lower[1:]
		}
		words[n] = pre + word + post
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

// TestHeadingTitleCase checks the capitalization of every heading of the tutorial, the
// guides and the cookbook.
func TestHeadingTitleCase(t *testing.T) {
	for _, dir := range []string{"docs/tutorial", "docs/guide", "docs/cookbook"} {
		root := filepath.Join("..", dir)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
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
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
