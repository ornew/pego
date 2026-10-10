package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Config configures a build.
type Config struct {
	Repo         string // root of the repository
	Out          string // output directory
	Wasm         bool   // build the playground's WebAssembly binary
	Check        bool   // fail on broken internal links
	GitHub       string // repository URL, for links to files that are not pages of the site
	Ref          string // branch or tag for those links
	PreviousSite string // published site whose current WASM/runtime pair should be retained
	DeployURL    string // immutable URL of this deploy, recorded in the asset manifest
}

// Stats summarizes a build.
type Stats struct {
	Pages int
	Files int
	Bytes int64
}

// Heading is a heading of a page, for the table of contents and the search index.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// Page is a page of the site.
type Page struct {
	Src         string // repository path of the Markdown source, if any
	URL         string // path of the page's directory relative to the site root: "" or "a/b/"
	Title       string
	Nav         string // short title for the navigation
	Section     string
	Index       bool   // the index of its section, which the navigation calls "Introduction"
	Kind        string // landing, doc or playground
	Description string
	Body        template.HTML
	TOC         []Heading
	Text        string   // plain text for the search index
	Scripts     []string // module scripts, relative to the site root
	Styles      []string // style sheets, relative to the site root
	SourceURL   string   // where the source of the page can be seen
}

// Section is a group of pages in the navigation.
type Section struct {
	Name        string
	Collapsible bool // the navigation folds the pages under the name, open only if the current page is one of them
	Pages       []*Page
}

// Contains reports whether the section lists p.
func (s *Section) Contains(p *Page) bool { return slices.Contains(s.Pages, p) }

// Site is a site being built.
type Site struct {
	cfg      Config
	pages    []*Page
	bySrc    map[string]*Page // by the repository path of the source
	byDir    map[string]*Page // by the repository directory the page is the index of
	sections []*Section
	problems []string
	wasm     string // site path of the playground's WebAssembly binary
}

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// marker marks an output directory written by Build, which a later build may replace.
const marker = ".pego-site"

// docSection lists the Markdown files of a section of the documentation.
type docSection struct {
	name  string
	globs []string
	// index, if set, is a file whose links give the order of the pages; pages it does not link to
	// follow in alphabetical order.
	index string
	// first lists files that come first, in this order.
	first []string
	// collapsible folds the section in the navigation under its name. It is closed unless the current
	// page belongs to the section.
	collapsible bool
}

var docSections = []docSection{
	{name: "Tutorial", globs: []string{"docs/tutorial/*.md"}},
	{name: "Guides", globs: []string{"docs/guide/*.md"}, index: "docs/guide/README.md"},
	{name: "Cookbook", globs: []string{"docs/cookbook/*.md"}, index: "docs/cookbook/README.md"},
	{name: "Specification", globs: []string{"spec/*.md"}, index: "spec/README.md"},
	{name: "Parsers", globs: []string{"parsers/README.md", "parsers/*/README.md"}, index: "parsers/README.md"},
	{name: "Project", globs: []string{"examples/README.md", "docs/*.md", "docs/design/*.md", "docs/optimizations/*.md"}, first: []string{
		"examples/README.md", "docs/development.md", "docs/benchmarks.md", "docs/performance.md",
		"docs/optimizations/README.md",
	}},
}

// Build builds the site.
func Build(cfg Config) (Stats, error) {
	s := &Site{cfg: cfg, bySrc: map[string]*Page{}, byDir: map[string]*Page{}}
	if cfg.PreviousSite != "" && !cfg.Wasm {
		return Stats{}, errors.New("-previous-site requires -wasm")
	}
	if cfg.DeployURL != "" {
		if _, err := siteURL(cfg.DeployURL); err != nil {
			return Stats{}, err
		}
	}
	previous, err := readPreviousAssets(cfg.PreviousSite)
	if err != nil {
		return Stats{}, fmt.Errorf("retaining previous playground: %w", err)
	}
	if err := prepareOut(cfg.Out); err != nil {
		return Stats{}, err
	}
	if err := s.collect(); err != nil {
		return Stats{}, err
	}
	if err := s.renderAll(); err != nil {
		return Stats{}, err
	}
	// Build the WebAssembly binary first: the pages refer to it by its content-hashed name.
	s.wasm = "playground/pego.wasm" // missing without -wasm
	if cfg.Wasm {
		name, err := buildWasm(cfg.Repo, filepath.Join(cfg.Out, "playground"))
		if err != nil {
			return Stats{}, err
		}
		s.wasm = "playground/" + name
		if err := publishAssets(filepath.Join(cfg.Out, "playground"), name, cfg.DeployURL, previous); err != nil {
			return Stats{}, err
		}
	}
	if err := s.writeAll(); err != nil {
		return Stats{}, err
	}
	if cfg.Check {
		s.problems = append(s.problems, checkLinks(cfg.Out)...)
	}
	if len(s.problems) > 0 {
		return Stats{}, fmt.Errorf("site has %d problems:\n  %s", len(s.problems), strings.Join(s.problems, "\n  "))
	}
	return stats(cfg.Out, len(s.pages))
}

