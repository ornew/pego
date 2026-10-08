package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// The landing page is assembled from README.md, so that the pitch on the site and in the repository
// cannot drift apart: the tagline and the introduction come from the top of the README, the feature
// cards from the list under "Why PEGO", and the live example from its first pego code block. The
// sections after that are rendered as they are, except the documentation table, which the site
// replaces with its own navigation.

// landingData is the data of the landing template.
type landingData struct {
	Tagline     string
	Subtitle    string
	Intro       []template.HTML
	Features    []feature
	Example     string // grammar of the live example
	ExampleIn   string // its input
	Snippets    template.HTML
	Sections    template.HTML
	Docs        []docCard
	Playground  string
	GettingInfo string
}

type feature struct {
	Title string
	Body  template.HTML
}

type docCard struct {
	Title, Text, URL string
}

var (
	strongRe = regexp.MustCompile(`(?s)^<p><strong>(.*?)</strong>\s*(.*?)</p>\s*$`)
	liRe     = regexp.MustCompile(`(?s)^<li>(.*)</li>\s*$`)
	inputRe  = regexp.MustCompile(`-i '([^']*)'`)
)

// skippedSections are README sections that the landing page replaces.
var skippedSections = map[string]bool{"Why PEGO": true, "Documentation": true}

func (s *Site) renderLanding() error {
	p := s.bySrc["README.md"]
	src, err := os.ReadFile(filepath.Join(s.cfg.Repo, "README.md"))
	if err != nil {
		return err
	}
	m := s.parseMarkdown(p, src)
	var d landingData
	var snippets, sections strings.Builder
	section := ""
	for n := m.doc.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level == 2 {
			section = m.text(h)
		}
		switch {
		case section == "":
			s.landingIntro(&d, m, n, &snippets)
		case section == "Why PEGO":
			if l, ok := n.(*ast.List); ok {
				for it := l.FirstChild(); it != nil; it = it.NextSibling() {
					li := liRe.FindStringSubmatch(m.render(it))
					if li == nil {
						continue
					}
					body := strings.TrimSpace(li[1])
					if !strings.HasPrefix(body, "<p>") {
						body = "<p>" + body + "</p>"
					}
					f := strongRe.FindStringSubmatch(body)
					if f == nil {
						return fmt.Errorf("README.md: a feature under Why PEGO does not start with bold text: %.80s", body)
					}
					d.Features = append(d.Features, feature{Title: strings.TrimSuffix(f[1], "."), Body: template.HTML(f[2])})
				}
			}
		case !skippedSections[section]:
			sections.WriteString(m.render(n))
		}
	}
	if d.Tagline == "" || len(d.Features) == 0 || d.Example == "" {
		return fmt.Errorf("README.md: could not find the tagline, the features under Why PEGO or a pego code block for the landing page")
	}
	d.Snippets = template.HTML(snippets.String())
	d.Sections = template.HTML(sections.String())
	d.Playground = "playground/"
	for _, c := range []struct{ url, text string }{
		{"docs/tutorial/getting-started/", "From a first grammar to typed trees and operator precedence, step by step."},
		{"docs/guide/", "Task-oriented guides to trees, expressions, errors, context, runtimes and code generation."},
		{"spec/", "The normative definition of the PEGO grammar language."},
		{"reference/", "The Go API and the command-line tool, generated from the source."},
		{"examples/", "Complete grammars, from JSON and CSV to Go and Python."},
		{"docs/design/", "The decisions behind PEGO and their rationale."},
	} {
		q := s.page(c.url)
		if q == nil {
			return fmt.Errorf("landing page: no page at %s", c.url)
		}
		d.Docs = append(d.Docs, docCard{Title: q.Title, Text: c.text, URL: c.url})
	}

	tmpl, err := template.ParseFS(templateFS, "templates/landing.html")
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, d); err != nil {
		return err
	}
	p.Title = "PEGO: Grammar In. Bulletproof Parser Out."
	p.Description = d.Subtitle
	p.Body = template.HTML(b.String())
	p.Scripts = []string{"assets/landing.js"}
	return nil
}

// landingIntro handles the nodes before the first section of the README: the title, the tagline, the
// badges, the quick links, the introduction and the code samples.
func (s *Site) landingIntro(d *landingData, m *markdown, n ast.Node, snippets *strings.Builder) {
	src := m.src
	switch n := n.(type) {
	case *ast.Paragraph:
		html := m.render(n)
		switch {
		case n.ChildCount() == 1 && n.FirstChild().Kind() == ast.KindEmphasis && d.Tagline == "":
			d.Tagline = m.text(n)
		case strings.Contains(html, "<img"), strings.Contains(html, " · "):
			// Badges and quick links: the site has its own navigation.
		case d.Subtitle == "":
			d.Subtitle = m.text(n)
		default:
			d.Intro = append(d.Intro, template.HTML(html))
		}
	case *ast.FencedCodeBlock:
		lang := string(n.Language(src))
		var code strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			code.Write(seg.Value(src))
		}
		switch {
		case lang == "pego" && d.Example == "":
			d.Example = code.String()
		case lang == "console" && d.ExampleIn == "":
			if mm := inputRe.FindStringSubmatch(code.String()); mm != nil {
				d.ExampleIn = mm[1]
			}
			snippets.WriteString(m.render(n))
		default:
			snippets.WriteString(m.render(n))
		}
	case *ast.Blockquote:
		snippets.WriteString(m.render(n))
	}
}
