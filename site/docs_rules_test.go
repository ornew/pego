package main

import (
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Documentation kinds. Links may only point in the directions of linkAllowed: the
// specification is self-contained, guides rest on the specification, the design
// records and the performance documents, recipes on guides and the specification,
// and tutorials may link anywhere.
const (
	kindSpec     = "specification"
	kindGuide    = "guide"
	kindCookbook = "cookbook"
	kindTutorial = "tutorial"
	kindDesign   = "design record"
	kindOptim    = "optimization record"
	kindPerf     = "performance document" // docs/performance.md and docs/benchmarks.md
	kindProject  = "project document"     // everything else under docs/, and README.md
	kindOther    = "other"                // source files, parsers, examples, external links
)

// externalAllowlist names the hosts that the specification may link to: the
// publications of the standards that it cites.
var externalAllowlist = []string{"www.rfc-editor.org", "rfc-editor.org"}

var (
	refLinkRe      = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:\s*(<[^>]*>|\S+)`)
	autolinkRe     = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9+.-]*:[^<>\s]+)>`)
	htmlHrefRe     = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>` + "`" + `]+))`)
	fenceOpenRe    = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	githubBlobRe   = regexp.MustCompile(`(?i)^https://github\.com/ornew/pego/(?:blob|tree)/[^/]+/(.*)$`)
	siteURLRe      = regexp.MustCompile(`(?i)^https?://zeroparse\.dev(/[^?#]*)?`)
	urlSchemeRe    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	opaqueSchemeRe = regexp.MustCompile(`(?i)^(?:mailto|tel|sms|data|javascript|urn):`)
	listItemRe     = regexp.MustCompile(`^\s*(?:[-*+]|\d{1,9}[.)])(?:\s|$)`)
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
	case under(rel, "docs/design"):
		return kindDesign
	case under(rel, "docs/optimizations"):
		return kindOptim
	case rel == "docs/performance.md", rel == "docs/benchmarks.md":
		return kindPerf
	case under(rel, "docs"), path.Dir(rel) == "." && path.Ext(rel) == ".md":
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
		switch to {
		case kindSpec, kindGuide, kindDesign, kindOptim, kindPerf, kindOther:
			return true
		}
		return false
	case kindCookbook:
		switch to {
		case kindSpec, kindGuide, kindCookbook, kindOther:
			return true
		}
		return false
	}
	return true
}

// isExternal reports whether dest names a resource outside the repository: it has a URL
// scheme in any letter case followed by "//" or one of the schemes without an authority
// (mailto and the like), or it is a protocol-relative URL. A file name that contains a
// colon, such as "foo:bar.md", is a path.
func isExternal(dest string) bool {
	dest = strings.TrimPrefix(dest, "<")
	if m := urlSchemeRe.FindString(dest); m != "" {
		return strings.HasPrefix(dest[len(m):], "//") || opaqueSchemeRe.MatchString(dest)
	}
	return strings.HasPrefix(dest, "//")
}

// externalAllowed reports whether the specification may link to the external dest.
func externalAllowed(dest string) bool {
	dest = strings.TrimSuffix(strings.TrimPrefix(dest, "<"), ">")
	u, err := url.Parse(dest)
	if err != nil || (!strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "http")) {
		return false
	}
	return slices.Contains(externalAllowlist, strings.ToLower(u.Hostname()))
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
	} else if isExternal(dest) || strings.HasPrefix(dest, "#") {
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
// inline code spans removed, so that only the text that can contain links remains. A
// fence may be indented, as it is inside a list item. A code span may continue over
// the lines of one paragraph, and the lines it covers stay in place, so line numbers
// are preserved.
func maskCode(lines []string) []string {
	out := make([]string, len(lines))
	var fence string
	inList, prevBlank, prevCode := false, true, false
	for i, text := range lines {
		if fence != "" {
			trimmed := strings.TrimSpace(text)
			if strings.TrimLeft(trimmed, fence[:1]) == "" && len(trimmed) >= len(fence) {
				fence = ""
			}
			continue
		}
		blank := strings.TrimSpace(text) == ""
		indent := indentOf(text)
		if !blank {
			if listItemRe.MatchString(text) {
				inList = true
			} else if indent == 0 {
				inList = false
			}
		}
		// An indented block outside a list is indented code, in which a fence is text.
		if !blank && indent >= 4 && !inList && (prevBlank || prevCode) {
			prevCode, prevBlank = true, false
			continue
		}
		if !blank {
			prevCode = false
		}
		prevBlank = blank
		if m := fenceOpenRe.FindStringSubmatch(text); m != nil {
			fence = m[1]
			continue
		}
		out[i] = text
	}
	// Code spans do not cross a table row or the start of a list item.
	for i := 0; i < len(out); {
		if strings.TrimSpace(out[i]) == "" {
			i++
			continue
		}
		j := i + 1
		if !isTableRow(out[i]) {
			for j < len(out) && strings.TrimSpace(out[j]) != "" && !isTableRow(out[j]) && !listItemRe.MatchString(out[j]) {
				j++
			}
		}
		copy(out[i:j], strings.Split(stripInlineCode(strings.Join(out[i:j], "\n")), "\n"))
		i = j
	}
	return out
}

// indentOf returns the width of the leading white space of text, with tabs to multiples of four.
func indentOf(text string) int {
	n := 0
	for _, c := range text {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4 - n%4
		default:
			return n
		}
	}
	return n
}