func prepareOut(out string) error {
	entries, err := os.ReadDir(out)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case len(entries) > 0:
		if _, err := os.Stat(filepath.Join(out, marker)); err != nil {
			return fmt.Errorf("%s is not empty and was not written by a previous build; refusing to replace it", out)
		}
		if err := os.RemoveAll(out); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, marker), nil, 0o644)
}

func (s *Site) add(p *Page) {
	s.pages = append(s.pages, p)
	if p.Src != "" {
		s.bySrc[p.Src] = p
		if path.Base(p.Src) == "README.md" {
			s.byDir[dirOf(p.Src)] = p
		}
	}
}

// dirOf returns the directory of a repository path, with "" for the root.
func dirOf(p string) string {
	if d := path.Dir(p); d != "." {
		return d
	}
	return ""
}

// urlOf returns the site URL of the page for a Markdown file: its path without the extension, or its
// directory for a README.
func urlOf(src string) string {
	if path.Base(src) == "README.md" {
		if d := dirOf(src); d != "" {
			return d + "/"
		}
		return ""
	}
	return strings.TrimSuffix(src, ".md") + "/"
}

// collect creates the pages, so that links can be resolved while they are rendered.
func (s *Site) collect() error {
	s.add(&Page{Src: "README.md", URL: "", Kind: "landing", Title: "PEGO", Nav: "Home"})
	s.add(&Page{URL: "playground/", Kind: "playground", Title: "Playground", Nav: "Playground",
		Description: "Write a PEGO grammar and parse input with it in your browser.",
		Scripts:     []string{"playground/app.js"}, Styles: []string{"playground/playground.css"}})

	for _, ds := range docSections {
		files, err := s.sectionFiles(ds)
		if err != nil {
			return err
		}
		sec := &Section{Name: ds.name, Collapsible: ds.collapsible}
		if ds.name == "Project" {
			idx := &Page{URL: "docs/design/", Kind: "doc", Title: "Design Records", Nav: "Design Records", Section: sec.Name}
			s.add(idx)
			s.byDir["docs/design"] = idx
			sec.Pages = append(sec.Pages, idx)
		}
		for _, f := range files {
			if s.bySrc[f] != nil {
				continue // listed by an earlier section
			}
			p := &Page{Src: f, URL: urlOf(f), Kind: "doc", Section: sec.Name, Index: f == ds.index,
				SourceURL: s.cfg.GitHub + "/blob/" + s.cfg.Ref + "/" + f}
			if f == "docs/optimizations/README.md" {
				p.Nav = "Optimizations"
			}
			s.add(p)
			if ds.name != "Project" || !isProjectDetail(f) {
				sec.Pages = append(sec.Pages, p)
			}
		}
		s.sections = append(s.sections, sec)
		if ds.name == "Specification" {
			s.sections = append(s.sections, s.referencePages())
		}
	}
	return s.checkCoverage()
}

// isProjectDetail reports whether f is an individual design or optimization page. These pages
// remain rendered and searchable, but their indexes are the only entries in Project navigation.
func isProjectDetail(f string) bool {
	return (strings.HasPrefix(f, "docs/design/") && f != "docs/design/README.md") ||
		(strings.HasPrefix(f, "docs/optimizations/") && f != "docs/optimizations/README.md")
}

