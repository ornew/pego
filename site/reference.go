package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/token"
	"html/template"
	"path/filepath"
	"strconv"
	"strings"
)

// The reference is generated from the Go source of the repository with go/doc, so that it always
// matches the code it is built with; pkg.go.dev has the reference of published versions.

const modulePath = "github.com/ornew/pego"

// refPackage is a package documented in the reference.
type refPackage struct {
	dir, importPath, url, title string
}

var refPackages = []refPackage{
	{dir: ".", importPath: modulePath, url: "reference/pego/", title: "Package pego"},
	{dir: "grammar", importPath: modulePath + "/grammar", url: "reference/grammar/", title: "Package grammar"},
}

const cliURL = "reference/cli/"

func (s *Site) referencePages() *Section {
	sec := &Section{Name: "Reference"}
	idx := &Page{URL: "reference/", Kind: "doc", Title: "API Reference", Nav: "Overview", Section: sec.Name}
	s.add(idx)
	sec.Pages = append(sec.Pages, idx)
	for _, rp := range refPackages {
		p := &Page{URL: rp.url, Kind: "doc", Title: rp.title, Nav: rp.title, Section: sec.Name,
			SourceURL: s.cfg.GitHub + "/tree/" + s.cfg.Ref + "/" + strings.TrimPrefix(rp.dir, ".")}
		s.add(p)
		sec.Pages = append(sec.Pages, p)
	}
	cli := &Page{URL: cliURL, Kind: "doc", Title: "The pego Command", Nav: "Command pego", Section: sec.Name,
		SourceURL: s.cfg.GitHub + "/blob/" + s.cfg.Ref + "/cmd/pego/main.go"}
	s.add(cli)
	sec.Pages = append(sec.Pages, cli)
	return sec
}

func (s *Site) page(url string) *Page {
	for _, p := range s.pages {
		if p.URL == url {
			return p
		}
	}
	return nil
}

// parseDir parses the non-test Go files of the package in dir that build on the host.
func parseDir(repo, dir string) (*token.FileSet, []*ast.File, error) {
	abs := filepath.Join(repo, filepath.FromSlash(dir))
	bp, err := build.ImportDir(abs, build.ImportComment)
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(abs, name), nil, parser.ParseComments)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, f)
	}
	return fset, files, nil
}

// loadPackage reads the documentation of the package in dir. go/doc removes unexported declarations
// and function bodies from the files it is given.
func loadPackage(repo, dir, importPath string) (*token.FileSet, []*ast.File, *doc.Package, error) {
	fset, files, err := parseDir(repo, dir)
	if err != nil {
		return nil, nil, nil, err
	}
	pkg, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return nil, nil, nil, err
	}
	return fset, files, pkg, nil
}

func (s *Site) renderReference() error {
	idx := s.page("reference/")
	var ib strings.Builder
	ib.WriteString(`<h1 id="api-reference">API Reference</h1>
<p>The reference of the public Go API and of the command-line tool, generated from the source of this
version of PEGO. The reference of released versions is also on
<a href="https://pkg.go.dev/github.com/ornew/pego">pkg.go.dev</a>.</p>
<table><thead><tr><th>Package</th><th>Synopsis</th></tr></thead><tbody>
`)
	for _, rp := range refPackages {
		p := s.page(rp.url)
		fset, files, pkg, err := loadPackage(s.cfg.Repo, rp.dir, rp.importPath)
		if err != nil {
			return fmt.Errorf("reference %s: %w", rp.importPath, err)
		}
		r := &refRenderer{s: s, page: p, fset: fset, files: files, pkg: pkg, dir: rp.dir}
		ifset, ifiles, ipkg, err := loadPackage(s.cfg.Repo, "internal/engine", modulePath+"/internal/engine")
		if err != nil {
			return fmt.Errorf("reference %s: %w", rp.importPath, err)
		}
		r.internal = &refRenderer{s: s, page: p, fset: ifset, files: ifiles, pkg: ipkg, dir: "internal/engine", ids: map[string]bool{}}
		for _, t := range pkg.Types {
			if target := aliasTarget(t); target != "" {
				r.internal.ids[target] = true
			}
		}
		p.Body = template.HTML(r.render(rp))
		p.TOC = r.toc
		p.Text = plainText(string(p.Body))
		p.Description = pkg.Synopsis(pkg.Doc)
		fmt.Fprintf(&ib, "<tr><td><a href=\"%s\"><code>%s</code></a></td><td>%s</td></tr>\n",
			relURL(idx.URL, p.URL), rp.importPath, template.HTMLEscapeString(p.Description))
	}
	cli := s.page(cliURL)
	body, toc, err := s.renderCLI(cli)
	if err != nil {
		return fmt.Errorf("reference of the pego command: %w", err)
	}
	cli.Body, cli.TOC, cli.Text = template.HTML(body), toc, plainText(body)
	fmt.Fprintf(&ib, "<tr><td><a href=\"%s\"><code>pego</code></a> (command)</td><td>%s</td></tr>\n",
		relURL(idx.URL, cli.URL), template.HTMLEscapeString(cli.Description))
	ib.WriteString("</tbody></table>\n")
	idx.Body = template.HTML(ib.String())
	idx.Text = plainText(ib.String())
	return nil
}

