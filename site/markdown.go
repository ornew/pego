package main

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdown renders a Markdown file of the repository as a page of the site: relative links are
// rewritten to site URLs (or to the source on GitHub), headings get GitHub-compatible IDs so that
// existing #fragments keep working, and GitHub alerts become callouts.
type markdown struct {
	md  goldmark.Markdown
	doc ast.Node
	src []byte
	// headings lists the headings in document order.
	headings []Heading
}

func (s *Site) parseMarkdown(p *Page, src []byte) *markdown {
	m := &markdown{src: src}
	m.md = goldmark.New(
		// GFM, with table alignment as align attributes: style attributes would need
		// 'unsafe-inline' in the Content-Security-Policy (see netlify.toml).
		goldmark.WithExtensions(
			extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
			extension.Strikethrough, extension.Linkify, extension.TaskList,
		),
		goldmark.WithParserOptions(parser.WithASTTransformers(
			util.Prioritized(transformer{s: s, p: p, m: m}, 100),
		)),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
	m.doc = m.md.Parser().Parse(text.NewReader(src))
	return m
}

// render renders the node n (by default the whole document) to HTML.
func (m *markdown) render(n ast.Node) string {
	if n == nil {
		n = m.doc
	}
	var b bytes.Buffer
	if err := m.md.Renderer().Render(&b, m.src, n); err != nil {
		panic(err) // rendering to a bytes.Buffer does not fail
	}
	return postProcess(b.String())
}

// title returns the text of the first level-1 heading.
func (m *markdown) title() string {
	for _, h := range m.headings {
		if h.Level == 1 {
			return h.Text
		}
	}
	return ""
}

// toc returns the headings of levels 2 and 3.
func (m *markdown) toc() []Heading {
	var hs []Heading
	for _, h := range m.headings {
		if h.Level == 2 || h.Level == 3 {
			hs = append(hs, h)
		}
	}
	return hs
}

type transformer struct {
	s *Site
	p *Page
	m *markdown
}

func (t transformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	ids := map[string]int{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			txt := t.m.text(n)
			id := slug(txt)
			if k := ids[id]; k > 0 {
				ids[id]++
				id = fmt.Sprintf("%s-%d", id, k)
			} else {
				ids[id] = 1
			}
			n.SetAttributeString("id", []byte(id))
			t.m.headings = append(t.m.headings, Heading{Level: n.Level, ID: id, Text: txt})
		case *ast.Link:
			n.Destination = []byte(t.s.resolveLink(t.p, string(n.Destination)))
		case *ast.Image:
			n.Destination = []byte(t.s.resolveLink(t.p, string(n.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

// text returns the text of the node as it is displayed: rendered, so that entities, escapes and
// code spans read as they do on the page, without tags, and with white space collapsed.
func (m *markdown) text(n ast.Node) string {
	var b bytes.Buffer
	r := m.md.Renderer()
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if err := r.Render(&b, m.src, c); err != nil {
			panic(err) // rendering to a bytes.Buffer does not fail
		}
	}
	// Inline tags join text without spaces ("(<code>.pegoc</code>)" reads "(.pegoc)").
	h := tagRe.ReplaceAllString(anchorRe.ReplaceAllString(b.String(), ""), "")
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(h), " "))
}

// slug returns the anchor GitHub generates for a heading: lower case, without punctuation, and with
// spaces replaced by hyphens.
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	alertRe   = regexp.MustCompile(`<blockquote>\s*<p>\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*`)
	headingRe = regexp.MustCompile(`<h([2-4]) id="([^"]+)">`)
	anchorRe  = regexp.MustCompile(`<a class="anchor"[^>]*>#</a>`)
	tagRe     = regexp.MustCompile(`<[^>]*>`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// postProcess turns GitHub alerts into callouts and adds a self link to each heading.
func postProcess(s string) string {
	s = alertRe.ReplaceAllStringFunc(s, func(m string) string {
		kind := alertRe.FindStringSubmatch(m)[1]
		label := kind[:1] + strings.ToLower(kind[1:])
		return fmt.Sprintf(`<blockquote class="callout callout-%s"><p class="callout-title">%s</p><p>`, strings.ToLower(kind), label)
	})
	return headingRe.ReplaceAllString(s, `<h$1 id="$2"><a class="anchor" href="#$2" aria-hidden="true">#</a>`)
}

// plainText returns the text of an HTML fragment, for the search index.
func plainText(h string) string {
	h = anchorRe.ReplaceAllString(h, "")
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(h, " ")), " "))
}