func (s *Site) sectionFiles(ds docSection) ([]string, error) {
	var files []string
	for _, g := range ds.globs {
		m, err := filepath.Glob(filepath.Join(s.cfg.Repo, filepath.FromSlash(g)))
		if err != nil {
			return nil, err
		}
		for _, f := range m {
			rel, err := filepath.Rel(s.cfg.Repo, f)
			if err != nil {
				return nil, err
			}
			files = append(files, filepath.ToSlash(rel))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("section %s: no files match %v", ds.name, ds.globs)
	}
	rank := map[string]int{}
	for i, f := range ds.first {
		rank[f] = i + 1
	}
	if ds.index != "" {
		src, err := os.ReadFile(filepath.Join(s.cfg.Repo, filepath.FromSlash(ds.index)))
		if err != nil {
			return nil, err
		}
		rank[ds.index] = 1
		for _, m := range mdLinkRe.FindAllStringSubmatch(string(src), -1) {
			f := path.Join(path.Dir(ds.index), m[1])
			if _, ok := rank[f]; !ok {
				rank[f] = len(rank) + 1
			}
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		ri, rj := rank[files[i]], rank[files[j]]
		switch {
		case ri > 0 && rj > 0:
			return ri < rj
		case ri > 0 || rj > 0:
			return ri > 0
		}
		return files[i] < files[j]
	})
	return files, nil
}

var mdLinkRe = regexp.MustCompile(`\]\(([^)#\s]+\.md)(?:#[^)]*)?\)`)

// checkCoverage reports Markdown files of the documentation that no section includes, so that a new
// directory of documents is not silently left out of the site.
func (s *Site) checkCoverage() error {
	for _, dir := range []string{"docs", "spec"} {
		err := filepath.WalkDir(filepath.Join(s.cfg.Repo, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
				return err
			}
			rel, _ := filepath.Rel(s.cfg.Repo, p)
			if s.bySrc[filepath.ToSlash(rel)] == nil {
				s.problems = append(s.problems, fmt.Sprintf("%s is not part of any section of the site (see docSections in site/build.go)", rel))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// resolveLink rewrites a link in the Markdown source of p: a link to a Markdown file or a directory
// with a page becomes a relative link to that page, and a link to another file of the repository
// becomes a link to the file on GitHub. Links to missing files are reported.
func (s *Site) resolveLink(p *Page, dest string) string {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "//") || schemeRe.MatchString(dest) {
		return dest
	}
	target, frag, hasFrag := strings.Cut(dest, "#")
	if hasFrag {
		frag = "#" + frag
	}
	// Paths may be percent-encoded ("my%20file.md"), and a path starting with "/" is relative to the
	// root of the repository, as on GitHub.
	if t, err := url.PathUnescape(target); err == nil {
		target = t
	}
	base := dirOf(p.Src)
	if strings.HasPrefix(target, "/") {
		base, target = "", "."+target
	}
	rp := path.Clean(path.Join(base, target))
	if rp == ".." || strings.HasPrefix(rp, "../") {
		s.problems = append(s.problems, fmt.Sprintf("%s: link %s leaves the repository", p.Src, dest))
		return dest
	}
	if rp == "." {
		rp = ""
	}
	if q := s.bySrc[rp]; q != nil {
		return relURL(p.URL, q.URL) + frag
	}
	if q := s.byDir[rp]; q != nil {
		return relURL(p.URL, q.URL) + frag
	}
	fi, err := os.Stat(filepath.Join(s.cfg.Repo, filepath.FromSlash(rp)))
	if err != nil {
		s.problems = append(s.problems, fmt.Sprintf("%s: link to missing file %s", p.Src, dest))
		return dest
	}
	kind := "blob"
	if fi.IsDir() {
		kind = "tree"
	}
	return s.cfg.GitHub + "/" + kind + "/" + s.cfg.Ref + "/" + (&url.URL{Path: rp}).EscapedPath() + frag
}

// relURL returns the relative URL from the page at from to the page at to (both site paths of
// directories).
func relURL(from, to string) string {
	r := strings.Repeat("../", strings.Count(from, "/")) + to
	if r == "" {
		return "./"
	}
	return r
}

var titleNumberRe = regexp.MustCompile(`^(\d{3})[.:]\s+`)
var optimizationTitleNumberRe = regexp.MustCompile(`^(\d+)[.:]\s+`)

// navTitle shortens a page title for the navigation. The index of a section is called "Introduction";
// other pages keep their own titles, even when their file is a README.md.
func navTitle(p *Page) string {
	if p.Index {
		return "Introduction"
	}
	t := p.Title
	if m := titleNumberRe.FindStringSubmatch(t); m != nil {
		return m[1] + " " + t[len(m[0]):]
	}
	if before, _, ok := strings.Cut(t, ": "); ok {
		t = before
	}
	return t
}

func (s *Site) renderAll() error {
	for _, p := range s.pages {
		if p.Kind != "doc" || p.Src == "" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(s.cfg.Repo, filepath.FromSlash(p.Src)))
		if err != nil {
			return err
		}
		m := s.parseMarkdown(p, src)
		body := m.render(nil)
		p.Title = m.title()
		if p.Title == "" {
			p.Title = strings.TrimSuffix(path.Base(p.Src), ".md")
		}
		if p.Nav == "" {
			p.Nav = navTitle(p)
		}
		p.TOC = m.toc()
		p.Body = template.HTML(body)
		p.Text = plainText(body)
	}
	s.addLegacyOptimizationAnchors()
	if err := s.renderDesignIndex(); err != nil {
		return err
	}
	if err := s.renderReference(); err != nil {
		return err
	}
	if err := s.renderLanding(); err != nil {
		return err
	}
	return s.renderPlayground()
}

// addLegacyOptimizationAnchors keeps existing performance-page URLs working. The old fragment is
// derived from each numbered optimization page's heading, so the detail page remains the source
// of its title and the performance page does not duplicate the catalog.
func (s *Site) addLegacyOptimizationAnchors() {
	perf := s.bySrc["docs/performance.md"]
	if perf == nil {
		return
	}
	var b strings.Builder
	for _, p := range s.pages {
		if !strings.HasPrefix(p.Src, "docs/optimizations/") || p.Src == "docs/optimizations/README.md" {
			continue
		}
		m := optimizationTitleNumberRe.FindStringSubmatch(p.Title)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		title := strings.TrimPrefix(p.Title, m[0])
		legacyID := fmt.Sprintf("%d-%s", n, slug(title))
		target := relURL(perf.URL, p.URL) + "#" + slug(p.Title)
		fmt.Fprintf(&b, `<a id="%s" data-moved-to="%s"></a>`+"\n", legacyID, template.HTMLEscapeString(target))
	}
	perf.Body = template.HTML(b.String() + string(perf.Body))
}

func (s *Site) renderPlayground() error {
	tmpl, err := template.ParseFS(templateFS, "templates/playground.html")
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, nil); err != nil {
		return err
	}
	s.page("playground/").Body = template.HTML(b.String())
	return nil
}

var (
	statusRe = regexp.MustCompile(`(?m)^- \*\*Status\*\*:\s*(.+)$`)
	dateRe   = regexp.MustCompile(`(?m)^- \*\*Date\*\*:\s*(.+)$`)
)

func (s *Site) renderDesignIndex() error {
	idx := s.byDir["docs/design"]
	var b strings.Builder
	b.WriteString(`<h1 id="design-records">Design Records</h1>
<p>Each record explains a decision in the design of PEGO and the alternatives that were considered.</p>
<table><thead><tr><th>Record</th><th>Status</th><th>Date</th></tr></thead><tbody>
`)
	for _, p := range s.pages {
		if !strings.HasPrefix(p.Src, "docs/design/") || p == idx {
			continue
		}
		src, err := os.ReadFile(filepath.Join(s.cfg.Repo, filepath.FromSlash(p.Src)))
		if err != nil {
			return err
		}
		status, date := "", ""
		if m := statusRe.FindSubmatch(src); m != nil {
			status = string(m[1])
		}
		if m := dateRe.FindSubmatch(src); m != nil {
			date = string(m[1])
		}
		fmt.Fprintf(&b, "<tr><td><a href=\"%s\">%s</a></td><td>%s</td><td>%s</td></tr>\n",
			template.HTMLEscapeString(relURL(idx.URL, p.URL)), template.HTMLEscapeString(p.Title),
			template.HTMLEscapeString(status), template.HTMLEscapeString(date))
	}
	b.WriteString("</tbody></table>\n")
	idx.Body = template.HTML(b.String())
	idx.Text = plainText(b.String())
	return nil
}

// layoutData is the data of the page layout template.
type layoutData struct {
	Site       *Site
	Page       *Page
	Root       string // relative URL of the site root
	Sections   []*Section
	Prev, Next *Page
	GitHub     string
	Wasm       string // relative URL of the playground's WebAssembly binary
}

// URL returns the relative URL of a site path from the page.
func (d layoutData) URL(p string) string { return relURL(d.Page.URL, p) }

func (s *Site) writeAll() error {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return err
	}
	var flat []*Page
	for _, sec := range s.sections {
		flat = append(flat, sec.Pages...)
	}
	for _, p := range s.pages {
		d := layoutData{Site: s, Page: p, Root: relURL(p.URL, ""), Sections: s.sections, GitHub: s.cfg.GitHub, Wasm: relURL(p.URL, s.wasm)}
		if i := slices.Index(flat, p); i >= 0 {
			if i > 0 {
				d.Prev = flat[i-1]
			}
			if i+1 < len(flat) {
				d.Next = flat[i+1]
			}
		}
		var b bytes.Buffer
		if err := tmpl.ExecuteTemplate(&b, "layout.html", d); err != nil {
			return fmt.Errorf("%s: %w", p.URL, err)
		}
		if err := writeFile(filepath.Join(s.cfg.Out, filepath.FromSlash(p.URL), "index.html"), b.Bytes()); err != nil {
			return err
		}
	}
	if err := s.writeSearchIndex(); err != nil {
		return err
	}
	if err := s.writeExamples(); err != nil {
		return err
	}
	return fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(s.cfg.Out, filepath.FromSlash(strings.TrimPrefix(p, "static/"))), data)
	})
}

func writeFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}

// searchEntry is a page in the search index. The keys are short because the index is downloaded.
type searchEntry struct {
	Title    string      `json:"t"`
	URL      string      `json:"u"`
	Section  string      `json:"s"`
	Headings [][2]string `json:"h"` // [id, text]
	Text     string      `json:"x"`
}

func (s *Site) writeSearchIndex() error {
	var entries []searchEntry
	for _, p := range s.pages {
		if p.Kind == "landing" || p.Kind == "playground" {
			continue
		}
		e := searchEntry{Title: p.Title, URL: p.URL, Section: p.Section, Text: p.Text, Headings: [][2]string{}}
		for _, h := range p.TOC {
			e.Headings = append(e.Headings, [2]string{h.ID, h.Text})
		}
		entries = append(entries, e)
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.cfg.Out, "search-index.json"), b)
}

// example is an example of the playground, read from playground/examples.txt.
type example struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	GrammarPath string `json:"grammarPath"`
	InputPath   string `json:"inputPath"`
	Grammar     string `json:"grammar"`
	Input       string `json:"input"`
}

func readExamples(repo string) ([]example, error) {
	data, err := os.ReadFile(filepath.Join(repo, "playground", "examples.txt"))
	if err != nil {
		return nil, err
	}
	var exs []example
	for l := range strings.SplitSeq(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 3 {
			return nil, fmt.Errorf("playground/examples.txt: bad line %q", l)
		}
		ex := example{Name: f[0], GrammarPath: f[1], InputPath: f[2], Description: strings.Join(f[3:], " ")}
		g, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(ex.GrammarPath)))
		if err != nil {
			return nil, err
		}
		in, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(ex.InputPath)))
		if err != nil {
			return nil, err
		}
		ex.Grammar, ex.Input = string(g), string(in)
		exs = append(exs, ex)
	}
	return exs, nil
}

