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
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/nanosite/render"
)

// siteTestOutput uses the actual temporary directory, since macOS can expose
// its temporary root through a symlink while publication requires a real path.
func siteTestOutput(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "dist")
}

// TestBuild builds the whole site, including the playground's WebAssembly binary, and checks that
// every document of the repository became a page, that the generated pages are complete, and that
// no internal link is broken (Build fails on broken links).
func TestBuild(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not available")
	}
	out := siteTestOutput(t)
	cfg := Config{Repo: "..", Out: out, Wasm: !testing.Short(), Check: true, GitHub: "https://github.com/ornew/pego", Ref: "main"}
	st, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pages, %d files, %d bytes", st.Pages, st.Files, st.Bytes)

	// Every Markdown document has a page.
	policy, err := render.ReadSourcePolicy("..")
	if err != nil {
		t.Fatal(err)
	}
	var docs []string
	for _, dir := range []string{"docs", "spec"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Ext(p) == ".md" {
				rel, _ := filepath.Rel("..", p)
				if render.Matches(policy.Exclude, filepath.ToSlash(rel)) {
					return nil
				}
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

	// Only pages with a mermaid fence load the diagram renderer, and the vendored build it loads exists.
	withFence := 0
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || filepath.Ext(p) != ".html" {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fence := strings.Contains(string(data), `class="language-mermaid"`)
		loader := strings.Contains(string(data), `assets/mermaid.js"`)
		if fence {
			withFence++
		}
		if fence != loader {
			t.Errorf("%s: mermaid fence %v, renderer script %v", p, fence, loader)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if withFence == 0 {
		t.Error("no page has a mermaid fence; documentation-style.md shows one")
	}
	if _, err := os.Stat(filepath.Join(out, "assets", "vendor", "mermaid-11.12.0.min.js")); err != nil {
		t.Errorf("vendored Mermaid is not published: %v", err)
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
			"Grammar In. Bulletproof Parser Out.", `class="feature"`, `id="live"`, `data-input="1 &#43; 2 * 3 ^ 2"`, `id="live-input" class="live-input" value="1 &#43; 2 * 3 ^ 2"`, "pego parse -g calc.pego",
			`href="docs/tutorial/getting-started/"`, `id="install"`,
		},
		"reference/pego/index.html": {
			`id="CompileSource"`, `id="Parser.Parse"`, "type Node struct", `id="Node.Clone"`, "pkg.go.dev",
		},
		"reference/grammar/index.html":   {`id="Format"`, `id="Grammar.Rules"`, "type RuleDef struct"},
		"reference/cli/index.html":       {`id="cmd-parse"`, "<code>-g string</code>", "-backend string", `id="commands"`},
		"docs/design/records/index.html": {"012. Typed Values in Generated Parsers", "Implemented"},
		"docs/optimizations/index.html":  {"Optimization Catalog", "001. Value-free rule bodies", "076. Inline value-free plain generated Go rules", "077. Inline value-building and memoized Node expressions in generated Go parsers"},
		"docs/performance/index.html":    {`id="1-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903"`, `id="76-inline-value-free-plain-generated-go-rules"`, `id="77-inline-value-building-and-memoized-node-expressions-in-generated-go-parsers"`},
		"playground/index.html":          {`id="pg-grammar"`, `src="../playground/app.js"`, `href="../playground/playground.css"`},
		"docs/guide/running-parsers/index.html": {
			`id="compiled-grammars"`, `href="../../../spec/"`, `<pre><code class="language-pego">`,
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

	// Legacy performance fragments are derived from the numbered detail-page headings.
	perf, err := os.ReadFile(filepath.Join(out, "docs", "performance", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join("..", "docs", "optimizations", "[0-9][0-9][0-9]-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if render.Matches(policy.Exclude, strings.TrimPrefix(filepath.ToSlash(entry), "../")) {
			continue
		}
		src, err := os.ReadFile(entry)
		if err != nil {
			t.Fatal(err)
		}
		heading := regexp.MustCompile(`(?m)^# (.+)$`).FindSubmatch(src)
		if heading == nil {
			t.Fatalf("%s has no title", entry)
		}
		title := string(heading[1])
		number := optimizationTitleNumberRe.FindStringSubmatch(title)
		if number == nil {
			t.Fatalf("%s has no numbered title: %q", entry, title)
		}
		n, err := strconv.Atoi(number[1])
		if err != nil {
			t.Fatal(err)
		}
		legacy := strconv.Itoa(n) + "-" + slug(strings.TrimPrefix(title, number[0]))
		sourcePath := strings.TrimPrefix(filepath.ToSlash(entry), "../")
		target := relURL("docs/performance/", urlOf(sourcePath)) + "#" + slug(title)
		if !strings.Contains(string(perf), `id="`+legacy+`" data-moved-to="`+target+`"`) {
			t.Errorf("performance page legacy anchor %q does not redirect to %q", legacy, target)
		}
	}
	siteJS, err := os.ReadFile(filepath.Join("static", "assets", "site.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(siteJS), "installLegacyOptimizationRedirects(window)") {
		t.Error("site script does not install legacy optimization redirects")
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
	if got, want := strings.Join(names, " "), "parse test fmt fix convert gen compile trace profile explain lint sample schema edit-fuzz lsp"; got != want {
		t.Errorf("command sections = %s, want %s", got, want)
	}
	for cmd, flags := range map[string][]string{
		"parse":   {"-g string", "-check", "-stream", "-f string"},
		"test":    {"-f string", "-format string", "-backend string", "-equivalence", "-update"},
		"fmt":     {"-w", "-l"},
		"fix":     {"-w", "-l"},
		"trace":   {"-g string", "-f string", "-max-depth int", "-rule value", "-failures", "-unit string", "-backend string"},
		"profile": {"-sort string", "-n int", "-f string"},
		"explain": {"-n int", "-s string"},
		"lint":    {"-g string", "-f string", "-disable value", "-strict", "-list"},
		"schema":  {"-g string", "-s string", "-all-rules", "-o string"},
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
		for _, page := range []string{"index.html", "playground/index.html", "docs/guide/running-parsers/index.html"} {
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
	want := []string{"missing/", "a/#nope", "#gone", "gone.js", "../outside/", "#unicode-%E3%81%82"}
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
	s.add(&Page{Src: "spec/operators-rule.md", URL: "spec/operators-rule/"})
	s.add(&Page{Src: "README.md", URL: ""})
	for _, tc := range []struct{ dest, want string }{
		{"https://example.com/a.md", "https://example.com/a.md"},
		{"#backends", "#backends"},
		{"../../spec/operators-rule.md#levels", "../../../spec/operators-rule/#levels"},
		{"../../spec/", "../../../spec/"},
		{"../../spec/README.md", "../../../spec/"},
		{"../../README.md", "../../../"},
		{"../../examples/calculator/calc.pego", "https://github.com/ornew/pego/blob/main/examples/calculator/calc.pego"},
		{"../../examples/calculator/", "https://github.com/ornew/pego/tree/main/examples/calculator"},
		// Links relative to the repository root and percent-encoded paths work on GitHub too.
		{"/spec/operators-rule.md#levels", "../../../spec/operators-rule/#levels"},
		{"/examples/calculator/calc.pego", "https://github.com/ornew/pego/blob/main/examples/calculator/calc.pego"},
		{"/", "../../../"},
		{"%2E%2E/%2E%2E/spec/operators-rule.md", "../../../spec/operators-rule/"},
		{"../../spec/operators-rule.md#caf%C3%A9", "../../../spec/operators-rule/#caf%C3%A9"},
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
	m := s.parseMarkdown(p, []byte("# Title\n\n## A &amp; B\n\n## Escaped \\*star\\*\n\n## The `#recover` pragma & <b>more</b>\n\nSoft\nbreak\n----\n\n## Compiled grammars (`.pegoc`)\n"))
	want := []Heading{
		{Level: 1, ID: "title", Text: "Title"},
		{Level: 2, ID: "a--b", Text: "A & B"},
		{Level: 2, ID: "escaped-star", Text: "Escaped *star*"},
		{Level: 2, ID: "the-recover-pragma--more", Text: "The #recover pragma & more"},
		{Level: 2, ID: "soft-break", Text: "Soft break"},
		{Level: 2, ID: "compiled-grammars-pegoc", Text: "Compiled grammars (.pegoc)"},
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
		"Compiled grammars (.pegoc)":           "compiled-grammars-pegoc",
		"Deep nesting and WithMaxDepth":        "deep-nesting-and-withmaxdepth",
		"005. Operators Rule (operators rule)": "005-operators-rule-operators-rule",
		"Choosing: a cheat sheet":              "choosing-a-cheat-sheet",
		"Snake_case and--dashes":               "snake_case-and--dashes",
		"Unicode: あいう":                         "unicode-あいう",
		"Errors, recovery & the `@error` op":   "errors-recovery--the-error-op",
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

// TestNavigation checks the navigation of the documentation, including the nested numbered design records.
func TestNavigation(t *testing.T) {
	out := siteTestOutput(t)
	if _, err := Build(Config{Repo: "..", Out: out, Check: true, GitHub: "https://github.com/ornew/pego", Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	// The links of the navigation are relative to the page; this strips the "../" that leads to the root.
	site := func(href string) string { return strings.TrimLeft(href, "./") }

	// Only designated section indexes are called "Introduction". Design and optimization
	// records remain rendered and searchable; design records also appear in their disclosure.
	secs := readSidebar(t, filepath.Join(out, "docs", "guide", "running-parsers", "index.html"))
	var names []string
	intro := map[string]bool{}
	for _, sec := range secs {
		names = append(names, sec.Name)
	}
	// Sections with nested groups close their first group before their own links, so the
	// introductions are collected from the whole sidebar.
	page, err := os.ReadFile(filepath.Join(out, "docs", "guide", "running-parsers", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range sidebarLinkRe.FindAllStringSubmatch(sidebarRe.FindString(string(page)), -1) {
		if html.UnescapeString(l[3]) == "Introduction" {
			intro[site(l[1])] = true
		}
	}
	if got, want := strings.Join(names, ", "), "Tutorial, Guides, Cookbook, Specification, Reference, Parsers, Project"; got != want {
		t.Errorf("sections = %s, want %s", got, want)
	}
	if want := map[string]bool{"docs/tutorial/": true, "docs/guide/": true, "docs/cookbook/": true, "spec/": true, "parsers/": true}; !maps.Equal(intro, want) {
		t.Errorf("pages titled Introduction = %v, want %v", intro, want)
	}
	// The tutorial and the specification are numbered series: the navigation numbers their
	// pages, and each page names its place above its title. The glossary stays unnumbered.
	for _, tc := range []struct{ page, label, nav string }{
		{"docs/tutorial/structured-input", "Lesson 2 of 10", "2. Parsing Structured Input"},
		{"spec/parser-expressions", "Chapter 5", "5. Rule Expressions"},
	} {
		content, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(tc.page), "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), `<p class="series-label">`+tc.label+`</p>`) {
			t.Errorf("%s does not name its place %q", tc.page, tc.label)
		}
		if !strings.Contains(sidebarRe.FindString(string(content)), `aria-current="page">`+tc.nav+`</a>`) {
			t.Errorf("%s is not %q in the navigation", tc.page, tc.nav)
		}
	}
	if glossary, err := os.ReadFile(filepath.Join(out, "spec", "glossary", "index.html")); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(glossary), "series-label") || !strings.Contains(string(glossary), `aria-current="page">Glossary</a>`) {
		t.Error("the glossary is numbered as a chapter")
	}
	apiPage, err := os.ReadFile(filepath.Join(out, "reference", "grammar", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	referenceBlock := regexp.MustCompile(`(?s)<div class="nav-section">\s*<h2>Reference</h2>(.*?)</div>`).FindSubmatch(apiPage)
	if referenceBlock == nil {
		t.Fatal("Reference section is missing")
	}
	referenceHTML := string(referenceBlock[1])
	apiGroup := regexp.MustCompile(`(?s)<details class="nav-fold nav-subfold">\s*<summary><span>Go API Reference</span></summary>(.*?)</details>`).FindStringSubmatch(referenceHTML)
	if apiGroup == nil {
		t.Fatal("Go API Reference should be closed by default, including on package pages")
	}
	for _, label := range []string{"Overview", "Package <code>pego</code>", "Package <code>pego/grammar</code>", "Package <code>pego/runtime</code>"} {
		if !strings.Contains(apiGroup[1], ">"+label+"</a>") {
			t.Errorf("Go API group omits label %q", label)
		}
	}
	if strings.Count(apiGroup[1], "<a ") != 4 || strings.Contains(apiGroup[1], "CLI Reference") {
		t.Error("Go API group should contain only its overview and three package pages")
	}
	ordinaryReference := strings.Replace(referenceHTML, apiGroup[0], "", 1)
	if !strings.Contains(ordinaryReference, ">CLI Reference</a>") || strings.Contains(ordinaryReference, "Package ") {
		t.Error("CLI should remain directly visible and package links should appear only inside Go API Reference")
	}
	if !strings.Contains(apiGroup[1], `aria-current="page">Package <code>pego/grammar</code>`) {
		t.Error("current package is not marked within its Go API group")
	}
	for path, heading := range map[string]string{
		"reference/index.html":         `<h1 id="api-reference">Go API Reference</h1>`,
		"reference/pego/index.html":    `<h1 id="top">Package <code>pego</code></h1>`,
		"reference/grammar/index.html": `<h1 id="top">Package <code>pego/grammar</code></h1>`,
		"reference/cli/index.html":     `<h1 id="top">CLI Reference</h1>`,
	} {
		content, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), heading) {
			t.Errorf("%s missing requested heading %s", path, heading)
		}
	}
	var project sidebarSection
	for _, sec := range secs {
		if sec.Name == "Project" {
			project = sec
		}
	}
	if project.Name == "" || project.Collapsible {
		t.Fatal("Project links should remain visible in an ordinary section")
	}
	designPage := filepath.Join(out, "docs", "design", "records", "042-responsive-lsp-analysis-and-code-actions", "index.html")
	record, err := os.ReadFile(designPage)
	if err != nil {
		t.Fatal(err)
	}
	guidesBlock := regexp.MustCompile(`(?s)<div class="nav-section">\s*<h2>Guides</h2>\s*<ul><li><a [^>]*>([^<]*)</a>`).FindSubmatch(record)
	if guidesBlock == nil || string(guidesBlock[1]) != "Introduction" {
		t.Errorf("the Guides navigation does not start with its Introduction before the nested groups")
	}
	projectBlock := regexp.MustCompile(`(?s)<div class="nav-section">\s*<h2>Project</h2>(.*?)</div>`).FindSubmatch(record)
	if projectBlock == nil {
		t.Fatal("Project navigation section is missing")
	}
	projectHTML := string(projectBlock[1])
	groupStart := `<details class="nav-fold nav-subfold">`
	groupRe := regexp.MustCompile(`(?s)<details class="nav-fold nav-subfold">(.*?)</details>`)
	groupAt := groupRe.FindStringSubmatchIndex(projectHTML)
	if groupAt == nil {
		t.Fatal("Project navigation has no nested numbered-record disclosure")
	}
	if got := strings.TrimSpace(projectHTML[:groupAt[0]]); got != "<ul><li>" {
		t.Errorf("Design Records disclosure is not the first Project item: prefix %q", got)
	}
	if strings.Count(projectHTML, groupStart) != 1 {
		t.Errorf("Project navigation has %d Design Records disclosures, want one", strings.Count(projectHTML, groupStart))
	}
	ordinaryHTML := projectHTML[:groupAt[0]] + projectHTML[groupAt[1]:]
	ordinaryLinks := sidebarLinkRe.FindAllStringSubmatch(ordinaryHTML, -1)
	ordinary := map[string]string{}
	for _, link := range ordinaryLinks {
		href := site(link[1])
		ordinary[href] = html.UnescapeString(link[3])
		if strings.HasPrefix(href, "docs/design/records/") && href != "docs/design/records/" {
			t.Errorf("numbered design record is directly exposed outside its disclosure: %s", href)
		}
		if strings.HasPrefix(href, "docs/optimizations/") && href != "docs/optimizations/" {
			t.Errorf("individual optimization appears in the sidebar: %s", href)
		}
	}
	if ordinary["docs/design/records/"] != "" || ordinary["docs/optimizations/"] != "Optimizations" || ordinary["docs/development/"] != "Development" {
		t.Errorf("ordinary Project links are missing: %v", ordinary)
	}
	group := groupRe.FindStringSubmatch(projectHTML)
	if group == nil {
		t.Fatal("numbered-record disclosure is not closed properly")
	}
	openTag := regexp.MustCompile(`<details class="nav-fold nav-subfold"[^>]*>`).FindString(projectHTML)
	if strings.Contains(openTag, " open") {
		t.Error("numbered-record disclosure opened automatically on a record page")
	}
	if !strings.Contains(group[1], "Design Records") {
		t.Error("numbered-record disclosure has no visible summary")
	}
	groupLinks := sidebarLinkRe.FindAllStringSubmatch(group[1], -1)
	if len(groupLinks) == 0 || html.UnescapeString(groupLinks[0][3]) != "Overview" || site(groupLinks[0][1]) != "docs/design/records/" {
		t.Errorf("Design Records disclosure does not start with its Overview link: %v", groupLinks)
	}
	overviewCount := 0
	for _, link := range groupLinks {
		if site(link[1]) == "docs/design/records/" {
			overviewCount++
		}
	}
	if overviewCount != 1 {
		t.Errorf("Design Records disclosure has %d Overview links, want one", overviewCount)
	}
	entries, err := filepath.Glob(filepath.Join("..", "docs", "design", "records", "[0-9][0-9][0-9]-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	var wantDesignPages []string
	for _, entry := range entries {
		sourcePath := strings.TrimPrefix(filepath.ToSlash(entry), "../")
		wantDesignPages = append(wantDesignPages, urlOf(sourcePath))
	}
	wantDesignPages = append([]string{"docs/design/records/"}, wantDesignPages...)
	var designPages []string
	var currentIsActive bool
	for _, link := range sidebarLinkRe.FindAllStringSubmatch(group[1], -1) {
		href := site(link[1])
		designPages = append(designPages, href)
		if href == "docs/design/records/042-responsive-lsp-analysis-and-code-actions/" && link[2] == " aria-current=\"page\"" {
			currentIsActive = true
		}
	}
	if !slices.Equal(designPages, wantDesignPages) {
		t.Errorf("nested design links = %v, want numeric record order %v", designPages, wantDesignPages)
	}
	if !currentIsActive {
		t.Error("current design record is not marked active inside the closed disclosure")
	}
	indexPage, err := os.ReadFile(filepath.Join(out, "docs", "design", "records", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	indexProject := regexp.MustCompile(`(?s)<div class="nav-section">\s*<h2>Project</h2>(.*?)</div>`).FindSubmatch(indexPage)
	if indexProject == nil {
		t.Fatal("Project section is missing from the design overview")
	}
	indexGroup := regexp.MustCompile(`(?s)<details class="nav-fold nav-subfold">(.*?)</details>`).FindSubmatch(indexProject[1])
	if indexGroup == nil || !regexp.MustCompile(`<a href="[^"]*" aria-current="page">Overview</a>`).Match(indexGroup[1]) {
		t.Error("Design Records overview is not marked active inside its disclosure")
	}
	indexDetails := regexp.MustCompile(`<details class="nav-fold nav-subfold"[^>]*>`).Find(indexProject[1])
	if strings.Contains(string(indexDetails), " open") {
		t.Error("Design Records disclosure opened automatically on the overview page")
	}
	pager := regexp.MustCompile(`(?s)<nav class="pager"[^>]*>(.*?)</nav>`).FindSubmatch(record)
	if pager == nil {
		t.Fatal("design record footer pager is missing")
	}
	pagerLinks := map[string]string{}
	for _, link := range regexp.MustCompile(`<a class="(prev|next)" href="([^"]*)"`).FindAllStringSubmatch(string(pager[1]), -1) {
		pagerLinks[link[1]] = site(link[2])
	}
	wantPager := map[string]string{"prev": "docs/design/records/041-missing-token-recovery/", "next": "docs/design/records/043-generated-parser-observability/"}
	if !maps.Equal(pagerLinks, wantPager) {
		t.Errorf("design record footer pager = %v, want %v", pagerLinks, wantPager)
	}

	// Individual records remain in search and the indexes point to their generated pages.
	search, err := os.ReadFile(filepath.Join(out, "search-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var searchEntries []searchEntry
	if err := json.Unmarshal(search, &searchEntries); err != nil {
		t.Fatal(err)
	}
	searchPages := map[string]bool{}
	for _, e := range searchEntries {
		searchPages[e.URL] = true
	}
	for _, page := range []string{"docs/design/records/012-typed-values/", "docs/optimizations/077-inline-node-expressions-in-generated-go-parsers/"} {
		if !searchPages[page] {
			t.Errorf("search index omits %s", page)
		}
	}
	designIndex, err := os.ReadFile(filepath.Join(out, "docs", "design", "records", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(designIndex), "docs/design/records/012-typed-values/") {
		t.Error("Design Records index omits its detail page")
	}
	optimizationIndex, err := os.ReadFile(filepath.Join(out, "docs", "optimizations", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(optimizationIndex), "077-inline-node-expressions-in-generated-go-parsers/") {
		t.Error("Optimizations index omits its detail page")
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

func TestHasMermaidFence(t *testing.T) {
	for src, want := range map[string]bool{
		"```mermaid\nflowchart LR\n```\n": true,
		"~~~mermaid\nx\n~~~\n":            true,
		"   ```mermaid  \nx\n```\n":       true,
		"```text\nmermaid\n```\n":         false,
		"```mermaidjs\nx\n```\n":          false,
		"The ```mermaid fence\n":          false,
		"":                                false,
	} {
		if got := hasMermaidFence([]byte(src)); got != want {
			t.Errorf("hasMermaidFence(%q) = %v, want %v", src, got, want)
		}
	}
}
