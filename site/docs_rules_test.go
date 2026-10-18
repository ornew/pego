package main

import (
	"bufio"
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
	inlineLinkRe = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	refLinkRe    = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*(\S+)`)
	githubBlobRe = regexp.MustCompile(`^https://github\.com/ornew/pego/(?:blob|tree)/[^/]+/(.*)$`)
)

// docKind classifies a repository path.
func docKind(rel string) string {
	switch {
	case strings.HasPrefix(rel, "spec/") && strings.HasSuffix(rel, ".md"):
		return kindSpec
	case strings.HasPrefix(rel, "docs/guide/"):
		return kindGuide
	case strings.HasPrefix(rel, "docs/cookbook/"):
		return kindCookbook
	case strings.HasPrefix(rel, "docs/tutorial/"):
		return kindTutorial
	case strings.HasPrefix(rel, "docs/"), rel == "README.md":
		return kindProject
	}
	return kindOther
}

// linkAllowed reports whether a page of kind from may link to a page of kind to.
func linkAllowed(from, to string) bool {
	switch from {
	case kindSpec:
		return to == kindSpec || to == kindOther
	case kindGuide:
		return to != kindTutorial && to != kindCookbook
	case kindCookbook:
		return to != kindTutorial
	}
	return true
}

// linkTarget resolves a link destination found in the file at rel to a repository
// path. It returns false for links that do not point into the repository.
func linkTarget(rel, dest string) (string, bool) {
	if m := githubBlobRe.FindStringSubmatch(dest); m != nil {
		dest = "/" + m[1]
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
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			fenced := false
			for line := 1; sc.Scan(); line++ {
				text := sc.Text()
				if strings.HasPrefix(strings.TrimSpace(text), "```") {
					fenced = !fenced
					continue
				}
				if fenced {
					continue
				}
				var dests []string
				for _, m := range inlineLinkRe.FindAllStringSubmatch(text, -1) {
					dests = append(dests, m[1])
				}
				if m := refLinkRe.FindStringSubmatch(text); m != nil {
					dests = append(dests, m[1])
				}
				for _, dest := range dests {
					rp, ok := linkTarget(rel, dest)
					if !ok {
						continue
					}
					if to := docKind(rp); !linkAllowed(from, to) {
						problems = append(problems, rel+":"+strconv.Itoa(line)+": "+from+" page links to "+to+" "+dest)
					}
				}
			}
			return sc.Err()
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
	} {
		if got, ok := linkTarget(c.rel, c.dest); !ok || got != c.want {
			t.Errorf("linkTarget(%q, %q) = %q, %v, want %q", c.rel, c.dest, got, ok, c.want)
		}
	}
	if _, ok := linkTarget("spec/a.md", "https://www.rfc-editor.org/rfc/rfc2119"); ok {
		t.Error("external link resolved to a repository path")
	}
}