func (s *Site) writeExamples() error {
	exs, err := readExamples(s.cfg.Repo)
	if err != nil {
		return err
	}
	b, err := json.Marshal(exs)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.cfg.Out, "playground", "examples.json"), b)
}

// buildWasm builds the playground's WebAssembly binary into dir/wasm, named after a hash of its
// content so that it can be cached forever (see netlify.toml), and copies the Go runtime support
// (wasm_exec.js) into dir. It returns the path of the binary relative to dir.
func buildWasm(repo, dir string) (string, error) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("building the playground needs the go command: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(abs, "pego.wasm.tmp")
	cmd := exec.Command(goTool, "build", "-trimpath", "-ldflags=-s -w", "-o", tmp, "./playground")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building pego.wasm: %v\n%s", err, out)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := "wasm/pego-" + hex.EncodeToString(sum[:])[:16] + ".wasm"
	if err := writeFile(filepath.Join(abs, filepath.FromSlash(name)), data); err != nil {
		return "", err
	}
	if err := os.Remove(tmp); err != nil {
		return "", err
	}
	out, err := exec.Command(goTool, "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOROOT: %w", err)
	}
	support, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		return "", err
	}
	return name, writeFile(filepath.Join(abs, "wasm_exec.js"), support)
}

func stats(out string, pages int) (Stats, error) {
	st := Stats{Pages: pages}
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st.Files++
		st.Bytes += info.Size()
		return nil
	})
	return st, err
}
