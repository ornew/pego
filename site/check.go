package main

import (
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	refRe = regexp.MustCompile(`<(?:a|link|script|img)\b[^>]*?\s(?:href|src)="([^"]*)"`)
	idRe  = regexp.MustCompile(`\sid="([^"]*)"`)
)

// checkLinks checks every relative link (href and src) in the HTML files under out: the file it points
// to must exist and, for a link with a fragment to an HTML page, the page must have an element with
// that id. It returns a description of each broken link.
func checkLinks(out string) []string {
	ids := map[string]map[string]bool{} // by slash path relative to out
	docs := map[string]string{}
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".html" {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(out, p)
		rel = filepath.ToSlash(rel)
		docs[rel] = string(data)
		set := map[string]bool{}
		for _, m := range idRe.FindAllStringSubmatch(string(data), -1) {
			set[html.UnescapeString(m[1])] = true
		}
		ids[rel] = set
		return nil
	})
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for file, data := range docs {
		for _, m := range refRe.FindAllStringSubmatch(data, -1) {
			ref := html.UnescapeString(m[1])
			if ref == "" || strings.HasPrefix(ref, "//") || schemeRe.MatchString(ref) {
				continue
			}
			target, frag, _ := strings.Cut(ref, "#")
			// Fragments and paths are compared decoded: goldmark percent-encodes non-ASCII characters
			// in link destinations, while ids are written as they are.
			if f, err := url.PathUnescape(frag); err == nil {
				frag = f
			}
			if t, err := url.PathUnescape(target); err == nil {
				target = t
			}
			var tp string
			if target == "" {
				tp = file
			} else {
				tp = path.Clean(path.Join(path.Dir(file), target))
				if tp == ".." || strings.HasPrefix(tp, "../") {
					problems = append(problems, fmt.Sprintf("%s: link %s leaves the site", file, ref))
					continue
				}
				if strings.HasSuffix(target, "/") || tp == "." {
					tp = path.Join(tp, "index.html")
				} else if fi, err := os.Stat(filepath.Join(out, filepath.FromSlash(tp))); err == nil && fi.IsDir() {
					tp = path.Join(tp, "index.html")
				}
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(tp))); err != nil {
				problems = append(problems, fmt.Sprintf("%s: link to missing %s", file, ref))
				continue
			}
			if frag != "" && strings.HasSuffix(tp, ".html") && !ids[tp][frag] {
				problems = append(problems, fmt.Sprintf("%s: link %s to a missing anchor", file, ref))
			}
		}
	}
	sort.Strings(problems)
	return problems
}