type refRenderer struct {
	s     *Site
	page  *Page
	fset  *token.FileSet
	files []*ast.File
	pkg   *doc.Package
	dir   string
	b     strings.Builder
	toc   []Heading
	// ids, if set, lists the symbols on the page; links to other symbols of the package are not
	// linked (used for the internal package, whose types appear only through aliases).
	ids map[string]bool
	// internal documents the targets of aliases to types of the internal engine package.
	internal *refRenderer
}

func (r *refRenderer) heading(level int, id, text string) {
	fmt.Fprintf(&r.b, "<h%d id=\"%s\"><a class=\"anchor\" href=\"#%s\" aria-hidden=\"true\">#</a>%s</h%d>\n",
		level, id, id, template.HTMLEscapeString(text), level)
	r.toc = append(r.toc, Heading{Level: level, ID: id, Text: text})
}

// docHTML renders a doc comment, linking [Name] references to the reference pages.
func (r *refRenderer) docHTML(text string) string {
	if text == "" {
		return ""
	}
	pr := r.pkg.Printer()
	pr.HeadingLevel = 4
	pr.DocLinkURL = func(link *comment.DocLink) string {
		frag := link.Name
		if link.Recv != "" {
			frag = link.Recv + "." + link.Name
		}
		if frag != "" {
			frag = "#" + frag
		}
		if link.ImportPath == "" || link.ImportPath == r.pkg.ImportPath {
			if r.ids != nil && !r.ids[strings.TrimPrefix(frag, "#")] {
				return ""
			}
			return frag
		}
		for _, rp := range refPackages {
			if rp.importPath == link.ImportPath {
				return relURL(r.page.URL, rp.url) + frag
			}
		}
		return "https://pkg.go.dev/" + link.ImportPath + frag
	}
	return string(pr.HTML(r.pkg.Parser().Parse(text)))
}

// decl prints a declaration with the comments inside it (such as those of struct fields).
func (r *refRenderer) decl(d ast.Decl) string {
	var comments []*ast.CommentGroup
	for _, f := range r.files {
		if f.Pos() <= d.Pos() && d.Pos() < f.End() {
			comments = f.Comments
		}
	}
	switch d := d.(type) {
	case *ast.FuncDecl:
		c := *d
		c.Doc, c.Body = nil, nil
		return r.print(&c, nil)
	case *ast.GenDecl:
		c := *d
		c.Doc = nil
		return r.print(&c, comments)
	}
	return r.print(d, comments)
}

func (r *refRenderer) print(n ast.Node, comments []*ast.CommentGroup) string {
	var b bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 4}
	var err error
	if comments != nil {
		err = cfg.Fprint(&b, r.fset, &printer.CommentedNode{Node: n, Comments: comments})
	} else {
		err = cfg.Fprint(&b, r.fset, n)
	}
	if err != nil {
		return "// " + err.Error()
	}
	return `<pre class="decl"><code class="language-go">` + template.HTMLEscapeString(b.String()) + "</code></pre>\n"
}

func (r *refRenderer) source(pos token.Pos) string {
	p := r.fset.Position(pos)
	file := filepath.Base(p.Filename)
	if r.dir != "." {
		file = r.dir + "/" + file
	}
	return fmt.Sprintf(`<a class="source" href="%s/blob/%s/%s#L%d">source</a>`, r.s.cfg.GitHub, r.s.cfg.Ref, file, p.Line)
}

func (r *refRenderer) symbol(level int, id, text string, d ast.Decl, docText string) {
	fmt.Fprintf(&r.b, "<div class=\"symbol\" id=\"%s\">\n", id)
	fmt.Fprintf(&r.b, "<h%d><a class=\"anchor\" href=\"#%s\" aria-hidden=\"true\">#</a>%s %s</h%d>\n",
		level, id, template.HTMLEscapeString(text), r.source(d.Pos()), level)
	r.toc = append(r.toc, Heading{Level: level, ID: id, Text: text})
	r.b.WriteString(r.decl(d))
	r.b.WriteString(r.docHTML(docText))
	r.b.WriteString("</div>\n")
}

func (r *refRenderer) values(vs []*doc.Value) {
	for _, v := range vs {
		r.b.WriteString(r.decl(v.Decl))
		r.b.WriteString(r.docHTML(v.Doc))
	}
}