func isTableRow(text string) bool { return strings.HasPrefix(strings.TrimSpace(text), "|") }

// stripInlineCode removes code spans: a run of n backticks up to the next run of
// exactly n backticks. An unmatched run is ordinary text. The line breaks inside a
// removed span are kept.
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
		b.WriteString(strings.Repeat("\n", strings.Count(text[i:end], "\n")))
		i = end
	}
	return b.String()
}

// destAt is a link destination and the offset at which it starts.
type destAt struct {
	dest string
	off  int
}

// inlineDestinations returns the destinations of the inline links "](dest)" in text. White
// space, including line breaks, may surround the destination. A destination is either
// enclosed in angle brackets or ends at white space or at the parenthesis that closes
// the link, and may contain balanced parentheses.
func inlineDestinations(text string) []destAt {
	var dests []destAt
	for from := 0; ; {
		i := strings.Index(text[from:], "](")
		if i < 0 {
			return dests
		}
		j := from + i + 2
		from = j
		for j < len(text) && strings.ContainsRune(" \t\r\n", rune(text[j])) {
			j++
		}
		if j < len(text) && text[j] == '<' {
			if k := strings.IndexByte(text[j:], '>'); k > 0 {
				dests = append(dests, destAt{text[j+1 : j+k], j + 1})
			}
			continue
		}
		depth := 0
		k := j
		for ; k < len(text); k++ {
			c := text[k]
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' || (c == ')' && depth == 0) {
				break
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
			}
		}
		if k > j {
			dests = append(dests, destAt{text[j:k], j})
		}
	}
}

// documentDestinations returns the link destinations of masked Markdown, which is the
// lines of a document joined by line breaks.
func documentDestinations(text string) []destAt {
	dests := inlineDestinations(text)
	for _, m := range autolinkRe.FindAllStringSubmatchIndex(text, -1) {
		dests = append(dests, destAt{text[m[2]:m[3]], m[2]})
	}
	for _, m := range htmlHrefRe.FindAllStringSubmatchIndex(text, -1) {
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				dests = append(dests, destAt{text[m[2*g]:m[2*g+1]], m[2*g]})
			}
		}
	}
	for _, m := range refLinkRe.FindAllStringSubmatchIndex(text, -1) {
		dests = append(dests, destAt{text[m[2]:m[3]], m[2]})
	}
	sort.SliceStable(dests, func(i, j int) bool { return dests[i].off < dests[j].off })
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
			doc := strings.Join(maskCode(strings.Split(string(data), "\n")), "\n")
			for _, d := range documentDestinations(doc) {
				line := strconv.Itoa(strings.Count(doc[:d.off], "\n") + 1)
				rp, ok := linkTarget(rel, d.dest)
				if !ok {
					if from == kindSpec && isExternal(d.dest) && !externalAllowed(d.dest) {
						problems = append(problems, rel+":"+line+": specification page links to external URL "+d.dest)
					}
					continue
				}
				if to := docKind(rp); !linkAllowed(from, to) {
					problems = append(problems, rel+":"+line+": "+from+" page links to "+to+" "+d.dest)
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
// specification links only to the specification (and to standards), guides and recipes
// link neither to project documents nor to the tutorial, guides never link to the
// cookbook, and the cookbook links only to guides, the specification and code.
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
		{kindGuide, kindDesign, true},
		{kindGuide, kindOptim, true},
		{kindGuide, kindPerf, true},
		{kindGuide, kindProject, false},
		{kindGuide, kindOther, true},
		{kindGuide, kindCookbook, false},
		{kindGuide, kindTutorial, false},
		{kindCookbook, kindGuide, true},
		{kindCookbook, kindSpec, true},
		{kindCookbook, kindCookbook, true},
		{kindCookbook, kindOther, true},
		{kindCookbook, kindDesign, false},
		{kindCookbook, kindOptim, false},
		{kindCookbook, kindPerf, false},
		{kindCookbook, kindProject, false},
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
		{"docs/guide/a.md", "/spec/language/types.md", "spec/language/types.md"},
		{"docs/guide/a.md", "https://github.com/ornew/pego/blob/main/docs/guide/editor-support.md", "docs/guide/editor-support.md"},
		{"docs/guide/a.md", "b%20c.md", "docs/guide/b c.md"},
		{"docs/guide/a.md", "../tutorial/", "docs/tutorial"},
		{"docs/guide/a.md", "../cookbook", "docs/cookbook"},
		{"docs/guide/a.md", "https://zeroparse.dev/docs/tutorial/getting-started/", "docs/tutorial/getting-started"},
		{"spec/a.md", "https://zeroparse.dev/", "README.md"},
		{"spec/a.md", "<../examples/x y.pego>", "examples/x y.pego"},
	} {
		if got, ok := linkTarget(c.rel, c.dest); !ok || got != c.want {
			t.Errorf("linkTarget(%q, %q) = %q, %v, want %q", c.rel, c.dest, got, ok, c.want)
		}
	}
	if _, ok := linkTarget("spec/a.md", "https://www.rfc-editor.org/rfc/rfc2119"); ok {
		t.Error("external link resolved to a repository path")
	}
	for _, dest := range []string{"HTTPS://example.com/x", "Mailto:a@b.c", "//example.com/x", "<https://example.com>"} {
		if !isExternal(dest) || externalAllowed(dest) {
			t.Errorf("%q: external = %v, allowed = %v, want true, false", dest, isExternal(dest), externalAllowed(dest))
		}
	}
	for _, dest := range []string{"https://www.rfc-editor.org/rfc/rfc2119", "HTTPS://RFC-EDITOR.ORG/rfc/rfc8174"} {
		if !isExternal(dest) || !externalAllowed(dest) {
			t.Errorf("%q: want external and allowed", dest)
		}
	}
	if _, ok := linkTarget("spec/a.md", "HTTPS://GITHUB.COM/ornew/pego/blob/main/pego.go"); !ok {
		t.Error("an upper-case GitHub URL did not resolve to a repository path")
	}
}

