package main

import (
	"encoding/json"
	"io/fs"
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
	if got, want := strings.Join(names, " "), "parse fmt convert gen compile trace profile explain"; got != want {
		t.Errorf("command sections = %s, want %s", got, want)
	}
	for cmd, flags := range map[string][]string{
		"parse":   {"-g string", "-check", "-stream", "-f string"},
		"fmt":     {"-w", "-l"},
		"trace":   {"-g string", "-f string", "-max-depth int", "-rule value", "-failures", "-unit string", "-backend string"},
		"profile": {"-sort string", "-n int", "-f string"},
		"explain": {"-n int", "-s string"},
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
		files = append(files, "pego.wasm", "wasm_exec.js")
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(out, "playground", filepath.FromSlash(f))); err != nil {
			t.Error(err)
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

A <grammar> is PEGO source
or JSON.

Run "pego <command> -h".
`)
	if err != nil {
		t.Fatal(err)
	}
	if u.synopsis != "pego <command> [flags]" || len(u.commands) != 2 || len(u.notes) != 2 {
		t.Fatalf("%+v", u)
	}
	p := u.commands[0]
	if p.name != "parse" || p.synopsis != "pego parse -g <grammar> [-s <rule>]\n     [-unit u]" || p.description != "Parse the input. Without -i, it reads standard input." {
		t.Errorf("parse: %+v", p)
	}
	if u.commands[1].name != "fmt" || u.notes[0] != "A <grammar> is PEGO source or JSON." {
		t.Errorf("%+v", u)
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
	write("a/index.html", `<p id="x">x</p>`)
	write("style.css", ``)
	problems := checkLinks(out)
	want := []string{"missing/", "a/#nope", "#gone", "gone.js", "../outside/", "a/?q=1&r=2"}
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
		{"../../examples/json/json.pego", "https://github.com/ornew/pego/blob/main/examples/json/json.pego"},
		{"../../examples/json/", "https://github.com/ornew/pego/tree/main/examples/json"},
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
	if len(s.problems) != 2 {
		t.Errorf("problems = %v, want two", s.problems)
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
