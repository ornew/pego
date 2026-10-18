package main

import (
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Documentation kinds. Links may only point in the directions of allowedLinks:
// the specification is self-contained, guides rest on the specification,
// recipes on guides and the specification, and tutorials may link anywhere.
const (
	kindSpec     = "specification"
	kindGuide    = "guide"
	kindCookbook = "cookbook"
	kindTutorial = "tutorial"
	kindProject  = "project document" // everything else under docs/, and README.md
	kindOther    = "other"            // source files, parsers, examples, external links
)

var (
	refLinkRe    = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*(<[^>]*>|\S+)`)
	autolinkRe   = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9+.-]*:[^<>\s]+)>`)
	htmlHrefRe   = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	fenceOpenRe  = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")
	githubBlobRe = regexp.MustCompile(`^https://github\.com/ornew/pego/(?:blob|tree)/[^/]+/(.*)$`)
	siteURLRe    = regexp.MustCompile(`^https?://pego\.ornew\.net(/[^?#]*)?`)
)

// under reports whether rel is dir or lies below it.
func under(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// docKind classifies a repository path. A path without an extension is a page of the
// published site (a directory link), and is classified by where it lies.
func docKind(rel string) string {
	page := path.Ext(rel) == ".md" || path.Ext(rel) == ""
	switch {
	case under(rel, "spec") && page:
		return kindSpec
	case under(rel, "docs/guide"):
		return kindGuide
	case under(rel, "docs/cookbook"):
		return kindCookbook
	case under(rel, "docs/tutorial"):
		return kindTutorial
	case under(rel, "docs"), rel == "README.md":
		return kindProject
	}
	return kindOther
}

// linkAllowed reports whether a page of kind from may link to a page of kind to.
func linkAllowed(from, to string) bool {
	switch from {
	case kindSpec:
		return to == kindSpec
	case kindGuide:
		return to != kindTutorial && to != kindCookbook
	case kindCookbook:
		return to != kindTutorial
	}
	return true
}

// linkTarget resolves a link destination found in the file at rel to a repository
// path. It returns false for links that do not point into the repository. A link to a
// page of the published site resolves to the path of that page without an extension.
func linkTarget(rel, dest string) (string, bool) {
	dest = strings.TrimSuffix(strings.TrimPrefix(dest, "<"), ">")
	if m := githubBlobRe.FindStringSubmatch(dest); m != nil {
		dest = "/" + m[1]
	} else if m := siteURLRe.FindStringSubmatch(dest); m != nil {
		dest = m[1]
		if dest == "" || dest == "/" {
			dest = "/README.md"
		}
	} else if strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") || strings.HasPrefix(dest, "#") {
		return "", false
	}
	target, _, _ := strings.Cut(dest, "#")
	target, _, _ = strings.Cut(target, "?")
	if t, err := url.PathUnescape(target); err == nil {
		target = t
	}
	if target == "" {
		return "", false
	}
	var rp string
	if strings.HasPrefix(target, "/") {
		rp = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		rp = path.Clean(path.Join(path.Dir(rel), target))
	}
	return rp, true
}

// maskCode returns the lines of a Markdown file with fenced code blocks emptied and
// inline code spans removed, so that only the text that can contain links remains.
func maskCode(lines []string) []string {
	out := make([]string, len(lines))
	var fence string
	for i, text := range lines {
		if fence != "" {
			trimmed := strings.TrimSpace(text)
			if strings.TrimLeft(trimmed, fence[:1]) == "" && len(trimmed) >= len(fence) {
				fence = ""
			}
			continue
		}
		if m := fenceOpenRe.FindStringSubmatch(text); m != nil {
			fence = m[1]
			continue
		}
		out[i] = stripInlineCode(text)
	}
	return out
}

