package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const occurrenceURI = "file:///occurrences.pego"

var occurrenceBenchmarkResult any

func occurrenceSource(count int) string {
	return "// Unicode positions: 😀\r\nrule atom = \"a\"\r\nrule main = " + strings.Repeat("atom ", count) + "\r\n"
}

func occurrenceServer(a *analysis, ctx context.Context) *Server {
	return &Server{docs: map[string]*document{occurrenceURI: {uri: occurrenceURI, text: a.text, an: a}}, state: stateInitialized, requestContext: ctx}
}

func occurrenceMessage(method string, position Position, include bool, newName string) *incoming {
	params := map[string]any{"textDocument": map[string]any{"uri": occurrenceURI}, "position": position}
	if method == "references" {
		params["context"] = map[string]any{"includeDeclaration": include}
	}
	if method == "rename" {
		params["newName"] = newName
	}
	data, _ := json.Marshal(params)
	return &incoming{Method: "textDocument/" + method, Params: data}
}

func BenchmarkOccurrenceRequests(b *testing.B) {
	for _, count := range []int{32, 4096} {
		a := analyze(occurrenceSource(count))
		if a.prog == nil {
			b.Fatal("fixture did not compile")
		}
		for _, operation := range []struct{ name, method, newName string }{
			{"references", "references", ""},
			{"documentHighlight", "documentHighlight", ""},
			{"prepareRename", "prepareRename", ""},
			{"rename", "rename", "renamed"},
			{"rename-long", "rename", strings.Repeat("x", 65536)},
		} {
			msg := occurrenceMessage(operation.method, Position{Line: 1, Character: 5}, true, operation.newName)
			for _, active := range []bool{false, true} {
				b.Run(fmt.Sprintf("refs%d/%s/active=%t", count, operation.name, active), func(b *testing.B) {
					var ctx context.Context
					if active {
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(context.Background())
						defer cancel()
					}
					s := occurrenceServer(a, ctx)
					b.ReportAllocs()
					for b.Loop() {
						var err *rpcError
						occurrenceBenchmarkResult, err = s.request(msg)
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

type occurrenceResponseCase struct {
	name string
	a    *analysis
	msg  *incoming
}

// Use only the existing request API so this corpus can also run at the parent
// revision. Both success values and exact error responses are fingerprinted.
func occurrenceResponseCorpus() []occurrenceResponseCase {
	src := "type T terminal\ntype U terminal\nrule atom T = \"a\"\nrule other = \"b\"\nrule main = atom atom\n"
	a := analyze(src)
	var cases []occurrenceResponseCase
	add := func(name string, a *analysis, method string, line, char int, include bool, newName string) {
		cases = append(cases, occurrenceResponseCase{name, a, occurrenceMessage(method, Position{Line: line, Character: char}, include, newName)})
	}
	for _, method := range []string{"references", "documentHighlight", "prepareRename", "rename"} {
		add(method+"/declaration", a, method, 2, 6, true, "renamed")
		add(method+"/reference", a, method, 4, 13, true, "renamed")
		add(method+"/type", a, method, 0, 6, true, "Renamed")
		add(method+"/no-target", a, method, 5, 0, true, "renamed")
	}
	add("references/no-declaration", a, "references", 2, 6, false, "")
	for _, name := range []string{"atom", "other", "a-b", "_", "_reserved", "名前", "", "rule"} {
		add("rename/rule/"+name, a, "rename", 2, 6, true, name)
	}
	for _, name := range []string{"T", "U", "small", "Match", "_reserved"} {
		add("rename/type/"+name, a, "rename", 0, 6, true, name)
	}
	for _, method := range []string{"prepareRename", "rename"} {
		add(method+"/builtin", analyze("type T = { M Match }\nrule a = \"a\"\n"), method, 0, 14, true, "Renamed")
		add(method+"/undefined", analyze("rule a = missing\n"), method, 0, 10, true, "renamed")
		add(method+"/syntax-error", analyze("rule a = b }\nrule b = ("), method, 0, 5, true, "renamed")
	}
	add("rename/used-undefined-rule", analyze("rule atom = missing\n"), "rename", 0, 6, true, "missing")
	add("rename/used-undefined-type", analyze("type T ~ Missing\nrule a = \"a\"\n"), "rename", 0, 6, true, "Missing")
	for _, method := range []string{"references", "documentHighlight", "prepareRename", "rename"} {
		cases = append(cases, occurrenceResponseCase{method + "/missing-params", a, &incoming{Method: "textDocument/" + method}})
		msg := occurrenceMessage(method, Position{}, true, "renamed")
		msg.Params = json.RawMessage(strings.ReplaceAll(string(msg.Params), occurrenceURI, "file:///unknown.pego"))
		cases = append(cases, occurrenceResponseCase{method + "/unknown-document", a, msg})
	}

	return cases
}

func occurrenceResponseJSON(tb testing.TB, value any, err *rpcError) []byte {
	tb.Helper()
	data, jerr := json.Marshal(struct {
		Result any
		Error  *rpcError
	}{value, err})
	if jerr != nil {
		tb.Fatal(jerr)
	}
	return data
}

func TestOccurrenceResponseFingerprints(t *testing.T) {
	for _, tc := range occurrenceResponseCorpus() {
		ctx, cancel := context.WithCancel(context.Background())
		value, err := occurrenceServer(tc.a, ctx).request(tc.msg)
		cancel()
		t.Logf("response %s %x", tc.name, sha256.Sum256(occurrenceResponseJSON(t, value, err)))
	}
}
