package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

// Exercise the CLI formatters with real Document events: an ordinary trace
// command parses only once and cannot produce repetition-resumption events.
func TestTraceReusedElementsPresentation(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			p, err := pego.CompileSource(`rule main = lines $$
rule lines = line*
rule line = [a-z]+ #atomic "\n"`, "main")
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			printer := tracePrinter{w: bufio.NewWriter(&out), json: format == "json", rules: []string{"main"}, maxDepth: 1}
			var profile pego.Profile
			active := false
			doc, err := p.NewDocument(strings.Repeat("ab\n", 32), pego.WithTrace(func(e pego.TraceEvent) {
				if active {
					printer.event(e)
					profile.Trace(e)
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Parse(); err != nil {
				t.Fatal(err)
			}
			if err := doc.Edit(48, 49, "long"); err != nil {
				t.Fatal(err)
			}
			active = true
			for round := 0; round < 2; round++ {
				out.Reset()
				if _, err := doc.Parse(); err != nil {
					t.Fatal(err)
				}
				printer.flush()
				if err := printer.w.Flush(); err != nil {
					t.Fatal(err)
				}
				if printer.err != nil {
					t.Fatal(printer.err)
				}
				if format == "text" {
					if round == 0 && !strings.Contains(out.String(), " [31 reused elements]\n") {
						t.Fatalf("missing reuse suffix: %s", out.String())
					}
					if round == 1 && (!strings.Contains(out.String(), " [memo]") || strings.Contains(out.String(), "reused elements")) {
						t.Fatalf("root memo hit replayed reuse: %s", out.String())
					}
					if strings.Count(out.String(), "\n") != 1 {
						t.Fatalf("filter/depth changed: %s", out.String())
					}
				} else {
					lines := strings.Split(strings.TrimSpace(out.String()), "\n")
					if len(lines) != 2 {
						t.Fatalf("got %d JSON events: %s", len(lines), out.String())
					}
					for i, line := range lines {
						var e map[string]any
						if err := json.Unmarshal([]byte(line), &e); err != nil {
							t.Fatal(err)
						}
						if e["rule"] != "main" || e["depth"] != float64(1) {
							t.Fatalf("filter/depth changed: %v", e)
						}
						if i == 1 && round == 0 {
							if e["reusedElements"] != float64(31) || e["matched"] != true || e["evals"] != float64(1) {
								t.Fatalf("exit lost execution/reuse data: %v", e)
							}
						} else if _, present := e["reusedElements"]; present {
							t.Fatalf("enter or memo hit reports historical reuse: %v", e)
						}
					}
				}
			}
			// Both root and enclosing repetition rows report inclusive reuse;
			// the total counts that reuse once across the two parses above.
			out.Reset()
			if err := printProfile(&out, &profile, profile.Rules, nil, len(profile.Rules)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "31 reused repetition elements (completed root calls)\n") {
				t.Fatalf("root-only summary missing: %s", out.String())
			}
			rows := 0
			for _, line := range strings.Split(out.String(), "\n") {
				f := strings.Fields(line)
				if len(f) == 13 && (f[0] == "main" || f[0] == "lines") {
					rows++
					if f[12] != "31" {
						t.Fatalf("inclusive reuse column: %s", line)
					}
				}
			}
			if rows != 2 {
				t.Fatalf("missing inclusive rows: %s", out.String())
			}
		})
	}
}