func funcText(f *doc.Func) string {
	if f.Recv != "" {
		return "func (" + f.Recv + ") " + f.Name
	}
	return "func " + f.Name
}

func funcID(f *doc.Func) string {
	if f.Recv != "" {
		return strings.TrimPrefix(f.Recv, "*") + "." + f.Name
	}
	return f.Name
}

func (r *refRenderer) render(rp refPackage) string {
	pkg := r.pkg
	fmt.Fprintf(&r.b, "<h1 id=\"top\">%s</h1>\n", template.HTMLEscapeString(rp.title))
	fmt.Fprintf(&r.b, "<pre class=\"import\"><code class=\"language-go\">import %q</code></pre>\n", rp.importPath)
	fmt.Fprintf(&r.b, "<p class=\"ref-links\">Generated from the source of this version. Also on <a href=\"https://pkg.go.dev/%s\">pkg.go.dev</a>.</p>\n", rp.importPath)

	r.heading(2, "pkg-overview", "Overview")
	r.b.WriteString(r.docHTML(pkg.Doc))

	// Index.
	r.heading(2, "pkg-index", "Index")
	r.b.WriteString("<ul class=\"ref-index\">\n")
	item := func(id, text string) {
		fmt.Fprintf(&r.b, "<li><a href=\"#%s\"><code>%s</code></a></li>\n", id, template.HTMLEscapeString(text))
	}
	if len(pkg.Consts) > 0 {
		item("pkg-constants", "Constants")
	}
	if len(pkg.Vars) > 0 {
		item("pkg-variables", "Variables")
	}
	for _, f := range pkg.Funcs {
		item(funcID(f), funcText(f))
	}
	for _, t := range pkg.Types {
		item(t.Name, "type "+t.Name)
		r.b.WriteString("<ul>\n")
		for _, f := range t.Funcs {
			item(funcID(f), funcText(f))
		}
		for _, f := range t.Methods {
			item(funcID(f), funcText(f))
		}
		r.b.WriteString("</ul>\n")
	}
	r.b.WriteString("</ul>\n")

	if len(pkg.Consts) > 0 {
		r.heading(2, "pkg-constants", "Constants")
		r.values(pkg.Consts)
	}
	if len(pkg.Vars) > 0 {
		r.heading(2, "pkg-variables", "Variables")
		r.values(pkg.Vars)
	}
	if len(pkg.Funcs) > 0 {
		r.heading(2, "pkg-functions", "Functions")
		for _, f := range pkg.Funcs {
			r.symbol(3, funcID(f), funcText(f), f.Decl, f.Doc)
		}
	}
	if len(pkg.Types) > 0 {
		r.heading(2, "pkg-types", "Types")
		for _, t := range pkg.Types {
			r.symbol(3, t.Name, "type "+t.Name, t.Decl, t.Doc)
			r.aliasTarget(t)
			r.values(t.Consts)
			r.values(t.Vars)
			for _, f := range t.Funcs {
				r.symbol(4, funcID(f), funcText(f), f.Decl, f.Doc)
			}
			for _, f := range t.Methods {
				r.symbol(4, funcID(f), funcText(f), f.Decl, f.Doc)
			}
		}
	}
	return r.b.String()
}

// cliFlag is a flag of a subcommand of the pego command, found in its source.
type cliFlag struct {
	name, kind, def, usage string
}

