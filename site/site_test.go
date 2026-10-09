package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestBuild builds the whole site, including the playground's WebAssembly binary, and checks that
// every document of the repository became a page, that the generated pages are complete, and that
// no internal link is broken (Build fails on broken links).
func TestBuild(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not available")
	}
	out := filepath.Join(t.TempDir(), "dist")
	cfg := Config{Repo: "..", Out: out, Wasm: !testing.Short(), Check: true, GitHub: "https://github.com/ornew/pego", Ref: "main"}
	st, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pages, %d files, %d bytes", st.Pages, st.Files, st.Bytes)

	// Every Markdown document has a page.
	var docs []string
	for _, dir := range []string{"docs", "spec"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Ext(p) == ".md" {
				rel, _ := filepath.Rel("..", p)
				docs = append(docs, filepath.ToSlash(rel))
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	docs = append(docs, "README.md", "examples/README.md")
	if len(docs) < 30 {
		t.Fatalf("found only %d documents", len(docs))
	}
	for _, d := range docs {
		page := filepath.Join(out, filepath.FromSlash(urlOf(d)), "index.html")
		data, err := os.ReadFile(page)
		if err != nil {
			t.Errorf("%s: no page: %v", d, err)
			continue
		}
		if d != "README.md" && !strings.Contains(string(data), `<article class="prose">`) {
			t.Errorf("%s: %s is not a document page", d, page)
		}
	}

	// No page needs 'unsafe-inline' in the Content-Security-Policy of netlify.toml.
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || filepath.Ext(p) != ".html" {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if m := inlineRe.FindString(string(data)); m != "" {
			t.Errorf("%s has inline style or script: %s", p, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Generated pages.
	for page, wants := range map[string][]string{
		"index.html": {
			"Grammar In. Bulletproof Parser Out.", `class="feature"`, `id="live"`, "pego parse -g calc.pego",
			`href="docs/tutorial/getting-started/"`, `id="install"`,
		},
		"reference/pego/index.html": {
			`id="CompileSource"`, `id="Parser.Parse"`, "type Node struct", `id="Node.Clone"`, "pkg.go.dev",
		},
		"reference/grammar/index.html": {`id="Format"`, `id="Grammar.Rules"`, "type RuleDef struct"},
		"reference/cli/index.html":     {`id="cmd-parse"`, "<code>-g string</code>", "-backend string", `id="commands"`},
		"docs/design/index.html":       {"012. Typed Values in Generated Parsers", "Implemented"},
		"playground/index.html":        {`id="pg-grammar"`, `src="../playground/app.js"`, `href="../playground/playground.css"`},
		"docs/guide/runtime/index.html": {
			`id="compiled-grammars-pegoc"`, `href="../../../spec/"`, `<pre><code class="language-pego">`,
		},
	} {
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(page)))
		if err != nil {
			t.Error(err)
			continue
		}
		for _, w := range wants {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s does not contain %q", page, w)
			}
		}
	}

	// The reference of the pego command has a section for every command, with every flag.
	cli, err := os.ReadFile(filepath.Join(out, "reference", "cli", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	sections := regexp.MustCompile(`id="cmd-([^"]+)"`).FindAllStringSubmatch(string(cli), -1)
	var names []string
	for _, m := range sections {
		names = append(names, m[1])
	}
	if got, want := strings.Join(names, " "), "parse fmt convert gen compile trace profile explain lint sample lsp"; got != want {
		t.Errorf("command sections = %s, want %s", got, want)
	}
	for cmd, flags := range map[string][]string{
		"parse":   {"-g string", "-check", "-stream", "-f string"},
		"fmt":     {"-w", "-l"},
		"trace":   {"-g string", "-f string", "-max-depth int", "-rule value", "-failures", "-unit string", "-backend string"},
		"profile": {"-sort string", "-n int", "-f string"},
		"explain": {"-n int", "-s string"},
		"lint":    {"-g string", "-f string", "-disable value", "-strict", "-list"},
	} {
		i := strings.Index(string(cli), `id="cmd-`+cmd+`"`)
		section := string(cli)[i:]
		if j := strings.Index(section[1:], `id="cmd-`); j >= 0 {
			section = section[:j+1]
		}
		for _, f := range flags {
			if !strings.Contains(section, "<code>"+f+"</code>") {
				t.Errorf("pego %s: no flag %s", cmd, f)
			}
		}
	}

	// The search index covers every page in the navigation.
	var index []searchEntry
	data, err := os.ReadFile(filepath.Join(out, "search-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	indexed := map[string]bool{}
	for _, e := range index {
		indexed[e.URL] = true
		if e.Title == "" || e.Text == "" {
			t.Errorf("search entry %s has no title or text", e.URL)
		}
	}
	for _, d := range docs {
		if d != "README.md" && !indexed[urlOf(d)] {
			t.Errorf("%s is not in the search index", d)
		}
	}

	// The playground's examples and runtime.
	var exs []example
	data, err = os.ReadFile(filepath.Join(out, "playground", "examples.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &exs); err != nil {
		t.Fatal(err)
	}
	if len(exs) < 6 || exs[0].Grammar == "" || exs[0].Input == "" {
		t.Errorf("examples.json has %d examples", len(exs))
	}
	files := []string{"worker.js", "app.js", "../assets/pego-client.js", "../assets/state.js", "../assets/highlight.js"}
	if cfg.Wasm {
		files = append(files, "wasm_exec.js")
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(out, "playground", filepath.FromSlash(f))); err != nil {
			t.Error(err)
		}
	}

	// Every page names the WebAssembly binary by a name that changes with its content, so that it can
	// be cached forever.
	if cfg.Wasm {
		wasmRe := regexp.MustCompile(`<html [^>]*data-wasm="([^"]+)"`)
		for _, page := range []string{"index.html", "playground/index.html", "docs/guide/runtime/index.html"} {
			data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(page)))
			if err != nil {
				t.Fatal(err)
			}
			m := wasmRe.FindSubmatch(data)
			if m == nil {
				t.Errorf("%s names no WebAssembly binary", page)
				continue
			}
			ref := filepath.Join(out, filepath.FromSlash(filepath.Dir(page)), filepath.FromSlash(string(m[1])))
			if !regexp.MustCompile(`/playground/wasm/pego-[0-9a-f]{16}\.wasm$`).MatchString(filepath.ToSlash(ref)) {
				t.Errorf("%s: %s is not content-hashed", page, m[1])
			}
			wasm, err := os.ReadFile(ref)
			if err != nil {
				t.Error(err)
				continue
			}
			sum := sha256.Sum256(wasm)
			if !strings.Contains(string(m[1]), hex.EncodeToString(sum[:])[:16]) {
				t.Errorf("%s: the name %s does not match the content", page, m[1])
			}
		}
	}
}

// inlineRe matches style attributes, style elements, inline scripts and event handler attributes.
var inlineRe = regexp.MustCompile(`\sstyle="|<style[\s>]|<script>|<script [^>]*>[^<]|\son[a-z]+="`)

// TestJavaScript runs the tests of the site's JavaScript modules in testdata/*_test.mjs with Node. It
// is skipped when node is not installed.
func TestJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tests, err := filepath.Glob(filepath.Join("testdata", "*_test.mjs"))
	if err != nil || len(tests) == 0 {
		t.Fatalf("no JavaScript tests: %v", err)
	}
	out, err := exec.Command(node, append([]string{"--test"}, tests...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestParseUsage(t *testing.T) {
	u, err := parseUsage(`usage: pego <command> [flags]

Commands:

  parse -g <grammar> [-s <rule>]
        [-unit u]
      Parse the input. Without -i,
      it reads standard input.

  fmt [-w]
      Format files.

  gen -pkg <name>
  gen -lang ts
      Generate a parser.

A <grammar> is PEGO source
or JSON.

Run "pego <command> -h".
`)
	if err != nil {
		t.Fatal(err)
	}
	if u.synopsis != "pego <command> [flags]" || len(u.commands) != 3 || len(u.notes) != 2 {
		t.Fatalf("%+v", u)
	}
	p := u.commands[0]
	if p.name != "parse" || p.synopsis != "pego parse -g <grammar> [-s <rule>]\n     [-unit u]" || p.description != "Parse the input. Without -i, it reads standard input." {
		t.Errorf("parse: %+v", p)
	}
	if u.commands[1].name != "fmt" || u.notes[0] != "A <grammar> is PEGO source or JSON." {
		t.Errorf("%+v", u)
	}
	if g := u.commands[2]; g.name != "gen" || g.synopsis != "pego gen -pkg <name>\npego gen -lang ts" || g.description != "Generate a parser." {
		t.Errorf("gen (two forms of one command): %+v", g)
	}

	flags := parseFlagDefaults("Usage of trace:\n  -f string\n    \toutput format (default \"text\")\n  -n int\n    \trows (0 shows all) (default 30)\n" +
		"  -rule value\n    \tonly this rule\n  -w\twrite the result\n  -s string\n    \tstart rule (default: main)\npego: flag: help requested\n")
	want := []cliFlag{
		{"f", "string", `"text"`, "output format"},
		{"n", "int", "30", "rows (0 shows all)"},
		{"rule", "value", "", "only this rule"},
		{"w", "", "", "write the result"},
		{"s", "string", "", "start rule (default: main)"},
	}
	if len(flags) != len(want) {
		t.Fatalf("flags = %+v", flags)
	}
	for i := range want {
		if flags[i] != want[i] {
			t.Errorf("flag %d = %+v, want %+v", i, flags[i], want[i])
		}
	}
}

func TestCheckLinks(t *testing.T) {
	out := t.TempDir()
	write := func(name, content string) {
		if err := writeFile(filepath.Join(out, filepath.FromSlash(name)), []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", `<a href="a/">ok</a> <a href="a/#x">ok</a> <a href="#top">ok</a> <h1 id="top"></h1>
<a href="missing/">missing page</a> <a href="a/#nope">missing anchor</a> <a href="#gone">missing local anchor</a>
<a href="https://example.com/x">external</a> <link href="style.css" rel="stylesheet"> <script src="gone.js"></script>
<a href="../outside/">outside</a> <a href="a/?q=1&amp;r=2">query</a>`)
	write("a/index.html", `<p id="x">x</p> <h2 id="unicode-あい">u</h2> <a href="#unicode-%E3%81%82%E3%81%84">ok</a>
<a href="../a/#unicode-あい">ok</a> <a href="#unicode-%E3%81%82">missing</a>`)
	write("style.css", ``)
	problems := checkLinks(out)
	want := []string{"missing/", "a/#nope", "#gone", "gone.js", "../outside/", "a/?q=1&r=2", "#unicode-%E3%81%82"}
	if len(problems) != len(want) {
		t.Fatalf("got %d problems, want %d:\n%s", len(problems), len(want), strings.Join(problems, "\n"))
	}
	for _, w := range want {
		found := false
		for _, p := range problems {
			found = found || strings.Contains(p, " "+w+" ") || strings.HasSuffix(p, " "+w)
		}
		if !found {
			t.Errorf("no problem reported for %s:\n%s", w, strings.Join(problems, "\n"))
		}
	}
}

func TestResolveLink(t *testing.T) {
	s := &Site{cfg: Config{Repo: "..", GitHub: "https://github.com/ornew/pego", Ref: "main"}, bySrc: map[string]*Page{}, byDir: map[string]*Page{}}
	from := &Page{Src: "docs/guide/runtime.md", URL: "docs/guide/runtime/"}
	s.add(from)
	s.add(&Page{Src: "spec/README.md", URL: "spec/"})
	s.add(&Page{Src: "spec/pratt.md", URL: "spec/pratt/"})
	s.add(&Page{Src: "README.md", URL: ""})
	for _, tc := range []struct{ dest, want string }{
		{"https://example.com/a.md", "https://example.com/a.md"},
		{"#backends", "#backends"},
		{"../../spec/pratt.md#levels", "../../../spec/pratt/#levels"},
		{"../../spec/", "../../../spec/"},
		{"../../spec/README.md", "../../../spec/"},
		{"../../README.md", "../../../"},
		{"../../examples/calculator/calc.pego", "https://github.com/ornew/pego/blob/main/examples/calculator/calc.pego"},
		{"../../examples/calculator/", "https://github.com/ornew/pego/tree/main/examples/calculator"},
		// Links relative to the repository root and percent-encoded paths work on GitHub too.
		{"/spec/pratt.md#levels", "../../../spec/pratt/#levels"},
		{"/examples/calculator/calc.pego", "https://github.com/ornew/pego/blob/main/examples/calculator/calc.pego"},
		{"/", "../../../"},
		{"%2E%2E/%2E%2E/spec/pratt.md", "../../../spec/pratt/"},
		{"../../spec/pratt.md#caf%C3%A9", "../../../spec/pratt/#caf%C3%A9"},
	} {
		if got := s.resolveLink(from, tc.dest); got != tc.want {
			t.Errorf("resolveLink(%q) = %q, want %q", tc.dest, got, tc.want)
		}
	}
	if len(s.problems) != 0 {
		t.Errorf("unexpected problems: %v", s.problems)
	}
	s.resolveLink(from, "missing.md")
	s.resolveLink(from, "../../../outside.md")
	s.resolveLink(from, "/../outside.md")
	if len(s.problems) != 3 {
		t.Errorf("problems = %v, want three", s.problems)
	}
}

// TestNonASCIIAnchors renders headings and links with non-ASCII text, which goldmark percent-encodes
// in link destinations, and checks that the links resolve.
func TestNonASCIIAnchors(t *testing.T) {
	s := &Site{cfg: Config{Repo: "..", GitHub: "https://github.com/ornew/pego", Ref: "main"}, bySrc: map[string]*Page{}, byDir: map[string]*Page{}}
	p := &Page{Src: "docs/x.md", URL: "docs/x/"}
	s.add(p)
	m := s.parseMarkdown(p, []byte("# Title\n\n## Café au lait\n\n## 日本語の見出し\n\n[a](#café-au-lait) [b](#日本語の見出し) [c](#caf%C3%A9-au-lait)\n"))
	out := t.TempDir()
	if err := writeFile(filepath.Join(out, "docs", "x", "index.html"), []byte(m.render(nil))); err != nil {
		t.Fatal(err)
	}
	if problems := checkLinks(out); len(problems) != 0 {
		t.Errorf("problems: %v", problems)
	}
	if len(s.problems) != 0 {
		t.Errorf("site problems: %v", s.problems)
	}
}

// TestHeadingText checks that heading anchors and the table of contents use the text of a heading as
// displayed, after entities and escapes, as GitHub does.
func TestHeadingText(t *testing.T) {
	s := &Site{cfg: Config{Repo: "..", GitHub: "https://github.com/ornew/pego", Ref: "main"}, bySrc: map[string]*Page{}, byDir: map[string]*Page{}}
	p := &Page{Src: "docs/x.md", URL: "docs/x/"}
	s.add(p)
	m := s.parseMarkdown(p, []byte("# Title\n\n## A &amp; B\n\n## Escaped \\*star\\*\n\n## The `#recover` attribute & <b>more</b>\n\nSoft\nbreak\n----\n\n## Compiled grammars (`.pegoc`)\n"))
	want := []Heading{
		{1, "title", "Title"},
		{2, "a--b", "A & B"},
		{2, "escaped-star", "Escaped *star*"},
		{2, "the-recover-attribute--more", "The #recover attribute & more"},
		{2, "soft-break", "Soft break"},
		{2, "compiled-grammars-pegoc", "Compiled grammars (.pegoc)"},
	}
	if len(m.headings) != len(want) {
		t.Fatalf("headings = %+v", m.headings)
	}
	for i, h := range want {
		if m.headings[i] != h {
			t.Errorf("heading %d = %+v, want %+v", i, m.headings[i], h)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Compiled grammars (.pegoc)":         "compiled-grammars-pegoc",
		"Deep nesting and WithMaxDepth":      "deep-nesting-and-withmaxdepth",
		"005. Pratt Expressions (pratt)":     "005-pratt-expressions-pratt",
		"Choosing: a cheat sheet":            "choosing-a-cheat-sheet",
		"Snake_case and--dashes":             "snake_case-and--dashes",
		"Unicode: あいう":                       "unicode-あいう",
		"Errors, recovery & the `#error` op": "errors-recovery--the-error-op",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrepareOutRefusesForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "precious.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareOut(dir); err == nil {
		t.Fatal("prepareOut replaced a directory it did not write")
	}
	if _, err := os.Stat(filepath.Join(dir, "precious.txt")); err != nil {
		t.Fatal(err)
	}
}

// sidebarSection is a section of the navigation of a page.
type sidebarSection struct {
	Name        string
	Collapsible bool // a details element, which the reader can fold
	Open        bool
	Links       []sidebarLink
}

// sidebarLink is a link of the navigation of a page.
type sidebarLink struct {
	Href    string // relative to the page
	Title   string
	Current bool
}

var (
	sidebarRe        = regexp.MustCompile(`(?s)<nav id="sidebar".*?</nav>`)
	sidebarSectionRe = regexp.MustCompile(`(?s)<(div|details) class="nav-section([^"]*)"( open)?>(.*?)</(?:div|details)>`)
	sidebarHeadingRe = regexp.MustCompile(`<h2>([^<]*)</h2>`)
	sidebarLinkRe    = regexp.MustCompile(`<a href="([^"]*)"( aria-current="page")?>([^<]*)</a>`)
)

// readSidebar returns the sections of the navigation of a generated page, without the list of the
// site for mobile screens.
func readSidebar(t *testing.T, page string) []sidebarSection {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	nav := sidebarRe.FindString(string(data))
	if nav == "" {
		t.Fatalf("%s has no sidebar", page)
	}
	var secs []sidebarSection
	for _, m := range sidebarSectionRe.FindAllStringSubmatch(nav, -1) {
		if strings.Contains(m[2], "mobile-links") {
			continue
		}
		h := sidebarHeadingRe.FindStringSubmatch(m[4])
		if h == nil {
			t.Fatalf("%s: a section has no heading", page)
		}
		sec := sidebarSection{Name: h[1], Collapsible: m[1] == "details", Open: m[3] != ""}
		for _, l := range sidebarLinkRe.FindAllStringSubmatch(m[4], -1) {
			sec.Links = append(sec.Links, sidebarLink{Href: l[1], Title: html.UnescapeString(l[3]), Current: l[2] != ""})
		}
		secs = append(secs, sec)
	}
	return secs
}

// TestNavigation checks the navigation of the documentation: the titles of the pages of each section,
// and the collapsible section of design records, closed unless the current page is one of its entries.
func TestNavigation(t *testing.T) {
	out := filepath.Join(t.TempDir(), "dist")
	if _, err := Build(Config{Repo: "..", Out: out, Check: true, GitHub: "https://github.com/ornew/pego", Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	// The links of the navigation are relative to the page; this strips the "../" that leads to the root.
	site := func(href string) string { return strings.TrimLeft(href, "./") }

	// Only the index of a section is called "Introduction"; every other page has its own title.
	secs := readSidebar(t, filepath.Join(out, "docs", "guide", "runtime", "index.html"))
	var names []string
	intro := map[string]bool{}
	for _, sec := range secs {
		names = append(names, sec.Name)
		for _, l := range sec.Links {
			if l.Title == "Introduction" {
				intro[site(l.Href)] = true
			}
		}
	}
	if got, want := strings.Join(names, ", "), "Tutorial, Guides, Cookbook, Specification, Reference, Parsers, Design records, Project"; got != want {
		t.Errorf("sections = %s, want %s", got, want)
	}
	if want := map[string]bool{"docs/guide/": true, "docs/cookbook/": true, "spec/": true, "parsers/": true}; !maps.Equal(intro, want) {
		t.Errorf("pages titled Introduction = %v, want the indexes of Guides, Cookbook, Specification and Parsers", intro)
	}
	for _, sec := range secs {
		if sec.Name != "Parsers" {
			continue
		}
		parsers := 0
		for _, l := range sec.Links {
			dir := site(l.Href)
			if dir == "parsers/" {
				continue
			}
			parsers++
			// A parser is titled by the first heading of its page.
			data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(dir), "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			m := regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`).FindSubmatch(data)
			if m == nil {
				t.Fatalf("%s has no heading", dir)
			}
			if h := html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(string(m[1]), "")); l.Title != h {
				t.Errorf("%s is titled %q in the navigation, want %q", dir, l.Title, h)
			}
		}
		if parsers < 5 {
			t.Errorf("the Parsers section lists %d parsers", parsers)
		}
	}

	// Design records folds: closed on a page of the guides, open on a page of the section.
	design := func(secs []sidebarSection) sidebarSection {
		for _, sec := range secs {
			if sec.Name == "Design records" {
				return sec
			}
		}
		t.Fatal("no Design records section")
		return sidebarSection{}
	}
	for _, sec := range secs {
		if sec.Collapsible != (sec.Name == "Design records") || sec.Open {
			t.Errorf("section %s: collapsible=%v open=%v on a guide", sec.Name, sec.Collapsible, sec.Open)
		}
	}
	if sec := design(secs); len(sec.Links) < 15 {
		t.Errorf("Design records lists %d pages", len(sec.Links))
	}
	for _, tc := range []struct {
		page, current string // current is the title of the entry of the page, if the section should be open
	}{
		{"docs/guide/runtime/index.html", ""},
		{"parsers/json/index.html", ""},
		{"docs/design/index.html", "Index"},
		{"docs/design/012-typed-values/index.html", "012 Typed Values in Generated Parsers"},
	} {
		sec := design(readSidebar(t, filepath.Join(out, filepath.FromSlash(tc.page))))
		if sec.Open != (tc.current != "") {
			t.Errorf("%s: Design records open = %v, want %v", tc.page, sec.Open, tc.current != "")
		}
		current := ""
		for _, l := range sec.Links {
			if l.Current {
				current = l.Title
			}
		}
		if current != tc.current {
			t.Errorf("%s: current entry of Design records = %q, want %q", tc.page, current, tc.current)
		}
	}
}

func TestNavTitle(t *testing.T) {
	for _, tc := range []struct {
		p    *Page
		want string
	}{
		{&Page{Src: "parsers/README.md", Title: "Ready-made parsers", Index: true}, "Introduction"},
		{&Page{Src: "parsers/json/README.md", Title: "JSON"}, "JSON"},
		{&Page{Src: "docs/tutorial/getting-started.md", Title: "Getting started: a tutorial"}, "Getting started"},
	} {
		if got := navTitle(tc.p); got != tc.want {
			t.Errorf("navTitle(%s) = %q, want %q", tc.p.Src, got, tc.want)
		}
	}
}
