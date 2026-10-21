package engine

import (
	"regexp"
	"strings"

	"github.com/ornew/pego/grammar"
)

var (
	tsBundleImport = regexp.MustCompile(`(?m)^(?:import|export) (?:type )?(?:\*\s+as \w+|\{[^}]*\}(?: as \w+)?)(?: from "[^"]*")?;\n`)
	tsBundleHeader = regexp.MustCompile(`(?s)\A(?://[^\n]*\n|\n|/\* eslint-disable \*/\n)*`)
)

// generateTSBundle generates a TypeScript parser as one module, the way these tests have always run
// it: the three files in one scope (runtime, types, parser), without the imports between them. Test
// hooks append code that reaches the runtime's internals and parsers' own names this way. The tests
// of the real file split (gen_ts_split_test.go) compile and run the files as they are written.
func generateTSBundle(g *grammar.Grammar, opts GenOptions) ([]byte, error) {
	files, err := GenerateTS(g, opts)
	if err != nil {
		return nil, err
	}
	contents := map[string]string{}
	for _, f := range files {
		contents[f.Path] = string(f.Content)
	}
	var b strings.Builder
	b.WriteString(tsGeneratedHeader)
	b.WriteString("/* eslint-disable */\n\n")
	for _, name := range []string{"runtime.ts", "types.ts", "parser.ts"} {
		src, ok := contents[name]
		if !ok {
			continue // types.ts exists only with opts.Types
		}
		src = tsBundleHeader.ReplaceAllString(src, "")
		src = tsBundleImport.ReplaceAllString(src, "")
		switch name {
		case "types.ts":
			src = regexp.MustCompile(`\bRuntimeNode\b`).ReplaceAllString(src, "Node")
		case "parser.ts":
			src = regexp.MustCompile(`\bTypes\.`).ReplaceAllString(src, "")
		}
		b.WriteString("\n// --- " + name + " ---\n\n")
		b.WriteString(src)
	}
	return []byte(b.String()), nil
}
