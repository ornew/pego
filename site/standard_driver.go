package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ornew/nanosite/sitegen"
)

func selectWasmMode(mode string) (string, error) {
	if mode == "" {
		mode = "go"
	}
	if mode != "go" && mode != "dual" {
		return "", fmt.Errorf("unsupported WASM mode %q (want go or dual)", mode)
	}
	return mode, nil
}

// Build uses Nanosite for document selection, Markdown, indexes, navigation,
// search and publication. Hooks supply only PEGO's product-specific pages.
func Build(cfg Config) (Stats, error) {
	if cfg.DeployURL != "" {
		if _, err := siteURL(cfg.DeployURL); err != nil {
			return Stats{}, err
		}
	}
	c, err := sitegen.ReadConfig(filepath.Join(cfg.Repo, "site/config.json"))
	if err != nil {
		return Stats{}, err
	}
	c.Repository, c.Branch = cfg.GitHub, cfg.Ref
	s := &Site{cfg: cfg, bySrc: map[string]*Page{}, byDir: map[string]*Page{}, wasm: "playground/pego.wasm"}
	mapped := map[*sitegen.Page]*Page{}
	lookup := func(p *sitegen.Page) *Page {
		if p == nil {
			return nil
		}
		return mapped[p]
	}
	ext := sitegen.Extensions{Marker: marker, AllowedOutput: []string{"site/dist"}, Protected: []string{"README.md", "docs", "spec", "site", ".git", "examples", "playground", "grammar", "internal", "cmd", "parsers", "bench"}}
	ext.Prepare = func(ctx *sitegen.Context) error {
		for _, q := range ctx.Pages {
			section := q.Section
			if section == "Design Records" {
				section = "Project"
			}
			p := &Page{Src: q.Source, URL: strings.TrimSuffix(q.Output, "index.html"), Title: q.Title, Nav: q.NavTitle, Section: section, Kind: "doc", Description: q.Description, SourceURL: cfg.GitHub + "/blob/" + cfg.Ref + "/" + q.Source}
			for _, h := range q.Headings {
				if h.Level >= 2 && h.Level <= 3 {
					p.TOC = append(p.TOC, Heading{ID: h.ID, Text: h.Title, Level: h.Level})
				}
			}
			for _, sec := range c.Sections {
				if sec.Index == q.Source {
					p.Index = true
					p.Nav = "Introduction"
				}
			}
			if p.Nav == p.Title {
				p.Nav = ""
			}
			if p.Nav == "" {
				p.Nav = navTitle(p)
			}
			if q.Source == "README.md" {
				p.Kind = "landing"
				p.Title = "PEGO"
				p.Nav = "Home"
				p.SourceURL = ""
			}
			if q.Source == "docs/design/README.md" {
				p.Src = ""
				p.SourceURL = ""
				p.Nav = "Overview"
			}
			s.add(p)
			if q.Source == "docs/design/README.md" {
				s.bySrc[q.Source] = p
				s.byDir["docs/design"] = p
			}
			mapped[q] = p
		}
		pg := &Page{URL: "playground/", Kind: "playground", Title: "Playground", Nav: "Playground", Description: "Write a PEGO grammar and parse input with it in your browser.", Scripts: []string{"playground/app.js"}, Styles: []string{"playground/playground.css"}}
		s.add(pg)
		if err := s.renderPlayground(); err != nil {
			return err
		}
		q, err := ctx.AddHTMLPage("playground/README.md", "playground/index.html", pg.Title, []byte(pg.Body))
		if err != nil {
			return err
		}
		if q != nil {
			mapped[q] = pg
		}
		ref := s.referencePages()
		if err := s.renderReference(); err != nil {
			return err
		}
		for _, p := range ref.Pages {
			q, err := ctx.AddHTMLPage(p.URL+"README.md", p.URL+"index.html", p.Title, []byte(p.Body))
			if err != nil {
				return err
			}
			if q == nil {
				continue
			}
			mapped[q] = p
			for _, h := range p.TOC {
				q.Headings = append(q.Headings, sitegen.Heading{ID: h.ID, Title: h.Text, Level: h.Level})
			}
		}
		if perf := s.bySrc["docs/performance.md"]; perf != nil {
			for _, p := range s.pages {
				if !isOptimizationDetail(p.Src) {
					continue
				}
				m := optimizationTitleNumberRe.FindStringSubmatch(p.Title)
				if m == nil {
					continue
				}
				n, _ := strconv.Atoi(m[1])
				id := fmt.Sprintf("%d-%s", n, slug(strings.TrimPrefix(p.Title, m[0])))
				for q, lp := range mapped {
					if lp == perf {
						q.Anchors[id] = true
					}
				}
			}
		}
		if cfg.PreviousSite != "" && !cfg.Wasm {
			return fmt.Errorf("-previous-site requires -wasm")
		}
		mode, err := selectWasmMode(cfg.WasmMode)
		if err != nil {
			return err
		}
		if mode == "dual" && !cfg.Wasm {
			return fmt.Errorf("-wasm-mode=dual requires -wasm")
		}
		var previousTinyGo *assetBytes
		previous, err := readPreviousAssetsWithTinyGo(cfg.PreviousSite, &previousTinyGo)
		if err != nil {
			return err
		}
		if cfg.Wasm {
			temp, err := os.MkdirTemp("", "pego-standard-assets-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(temp)
			name, err := buildWasm(cfg.Repo, temp, "go")
			if err != nil {
				return err
			}
			s.wasm = "playground/" + name
			var tinygo *assetBytes
			if mode == "dual" {
				tinyDir := filepath.Join(temp, "tinygo")
				if err := os.Mkdir(tinyDir, 0755); err != nil {
					return err
				}
				tinyName, err := buildWasm(cfg.Repo, tinyDir, "tinygo")
				if err != nil {
					return err
				}
				wasm, err := os.ReadFile(filepath.Join(tinyDir, filepath.FromSlash(tinyName)))
				if err != nil {
					return err
				}
				runtime, err := os.ReadFile(filepath.Join(tinyDir, "wasm_exec.js"))
				if err != nil {
					return err
				}
				tinygo = &assetBytes{asset: describeAssets(wasm, runtime), wasm: wasm, runtime: runtime}
				s.tinygoWasm = "playground/" + tinygo.asset.Wasm
				if err := os.RemoveAll(tinyDir); err != nil {
					return err
				}
			}
			if err := publishAssets(temp, name, cfg.DeployURL, previous); err != nil {
				return err
			}
			if err := publishTinyGoAssets(temp, tinygo, previousTinyGo); err != nil {
				return err
			}
			err = filepath.WalkDir(temp, func(file string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(temp, file)
				data, e := os.ReadFile(file)
				ctx.Assets["playground/"+filepath.ToSlash(rel)] = data
				return e
			})
			if err != nil {
				return err
			}
		}
		exs, err := readExamples(cfg.Repo)
		if err != nil {
			return err
		}
		data, err := json.Marshal(exs)
		if err != nil {
			return err
		}
		ctx.Assets["playground/examples.json"] = data
		return nil
	}
	ext.Decorate = func(ctx *sitegen.Context, q *sitegen.Page, body string) (string, error) {
		p := mapped[q]
		p.Body = template.HTML(body)
		if p.Kind == "landing" {
			if err := s.renderLanding(); err != nil {
				return "", err
			}
			q.Title = p.Title
		}
		if p.Src == "docs/performance.md" {
			s.addLegacyOptimizationAnchors()
		}
		p.Text = plainText(string(p.Body))
		return string(p.Body), nil
	}
	sectionIndex := map[string]string{}
	for _, sec := range c.Sections {
		sectionIndex[sec.Title] = sec.Index
	}
	ext.View = func(v sitegen.View) (any, error) {
		var secs []*Section
		for _, g := range v.Groups {
			sec := &Section{Name: g.Title, Collapsible: g.Collapsed}
			for _, q := range g.Pages {
				p := lookup(q)
				sec.Pages = append(sec.Pages, p)
				if idx := sectionIndex[g.Title]; idx != "" && q.Source == idx {
					sec.IndexPages = append(sec.IndexPages, p)
				} else {
					sec.NavPages = append(sec.NavPages, p)
				}
			}
			for _, child := range g.Children {
				group := &Section{Name: child.Title, Collapsible: child.Collapsed}
				for _, q := range child.Pages {
					p := lookup(q)
					sec.Pages = append(sec.Pages, p)
					group.Pages = append(group.Pages, p)
					group.NavPages = append(group.NavPages, p)
				}
				sec.Children = append(sec.Children, group)
			}
			secs = append(secs, sec)
		}
		s.sections = secs
		p := lookup(v.Page)
		p.Body = v.Body
		tinygoWasm := ""
		if s.tinygoWasm != "" {
			tinygoWasm = relURL(p.URL, s.tinygoWasm)
		}
		return layoutData{Site: s, Page: p, Root: relURL(p.URL, ""), Sections: secs, Prev: lookup(v.Previous), Next: lookup(v.Next), GitHub: cfg.GitHub, Wasm: relURL(p.URL, s.wasm), TinyGoWasm: tinygoWasm}, nil
	}
	if cfg.Check {
		ext.Validate = func(stage string) error {
			if problems := checkLinks(stage); len(problems) > 0 {
				return fmt.Errorf("site has %d problems:\n  %s", len(problems), strings.Join(problems, "\n  "))
			}
			return nil
		}
	}
	result, err := sitegen.BuildWith(cfg.Repo, cfg.Out, c, false, ext)
	if err != nil {
		return Stats{}, err
	}
	return stats(cfg.Out, len(result.Pages))
}