// renderCLI documents the pego command from its source: the package comment, the usage message and
// the flags each subcommand defines with flag.NewFlagSet.
func (s *Site) renderCLI(p *Page) (string, []Heading, error) {
	fset, files, pkg, err := loadPackage(s.cfg.Repo, "cmd/pego", modulePath+"/cmd/pego")
	if err != nil {
		return "", nil, err
	}
	p.Description = "Parse, format, convert, compile and generate parsers from the command line."
	var usage string
	type command struct {
		name  string
		flags []cliFlag
	}
	var commands []command
	// Parse the source again: the files that go/doc has seen lack unexported declarations and bodies.
	sfset, sfiles, err := parseDir(s.cfg.Repo, "cmd/pego")
	if err != nil {
		return "", nil, err
	}
	for _, f := range sfiles {
		ast.Inspect(f, func(n ast.Node) bool {
			if vs, ok := n.(*ast.ValueSpec); ok {
				for i, name := range vs.Names {
					if name.Name == "usage" && i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							usage, _ = strconv.Unquote(lit.Value)
						}
					}
				}
			}
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				return true
			}
			var cmd command
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				args := make([]string, len(call.Args))
				for i, a := range call.Args {
					var b bytes.Buffer
					_ = printer.Fprint(&b, sfset, a)
					args[i] = b.String()
					if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						args[i], _ = strconv.Unquote(lit.Value)
					}
				}
				switch {
				case sel.Sel.Name == "NewFlagSet" && len(args) > 0:
					cmd.name = args[0]
				case (sel.Sel.Name == "String" || sel.Sel.Name == "Bool" || sel.Sel.Name == "Int") && len(args) == 3:
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "fs" {
						cmd.flags = append(cmd.flags, cliFlag{name: args[0], kind: strings.ToLower(sel.Sel.Name), def: args[1], usage: args[2]})
					}
				}
				return true
			})
			if cmd.name != "" {
				commands = append(commands, cmd)
			}
			return false
		})
	}
	if usage == "" || len(commands) == 0 {
		return "", nil, fmt.Errorf("found no usage message or flag sets in cmd/pego")
	}
	r := &refRenderer{s: s, page: p, fset: fset, files: files, pkg: pkg, dir: "cmd/pego"}
	r.b.WriteString("<h1 id=\"top\">The pego Command</h1>\n")
	r.b.WriteString(`<pre><code class="language-bash">go install github.com/ornew/pego/cmd/pego@latest</code></pre>` + "\n")
	r.heading(2, "overview", "Overview")
	r.b.WriteString(r.docHTML(pkg.Doc))
	r.heading(2, "usage", "Usage")
	r.b.WriteString("<pre><code class=\"language-text\">" + template.HTMLEscapeString(usage) + "</code></pre>\n")
	r.heading(2, "flags", "Flags")
	for _, c := range commands {
		r.heading(3, "cmd-"+c.name, "pego "+c.name)
		r.b.WriteString("<table><thead><tr><th>Flag</th><th>Default</th><th>Description</th></tr></thead><tbody>\n")
		for _, f := range c.flags {
			def := f.def
			if f.kind == "string" {
				def = strconv.Quote(def)
			}
			if f.kind == "bool" {
				fmt.Fprintf(&r.b, "<tr><td><code>-%s</code></td><td><code>%s</code></td><td>%s</td></tr>\n",
					f.name, template.HTMLEscapeString(def), template.HTMLEscapeString(f.usage))
			} else {
				fmt.Fprintf(&r.b, "<tr><td><code>-%s %s</code></td><td><code>%s</code></td><td>%s</td></tr>\n",
					f.name, f.kind, template.HTMLEscapeString(def), template.HTMLEscapeString(f.usage))
			}
		}
		r.b.WriteString("</tbody></table>\n")
	}
	return r.b.String(), r.toc, nil
}

// aliasTarget returns the name of the type of the internal engine package that t is an alias of, if
// any (as in type Node = engine.Node).
func aliasTarget(t *doc.Type) string {
	for _, spec := range t.Decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name.Name != t.Name || !ts.Assign.IsValid() {
			continue
		}
		if sel, ok := ts.Type.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "engine" {
				return sel.Sel.Name
			}
		}
	}
	return ""
}

// aliasTarget documents the definition, fields and methods of the internal type that t is an alias
// of. The internal package is not importable, so without this the reference (like pkg.go.dev) would
// not show them.
func (r *refRenderer) aliasTarget(t *doc.Type) {
	name := aliasTarget(t)
	if name == "" || r.internal == nil {
		return
	}
	in := r.internal
	for _, it := range in.pkg.Types {
		if it.Name != name {
			continue
		}
		in.b.Reset()
		in.b.WriteString("<div class=\"alias-target\">\n")
		fmt.Fprintf(&in.b, "<p class=\"alias-note\">The type it stands for is defined in an internal package as follows. %s</p>\n",
			in.source(it.Decl.Pos()))
		in.b.WriteString(in.decl(it.Decl))
		if it.Doc != t.Doc {
			in.b.WriteString(in.docHTML(it.Doc))
		}
		for _, f := range it.Methods {
			if !ast.IsExported(f.Name) {
				continue
			}
			id := t.Name + "." + f.Name
			in.b.WriteString("<div class=\"symbol\" id=\"" + id + "\">\n")
			fmt.Fprintf(&in.b, "<h4><a class=\"anchor\" href=\"#%s\" aria-hidden=\"true\">#</a>func (%s) %s %s</h4>\n",
				id, template.HTMLEscapeString(f.Recv), f.Name, in.source(f.Decl.Pos()))
			r.toc = append(r.toc, Heading{Level: 4, ID: id, Text: "func (" + f.Recv + ") " + f.Name})
			in.b.WriteString(in.decl(f.Decl))
			in.b.WriteString(in.docHTML(f.Doc))
			in.b.WriteString("</div>\n")
		}
		in.b.WriteString("</div>\n")
		r.b.WriteString(in.b.String())
		return
	}
}
