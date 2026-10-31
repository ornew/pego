package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Limits of Cloudflare Workers static assets for the _headers and _redirects files.
const (
	maxHeaderRules     = 100
	maxHeaderLine      = 2000
	maxStaticRedirects = 2000
	maxDynamicRedirect = 100
	maxRedirectLine    = 1000
)

// wranglerConfig reads wrangler.jsonc, dropping the full-line // comments that JSONC allows.
func wranglerConfig(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../wrangler.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "//") {
			lines = append(lines, l)
		}
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &cfg); err != nil {
		t.Fatalf("wrangler.jsonc: %v", err)
	}
	return cfg
}

// TestWranglerAssetsDirectory checks that the Worker serves the directory that build.sh writes and
// has no script of its own.
func TestWranglerAssetsDirectory(t *testing.T) {
	cfg := wranglerConfig(t)
	if cfg["name"] != "zeroparse-site" {
		t.Errorf("name = %v", cfg["name"])
	}
	if _, err := os.Stat("../wrangler.jsonc"); err != nil {
		t.Fatal(err)
	}
	if d, ok := cfg["compatibility_date"].(string); !ok || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(d) {
		t.Errorf("compatibility_date = %v", cfg["compatibility_date"])
	}
	if _, ok := cfg["main"]; ok {
		t.Error("an assets-only Worker has no main script")
	}
	assets, _ := cfg["assets"].(map[string]any)
	dir, _ := assets["directory"].(string)
	script, err := os.ReadFile("build.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`-out (\S+)`).FindStringSubmatch(string(script))
	if m == nil {
		t.Fatal("build.sh passes no -out directory")
	}
	// build.sh changes into its own directory, site/, before it runs the builder.
	if want := path.Join("site", m[1]); path.Clean(dir) != want {
		t.Errorf("assets.directory %q does not match the build output %q", dir, want)
	}
	if strings.HasPrefix(dir, "/") || strings.Contains(dir, "..") {
		t.Errorf("assets.directory %q must stay inside the repository", dir)
	}
}

// TestNoNetlify checks that no Netlify configuration, path or mention is left in the repository's
// site files and documentation.
func TestNoNetlify(t *testing.T) {
	name := "net" + "lify"
	oldHost := "zp." + "ornew.net"
	for _, f := range []string{"../netlify.toml", "../.netlify", "../netlify"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s still exists", f)
		}
	}
	self := "deploy_test.go"
	skip := map[string]bool{".git": true, "dist": true, "node_modules": true, ".scratch": true, ".claude": true}
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			// Imported Nanosite documentation is not site configuration.
			if filepath.ToSlash(p) == "../.agents" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(p) == self || (filepath.Base(p) != "_headers" && !strings.Contains("|.go|.sh|.md|.json|.jsonc|.toml|.html|.js|.yml|.yaml|", "|"+filepath.Ext(p)+"|")) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		low := strings.ToLower(string(data))
		if strings.Contains(low, name) || strings.Contains(low, oldHost) {
			t.Errorf("%s mentions the former host or platform", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCanonicalHost checks that the configured site URL, the builder's production site and the
// wrangler configuration name one host.
func TestCanonicalHost(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		SiteURL       string `json:"siteURL"`
		RedirectRules *bool  `json:"redirectRules"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SiteURL != productionSite || productionSite != "https://zeroparse.dev" {
		t.Errorf("siteURL %q, productionSite %q, want https://zeroparse.dev", cfg.SiteURL, productionSite)
	}
	if cfg.RedirectRules == nil || !*cfg.RedirectRules {
		t.Error("redirectRules must be true: the Worker reads _redirects")
	}
}

type headerRule struct {
	pattern string
	values  map[string]string
}

// parseHeaders reads a _headers file: a URL pattern line, then indented Name: value lines.
func parseHeaders(t *testing.T, data string) []headerRule {
	t.Helper()
	var rules []headerRule
	for i, line := range strings.Split(data, "\n") {
		if len(line) > maxHeaderLine {
			t.Errorf("_headers line %d has %d characters (limit %d)", i+1, len(line), maxHeaderLine)
		}
		switch {
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#"):
		case line[0] != ' ' && line[0] != '\t':
			rules = append(rules, headerRule{pattern: strings.TrimSpace(line), values: map[string]string{}})
		default:
			if len(rules) == 0 {
				t.Fatalf("_headers line %d has a header before any URL pattern", i+1)
			}
			name, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
			if !ok || name == "" || strings.HasPrefix(name, "#") {
				t.Fatalf("_headers line %d is not \"Name: value\": %q", i+1, line)
			}
			rules[len(rules)-1].values[name] = value
		}
	}
	return rules
}

// checkWorkerControlFiles checks _headers and _redirects of a built site against the policy that
// the site needs and the rules of Workers static assets.
func checkWorkerControlFiles(t *testing.T, out string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "_headers"))
	if err != nil {
		t.Fatal(err)
	}
	rules := parseHeaders(t, string(data))
	if len(rules) > maxHeaderRules {
		t.Errorf("_headers has %d rules (limit %d)", len(rules), maxHeaderRules)
	}
	byPattern := map[string]map[string]string{}
	for _, r := range rules {
		if byPattern[r.pattern] != nil {
			t.Errorf("_headers repeats the pattern %s", r.pattern)
		}
		byPattern[r.pattern] = r.values
	}
	all := byPattern["/*"]
	csp := all["Content-Security-Policy"]
	for _, want := range []string{"default-src 'self'", "script-src 'self' 'wasm-unsafe-eval'", "style-src 'self' 'unsafe-inline'", "img-src 'self' data:", "connect-src 'self'", "worker-src 'self'", "object-src 'none'", "base-uri 'self'", "form-action 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("Content-Security-Policy lacks %q: %s", want, csp)
		}
	}
	if all["X-Content-Type-Options"] != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", all["X-Content-Type-Options"])
	}
	if all["Referrer-Policy"] != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q", all["Referrer-Policy"])
	}
	const immutable = "public, max-age=31536000, immutable"
	if got := byPattern["/playground/wasm/*"]["Cache-Control"]; got != immutable {
		t.Errorf("wasm Cache-Control = %q", got)
	}
	if got := byPattern["/playground/wasm/*"]["Content-Type"]; got != "application/wasm" {
		t.Errorf("wasm Content-Type = %q", got)
	}
	if got := byPattern["/playground/runtime/*"]["Cache-Control"]; got != immutable {
		t.Errorf("runtime Cache-Control = %q", got)
	}

	checkRedirects(t, out)
}

// checkRedirects checks the generated _redirects file against the rules of Workers.
func checkRedirects(t *testing.T, out string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "_redirects"))
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{"301": true, "302": true, "303": true, "307": true, "308": true}
	seen := map[string]bool{}
	static, dynamic := 0, 0
	sawDynamic := false
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > maxRedirectLine {
			t.Errorf("_redirects line %d has %d characters (limit %d)", i+1, len(line), maxRedirectLine)
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Errorf("_redirects line %d: want source, destination and code: %q", i+1, line)
			continue
		}
		src, dst, code := f[0], f[1], f[2]
		if !codes[code] {
			t.Errorf("_redirects line %d: status %s is not a redirect code Workers supports", i+1, code)
		}
		if !strings.HasPrefix(src, "/") {
			t.Errorf("_redirects line %d: source %q is not a path", i+1, src)
		}
		if seen[src] {
			t.Errorf("_redirects line %d repeats the source %s", i+1, src)
		}
		seen[src] = true
		if strings.Count(src, "*") > 1 {
			t.Errorf("_redirects line %d: more than one splat", i+1)
		}
		if strings.ContainsAny(src, "*:") {
			dynamic++
			sawDynamic = true
		} else {
			static++
			if sawDynamic {
				t.Errorf("_redirects line %d: a static redirect follows a dynamic one", i+1)
			}
		}
		// A redirect leads to a published page or file.
		if strings.HasPrefix(dst, "/") && !strings.ContainsAny(dst, "*:") {
			target := filepath.Join(out, filepath.FromSlash(strings.TrimPrefix(dst, "/")))
			if strings.HasSuffix(dst, "/") {
				target = filepath.Join(target, "index.html")
			}
			if info, err := os.Stat(target); err != nil || info.IsDir() {
				t.Errorf("_redirects line %d: destination %s is not published", i+1, dst)
			}
		}
	}
	if static > maxStaticRedirects || dynamic > maxDynamicRedirect {
		t.Errorf("_redirects has %d static and %d dynamic rules (limits %d and %d)", static, dynamic, maxStaticRedirects, maxDynamicRedirect)
	}
	if static == 0 {
		t.Error("_redirects has no rules; the moved specification and design records need them")
	}
	t.Log("redirects: " + strconv.Itoa(static) + " static, " + strconv.Itoa(dynamic) + " dynamic")
}

// TestWorkerControlFilesParsers exercises the parsers on small inputs, so that a wrong checker
// cannot accept everything.
func TestWorkerControlFilesParsers(t *testing.T) {
	rules := parseHeaders(t, "# c\n/*\n  A: b\n  C: d: e\n\n/x/*\n\tF: g\n")
	if len(rules) != 2 || rules[0].pattern != "/*" || rules[0].values["C"] != "d: e" || rules[1].values["F"] != "g" {
		t.Fatalf("parsed %+v", rules)
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "x")
	write("_redirects", "/a/ /index.html 301\n/b/* /index.html 302\n")
	checkRedirects(t, dir) // valid: must not fail
}