// stripInlineCode removes code spans: a run of n backticks up to the next run of
// exactly n backticks. An unmatched run is ordinary text.
func stripInlineCode(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '`' {
			b.WriteByte(text[i])
			i++
			continue
		}
		n := 0
		for i+n < len(text) && text[i+n] == '`' {
			n++
		}
		end := -1
		for j := i + n; j < len(text); {
			if text[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(text) && text[j+m] == '`' {
				m++
			}
			if m == n {
				end = j + m
				break
			}
			j += m
		}
		if end < 0 {
			b.WriteString(text[i : i+n])
			i += n
			continue
		}
		i = end
	}
	return b.String()
}

// inlineDestinations returns the destinations of the inline links "](dest)" in text. A
// destination is either enclosed in angle brackets or ends at white space or at the
// parenthesis that closes the link, and may contain balanced parentheses.
func inlineDestinations(text string) []string {
	var dests []string
	for from := 0; ; {
		i := strings.Index(text[from:], "](")
		if i < 0 {
			return dests
		}
		j := from + i + 2
		from = j
		if j < len(text) && text[j] == '<' {
			if k := strings.IndexByte(text[j:], '>'); k > 0 {
				dests = append(dests, text[j+1:j+k])
			}
			continue
		}
		depth := 0
		k := j
		for ; k < len(text); k++ {
			c := text[k]
			if c == ' ' || c == '\t' || (c == ')' && depth == 0) {
				break
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
			}
		}
		if k > j {
			dests = append(dests, text[j:k])
		}
	}
}

// documentDestinations returns the link destinations of one line of masked Markdown.
func documentDestinations(text string) []string {
	dests := inlineDestinations(text)
	for _, m := range autolinkRe.FindAllStringSubmatch(text, -1) {
		dests = append(dests, m[1])
	}
	for _, m := range htmlHrefRe.FindAllStringSubmatch(text, -1) {
		dests = append(dests, m[1]+m[2])
	}
	if m := refLinkRe.FindStringSubmatch(text); m != nil {
		dests = append(dests, m[1])
	}
	return dests
}

