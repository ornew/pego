package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

var renderBenchmarkResult any

// Keep this harness usable without the controlled rendering helpers so that the
// same successful requests can also be measured against the previous revision.
func renderBenchmarkSource(fields int) string {
	var source strings.Builder
	source.WriteString("// __A type__ with `fields`.\ntype T struct {\n")
	for i := range fields {
		fmt.Fprintf(&source, " F%d Match\n", i)
	}
	source.WriteString("}\n// The main rule.\nrule main = ")
	for i := range fields {
		fmt.Fprintf(&source, "c%d:\"a\" ", i)
	}
	source.WriteString("\nrule value = \"v\" => T{}\n")
	return source.String()
}

func renderBenchmarkInputs(tb testing.TB) map[string]string {
	tb.Helper()
	sources := map[string]string{"fields512": renderBenchmarkSource(512)}
	for name, path := range map[string]string{"json": "../../parsers/json/json.pego", "source-grammar": "../../pego.pego"} {
		data, err := os.ReadFile(path)
		if err != nil {
			tb.Fatal(err)
		}
		sources[name] = string(data)
	}
	return sources
}

func renderBenchmarkRequest(a *analysis, method string) *incoming {
	p := docParams("file:///render.pego")
	if method != "textDocument/documentSymbol" {
		off := 0
		for _, d := range a.defs {
			if d.rule != nil {
				off = d.nameStart
				break
			}
		}
		if method == "textDocument/completion" {
			off = len(a.text)
			if i := strings.Index(a.text, "T{}"); i >= 0 {
				off = i + 2
			}
		}
		p["position"] = a.idx.position(off)
	}
	params, _ := json.Marshal(p)
	return &incoming{Method: method, Params: params}
}

func renderBenchmarkServer(a *analysis, ctx context.Context, rich bool) *Server {
	s := &Server{docs: map[string]*document{"file:///render.pego": {uri: "file:///render.pego", text: a.text, an: a}}, state: stateInitialized, requestContext: ctx}
	s.client.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport = rich
	s.client.TextDocument.Completion.CompletionItem.SnippetSupport = rich
	if rich {
		s.client.TextDocument.Hover.ContentFormat = []string{"markdown"}
		s.client.TextDocument.Completion.CompletionItem.DocumentationFormat = []string{"markdown"}
	}
	return s
}

func BenchmarkRequestRendering(b *testing.B) {
	sources := renderBenchmarkInputs(b)
	for _, name := range []string{"json", "source-grammar", "fields512"} {
		a := analyze(sources[name])
		if a.prog == nil {
			b.Fatalf("%s has no compiled grammar", name)
		}
		for _, operation := range []string{"documentSymbol", "hover", "completion"} {
			msg := renderBenchmarkRequest(a, "textDocument/"+operation)
			for _, rich := range []bool{false, true} {
				for _, active := range []bool{false, true} {
					b.Run(fmt.Sprintf("%s/%s/rich=%t/active=%t", name, operation, rich, active), func(b *testing.B) {
						var ctx context.Context
						if active {
							var cancel context.CancelFunc
							ctx, cancel = context.WithCancel(context.Background())
							defer cancel()
						}
						s := renderBenchmarkServer(a, ctx, rich)
						b.ReportAllocs()
						for b.Loop() {
							var err *rpcError
							renderBenchmarkResult, err = s.request(msg)
							if err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}

// Print deterministic response fingerprints for comparing this exact request
// corpus with the previous revision, including plaintext/Markdown negotiation.
func TestRequestRenderingFingerprints(t *testing.T) {
	sources := renderBenchmarkInputs(t)
	for _, name := range []string{"json", "source-grammar", "fields512"} {
		a := analyze(sources[name])
		if a.prog == nil {
			t.Fatalf("%s has no compiled grammar", name)
		}
		for _, operation := range []string{"documentSymbol", "hover", "completion"} {
			msg := renderBenchmarkRequest(a, "textDocument/"+operation)
			for _, rich := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				result, err := renderBenchmarkServer(a, ctx, rich).request(msg)
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				data, jerr := json.Marshal(result)
				if jerr != nil {
					t.Fatal(jerr)
				}
				t.Logf("response %s/%s/%t %x", name, operation, rich, sha256.Sum256(data))
			}
		}
	}
}
