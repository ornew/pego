package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const semanticURI = "file:///semantic.pego"

var semanticBenchmarkResult any

func semanticServer(a *analysis, ctx context.Context) *Server {
	return &Server{docs: map[string]*document{semanticURI: {uri: semanticURI, text: a.text, an: a}}, state: stateInitialized, requestContext: ctx}
}

func semanticMessage() *incoming {
	params, _ := json.Marshal(docParams(semanticURI))
	return &incoming{Method: "textDocument/semanticTokens/full", Params: params}
}

func semanticDenseSource(count int) string {
	return "rule atom = \"a\"\nrule main = " + strings.Repeat("atom ", count) + "\n"
}

func semanticOperatorsSource(count int) string {
	var out strings.Builder
	out.WriteString("rule expr = operators {\n operand \"a\"\n level {\n")
	for i := range count {
		out.WriteString(" infix ")
		out.WriteString([]string{"left", "right", "none"}[i%3])
		out.WriteString(" \"+\"\n")
	}
	return out.String() // Incomplete syntax intentionally exercises fallback coloring.
}

type semanticSourceCase struct{ name, text string }

func semanticRepresentativeSources(tb testing.TB) []semanticSourceCase {
	tb.Helper()
	var sources []semanticSourceCase
	for _, input := range []struct{ name, path string }{{"json", "../../parsers/json/json.pego"}, {"source-grammar", "../../pego.pego"}} {
		data, err := os.ReadFile(input.path)
		if err != nil {
			tb.Fatal(err)
		}
		sources = append(sources, semanticSourceCase{input.name, string(data)})
	}
	return sources
}

func BenchmarkSemanticRequests(b *testing.B) {
	sources := append(semanticRepresentativeSources(b),
		semanticSourceCase{"dense2048", semanticDenseSource(2048)},
		semanticSourceCase{"comment4096", "/*😀\r\n" + strings.Repeat("content😀\r\n\r\n", 2048) + "*/\r\n"},
		semanticSourceCase{"incomplete-operators128", semanticOperatorsSource(128)},
	)
	for _, fixture := range sources {
		a := analyze(fixture.text)
		msg := semanticMessage()
		for _, active := range []bool{false, true} {
			mode := "ordinary"
			if active {
				mode = "cancellable"
			}
			b.Run(fixture.name+"/"+mode, func(b *testing.B) {
				var ctx context.Context
				if active {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(context.Background())
					defer cancel()
				}
				s := semanticServer(a, ctx)
				b.ReportAllocs()
				for b.Loop() {
					var err *rpcError
					semanticBenchmarkResult, err = s.request(msg)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func semanticResponseSources(tb testing.TB) []semanticSourceCase {
	return append(semanticRepresentativeSources(tb), []semanticSourceCase{
		{"empty", ""},
		{"punctuation", "() {} => ... / !"},
		{"mixed", "type N terminal // c\nrule ab = x:n \"s😀\" [a] #stream => P{F: len($x).startPos}\nrule n N = operators {\n operand a\n level { infix left \"+\" }\n}\nrule bind = {v := 1}"},
		{"comment-lf", "/*😀\n\nsecond\n*/\nrule main = \"x\""},
		{"comment-crlf", "/*😀\r\n\r\nsecond\r\n*/\r\nrule main = \"x\""},
		{"comment-cr", "/*😀\r\rsecond\r*/\rrule main = \"x\""},
		{"unterminated-comment", "/*😀\r\n\r\nunfinished"},
		{"dense128", semanticDenseSource(128)},
		{"incomplete-operators64", semanticOperatorsSource(64)},
		{"no-rule-associativity", "infix /* one */ /* two */ left"},
		{"attributes-actions", "type Foo struct { Value Match }\nrule main = {x := 1} a:[a-z]+ @error(message=\"letter\") => Foo{Value: $a}"},
	}...)
}

// This corpus uses only the existing request API and can run unchanged against
// the parent revision, including partial grammar and exact RPC error responses.
func TestSemanticResponseFingerprints(t *testing.T) {
	for _, fixture := range semanticResponseSources(t) {
		a := analyze(fixture.text)
		ctx, cancel := context.WithCancel(context.Background())
		value, err := semanticServer(a, ctx).request(semanticMessage())
		cancel()
		t.Logf("response %s %x", fixture.name, sha256.Sum256(occurrenceResponseJSON(t, value, err)))
	}
	a := analyze("")
	for _, name := range []string{"missing-params", "unknown-document"} {
		msg := semanticMessage()
		if name == "missing-params" {
			msg.Params = nil
		} else {
			msg.Params = json.RawMessage(strings.ReplaceAll(string(msg.Params), semanticURI, "file:///unknown.pego"))
		}
		value, err := semanticServer(a, nil).request(msg)
		t.Logf("response %s %x", name, sha256.Sum256(occurrenceResponseJSON(t, value, err)))
	}
}