func TestDirectoryAndSiteLinkKinds(t *testing.T) {
	for _, c := range []struct{ rel, want string }{
		{"docs/tutorial", kindTutorial},
		{"docs/cookbook", kindCookbook},
		{"docs/guide", kindGuide},
		{"docs/guide/a.md", kindGuide},
		{"docs/tutorials-old.md", kindProject},
		{"docs/design/records/001-a.md", kindDesign},
		{"docs/optimizations/001-a.md", kindOptim},
		{"docs/performance.md", kindPerf},
		{"docs/benchmarks.md", kindPerf},
		{"spec", kindSpec},
		{"spec/types", kindSpec},
		{"spec/language/syntax.md", kindSpec},
		{"spec/language/testdata/x.pego", kindOther},
		{"examples/x.pego", kindOther},
		{"README.md", kindProject},
		{"AGENTS.md", kindProject},
		{"CONTRIBUTING.md", kindProject},
		{"go.mod", kindOther},
		{"parsers/README.md", kindOther},
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
		"[a](x.md) and [b](<y z.md>) and [c](f(1).md \"title\") and [d]( spaced.md )",
		"see <https://zeroparse.dev/docs/guide/> and <a href=\"../tutorial/x.md\">t</a> and <a href=bare.md>u</a>",
		"inline `[no](bad.md)` and ``[no](bad2.md) with ` inside`` then [yes](ok.md)",
		"a stray ` backtick before [kept](stray.md) in a paragraph",
		"",
		"```go",
		"[no](fence.md)",
		"````",
		"[yes](after.md)",
		"~~~text",
		"```",
		"[no](tilde.md)",
		"~~~",
		"[ref]: <r s.md>",
		"",
		"[multi](",
		"    next-line.md",
		")",
		"",
		"1. an item",
		"",
		"    ```text",
		"    [no](indented-fence.md)",
		"    ```",
		"",
		"a span `over",
		"[no](two-lines.md)` and [yes](end.md)",
		"",
		"| `odd | [row1](row1.md) |",
		"| even` | [row2](row2.md) |",
		"",
		"- `odd [item1](item1.md)",
		"- tick` [item2](item2.md)",
		"",
		"text",
		"",
		"    ```text",
		"    [no](indented-code.md)",
		"",
		"after [yes](after-code.md)",
		"",
		"[colon](foo:bar.md) and [mail](mailto:a@b.c) and [url](https://example.com/x.md)",
	}
	doc := strings.Join(maskCode(lines), "\n")
	var got []string
	for _, d := range documentDestinations(doc) {
		got = append(got, d.dest)
	}
	want := []string{"x.md", "y z.md", "f(1).md", "spaced.md", "https://zeroparse.dev/docs/guide/", "../tutorial/x.md", "bare.md", "ok.md", "stray.md", "after.md", "<r s.md>", "next-line.md", "end.md", "row1.md", "row2.md", "item1.md", "item2.md", "after-code.md", "foo:bar.md", "mailto:a@b.c", "https://example.com/x.md"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("destinations = %q, want %q", got, want)
	}
	if n := strings.Count(doc, "\n"); n != len(lines)-1 {
		t.Errorf("masking changed the number of lines: %d, want %d", n+1, len(lines))
	}
}

func TestIsExternal(t *testing.T) {
	for dest, want := range map[string]bool{
		"https://example.com/x.md": true,
		"HTTP://example.com":       true,
		"//example.com/x":          true,
		"mailto:a@b.c":             true,
		"foo:bar.md":               false,
		"../guide/foo:bar.md":      false,
		"x.md":                     false,
	} {
		if got := isExternal(dest); got != want {
			t.Errorf("isExternal(%q) = %v, want %v", dest, got, want)
		}
	}
}