// docLinkViolations returns one message per link that points in a forbidden direction.
func docLinkViolations(t *testing.T, repo string) []string {
	t.Helper()
	var problems []string
	for _, dir := range []string{"docs", "spec"} {
		err := filepath.WalkDir(filepath.Join(repo, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
				return err
			}
			rel, _ := filepath.Rel(repo, p)
			rel = filepath.ToSlash(rel)
			from := docKind(rel)
			if from == kindProject || from == kindOther || strings.HasSuffix(rel, "TEMPLATE.md") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for i, text := range maskCode(strings.Split(string(data), "\n")) {
				for _, dest := range documentDestinations(text) {
					rp, ok := linkTarget(rel, dest)
					if !ok {
						continue
					}
					if to := docKind(rp); !linkAllowed(from, to) {
						problems = append(problems, rel+":"+strconv.Itoa(i+1)+": "+from+" page links to "+to+" "+dest)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(problems)
	return problems
}

// TestDocumentLinkDirections enforces the reading order of the documentation: the
// specification links only to the specification, guides never link to the tutorial
// or the cookbook, and the cookbook never links to the tutorial.
func TestDocumentLinkDirections(t *testing.T) {
	for _, p := range docLinkViolations(t, "..") {
		t.Error(p)
	}
}

func TestLinkAllowed(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{kindSpec, kindSpec, true},
		{kindSpec, kindGuide, false},
		{kindSpec, kindCookbook, false},
		{kindSpec, kindTutorial, false},
		{kindSpec, kindProject, false},
		{kindSpec, kindOther, false},
		{kindGuide, kindSpec, true},
		{kindGuide, kindGuide, true},
		{kindGuide, kindProject, true},
		{kindGuide, kindCookbook, false},
		{kindGuide, kindTutorial, false},
		{kindCookbook, kindGuide, true},
		{kindCookbook, kindSpec, true},
		{kindCookbook, kindCookbook, true},
		{kindCookbook, kindTutorial, false},
		{kindTutorial, kindSpec, true},
		{kindTutorial, kindCookbook, true},
	}
	for _, c := range cases {
		if got := linkAllowed(c.from, c.to); got != c.want {
			t.Errorf("linkAllowed(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
	for _, c := range []struct{ rel, dest, want string }{
		{"spec/a.md", "../docs/guide/x.md#y", "docs/guide/x.md"},
		{"docs/guide/a.md", "/spec/types.md", "spec/types.md"},
		{"docs/guide/a.md", "https://github.com/ornew/pego/blob/main/docs/guide/editor-support.md", "docs/guide/editor-support.md"},
		{"docs/guide/a.md", "b%20c.md", "docs/guide/b c.md"},
		{"docs/guide/a.md", "../tutorial/", "docs/tutorial"},
		{"docs/guide/a.md", "../cookbook", "docs/cookbook"},
		{"docs/guide/a.md", "https://pego.ornew.net/docs/tutorial/getting-started/", "docs/tutorial/getting-started"},
		{"spec/a.md", "https://pego.ornew.net/", "README.md"},
		{"spec/a.md", "<../examples/x y.pego>", "examples/x y.pego"},
	} {
		if got, ok := linkTarget(c.rel, c.dest); !ok || got != c.want {
			t.Errorf("linkTarget(%q, %q) = %q, %v, want %q", c.rel, c.dest, got, ok, c.want)
		}
	}
	if _, ok := linkTarget("spec/a.md", "https://www.rfc-editor.org/rfc/rfc2119"); ok {
		t.Error("external link resolved to a repository path")
	}
}

func TestDirectoryAndSiteLinkKinds(t *testing.T) {
	for _, c := range []struct{ rel, want string }{
		{"docs/tutorial", kindTutorial},
		{"docs/cookbook", kindCookbook},
		{"docs/guide", kindGuide},
		{"docs/guide/a.md", kindGuide},
		{"docs/tutorials-old.md", kindProject},
		{"spec", kindSpec},
		{"spec/types", kindSpec},
		{"spec/syntax.md", kindSpec},
		{"spec/testdata/x.pego", kindOther},
		{"examples/x.pego", kindOther},
		{"README.md", kindProject},
	} {
		if got := docKind(c.rel); got != c.want {
			t.Errorf("docKind(%q) = %s, want %s", c.rel, got, c.want)
		}
	}
	for _, from := range []string{kindGuide, kindCookbook} {
		rp, _ := linkTarget("docs/guide/a.md", "../tutorial/")
		if linkAllowed(from, docKind(rp)) {
			t.Errorf("%s may link to a directory link of the tutorial", from)
		}
	}
	rp, _ := linkTarget("spec/a.md", "https://github.com/ornew/pego/blob/main/pego.pego")
	if linkAllowed(kindSpec, docKind(rp)) {
		t.Error("the specification may link to a source file on GitHub")
	}
}

func TestDocumentDestinations(t *testing.T) {
	lines := []string{
		"[a](x.md) and [b](<y z.md>) and [c](f(1).md \"title\")",
		"see <https://pego.ornew.net/docs/guide/> and <a href=\"../tutorial/x.md\">t</a>",
		"inline `[no](bad.md)` and ``[no](bad2.md) with ` inside`` then [yes](ok.md)",
		"```go",
		"[no](fence.md)",
		"````",
		"[yes](after.md)",
		"~~~text",
		"```",
		"[no](tilde.md)",
		"~~~",
		"[ref]: <r s.md>",
	}
	var got []string
	for _, text := range maskCode(lines) {
		got = append(got, documentDestinations(text)...)
	}
	want := []string{"x.md", "y z.md", "f(1).md", "https://pego.ornew.net/docs/guide/", "../tutorial/x.md", "ok.md", "after.md", "<r s.md>"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("destinations = %q, want %q", got, want)
	}
}
