package main

import (
	"github.com/ornew/nanosite/render"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
)

// Project rendering policy and extension access to the shared Nanosite document.
type markdown struct {
	shared   *render.Document
	md       goldmark.Markdown
	doc      ast.Node
	src      []byte
	headings []Heading
}

func (s *Site) parseMarkdown(p *Page, src []byte) *markdown {
	d := render.Parse(src, render.Options{RawHTML: true, AlignAttributes: true, Alerts: true, HeadingLinks: true, ResolveLink: func(dest string) string { return s.resolveLink(p, dest) }})
	return &markdown{shared: d, md: d.Markdown, doc: d.AST, src: d.Source, headings: d.Headings}
}
func (m *markdown) render(n ast.Node) string {
	s, err := m.shared.Render(n)
	if err != nil {
		panic(err)
	}
	return s
}
func (m *markdown) title() string          { return m.shared.Title() }
func (m *markdown) toc() []Heading         { return m.shared.TOC(2, 3) }
func (m *markdown) text(n ast.Node) string { return m.shared.Text(n) }
func slug(s string) string                 { return render.Slug(s) }
func postProcess(s string) string          { return render.PostProcess(s, true, true) }
func plainText(s string) string            { return render.PlainText(s) }
