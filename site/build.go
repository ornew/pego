package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
	"strconv"
	"strings"

	"github.com/ornew/nanosite/publish"
	"github.com/ornew/nanosite/render"
)

// Config configures a build.
type Config struct {
	Exclude      []string
	Repo         string // root of the repository
	Out          string // output directory
	Wasm         bool   // build the playground's WebAssembly binary
	WasmMode     string // go (default) or dual (Go and TinyGo)
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
type Heading = render.Heading

// Page is a page of the site.
type Page struct {
	Src         string // repository path of the Markdown source, if any
	URL         string // path of the page's directory relative to the site root: "" or "a/b/"
	Title       string
	Nav         string        // short title for the navigation
	NavHTML     template.HTML // formatted labels supplied by generated pages only
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

// NavLabel keeps ordinary page labels escaped while allowing generated labels
// to mark package names as code without changing plain search/page titles.
func (p *Page) NavLabel() template.HTML {
	if p.NavHTML != "" {
		return p.NavHTML
	}
	return template.HTML(template.HTMLEscapeString(p.Nav))
}

// Section is a group of pages in the navigation.
type Section struct {
	Name        string
	Collapsible bool // the navigation folds the pages under the name, open only if the current page is one of them
	Pages       []*Page
	// IndexPages are the section's configured index page, shown before the nested groups;
	// NavPages are the other pages shown directly, after them; Children preserves configured
	// nested groups. Pages includes descendants for current-page checks.
	IndexPages []*Page
	NavPages   []*Page
	Children   []*Section
}

// Contains reports whether the section lists p.
func (s *Section) Contains(p *Page) bool { return slices.Contains(s.Pages, p) }

// Site is a site being built.
type Site struct {
	cfg        Config
	pages      []*Page
	bySrc      map[string]*Page // by the repository path of the source
	byDir      map[string]*Page // by the repository directory the page is the index of
	sections   []*Section
	problems   []string
	wasm       string // site path of the playground's WebAssembly binary
	tinygoWasm string // optional site path of the TinyGo binary
}

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// marker marks an output directory written by Build, which a later build may replace.
const marker = ".pego-site"

// docSection lists the Markdown files of a section of the documentation.
func prepareOut(out string) error {
	return publish.Run(publish.Options{Repo: ".", Out: out, Marker: marker}, func(string) error { return nil })
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
	return strings.TrimSuffix(render.MarkdownOutput(src, true, []string{"README.md"}), "index.html")
}

// collect creates the pages, so that links can be resolved while they are rendered.
// isOptimizationDetail reports whether f is an individual optimization page. Such pages remain
// rendered and searchable, but only their index appears in Project navigation.
func isOptimizationDetail(f string) bool {
	return strings.HasPrefix(f, "docs/optimizations/") && f != "docs/optimizations/README.md"
}

var designRecordFileRe = regexp.MustCompile(`^docs/design/[0-9]{3}-.+\.md$`)

func isDesignRecord(f string) bool { return designRecordFileRe.MatchString(f) }

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

// layoutData is the data of the page layout template.
type layoutData struct {
	Site       *Site
	Page       *Page
	Root       string // relative URL of the site root
	Sections   []*Section
	Prev, Next *Page
	GitHub     string
	Wasm       string // relative URL of the playground's WebAssembly binary
	TinyGoWasm string // relative URL of the optional TinyGo binary
}

// URL returns the relative URL of a site path from the page.
func (d layoutData) URL(p string) string { return relURL(d.Page.URL, p) }

func writeFile(name string, data []byte) error { return publish.WriteFile(name, data) }

// searchEntry is a page in the search index. The keys are short because the index is downloaded.
type searchEntry struct {
	Title    string      `json:"t"`
	URL      string      `json:"u"`
	Section  string      `json:"s"`
	Headings [][2]string `json:"h"` // [id, text]
	Text     string      `json:"x"`
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

type wasmBuildSpec struct {
	command    string
	args       []string
	rootVar    string
	runtimeRel string
}

func selectWasmBuild(toolchain, output string) (wasmBuildSpec, error) {
	if toolchain == "" {
		toolchain = "go"
	}
	switch toolchain {
	case "go":
		return wasmBuildSpec{"go", []string{"build", "-trimpath", "-ldflags=-s -w", "-o", output, "./playground"}, "GOROOT", "lib/wasm/wasm_exec.js"}, nil
	case "tinygo":
		return wasmBuildSpec{"tinygo", []string{"build", "-target=wasm", "-no-debug", "-o", output, "./playground"}, "TINYGOROOT", "targets/wasm_exec.js"}, nil
	default:
		return wasmBuildSpec{}, fmt.Errorf("unsupported WASM toolchain %q (want go or tinygo)", toolchain)
	}
}

// buildWasm builds the playground's WebAssembly binary into dir/wasm, named after a hash of its
// content so that it can be cached forever (see netlify.toml), and copies the matching runtime
// support (wasm_exec.js) into dir. It returns the path of the binary relative to dir.
func buildWasm(repo, dir, toolchain string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(abs, "pego.wasm.tmp")
	spec, err := selectWasmBuild(toolchain, tmp)
	if err != nil {
		return "", err
	}
	compiler, err := exec.LookPath(spec.command)
	if err != nil {
		return "", fmt.Errorf("building the playground needs the %s command: %w", spec.command, err)
	}
	cmd := exec.Command(compiler, spec.args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building pego.wasm with %s: %v\n%s", spec.command, err, out)
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
	out, err := exec.Command(compiler, "env", spec.rootVar).Output()
	if err != nil {
		return "", fmt.Errorf("%s env %s: %w", spec.command, spec.rootVar, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("%s env %s returned an empty path", spec.command, spec.rootVar)
	}
	runtimePath := filepath.Join(root, filepath.FromSlash(spec.runtimeRel))
	support, err := os.ReadFile(runtimePath)
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
