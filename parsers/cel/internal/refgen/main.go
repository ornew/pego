// Command refgen writes the reference results of the tests of package cel: the AST (or the error) that cel-go
// returns for a set of expressions, which testdata/ref/*.tsv vendors so that the tests need no cel-go.
//
// It is a module of its own because it depends on cel-go. Run it from this directory:
//
//	go run . -spec /path/to/cel-spec -out ../../testdata/ref
//
// where cel-spec is the checkout of google/cel-spec at the revision of the manifest, or the fetched corpus (print its
// directory with "go run ./hack/corpora dir cel-spec" in the root of the repository).
//
// It reads the expressions of the conformance tests from cel-spec (tests/simple/testdata/*.textproto), those in the
// tests of cel-go's parser, a list of hand-picked edge cases and random expressions, parses each with cel-go
// (see canon.go for the settings and the form of the output) and writes one file per set. A line of a file is the
// expression as a Go string literal, a tab, and the canonical form of its AST or ERR. In the files of expressions from
// cel-spec and cel-go (conformance.tsv, celgo.tsv and macros.tsv) the expression is replaced by its hash
// (upstream.Hash), so that the files hold no text of those projects.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	upstreampkg "github.com/ornew/zeroparse/parsers/cel/internal/upstream"
)

func main() {
	spec := flag.String("spec", "", "directory of a checkout of github.com/google/cel-spec")
	out := flag.String("out", "../../testdata/ref", "output directory")
	nrandom := flag.Int("n", 12000, "number of random expressions")
	nlit := flag.Int("literals", 4000, "number of random literals")
	nmacros := flag.Int("macros", 6000, "number of random macro calls")
	ndeep := flag.Int("deep", 2000, "number of deeply nested expressions")
	seed := flag.Uint64("seed", 1, "seed of the random generator")
	flag.Parse()
	if *spec == "" {
		fmt.Fprintln(os.Stderr, "refgen: -spec is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	p := newParser()
	// A file of verdicts (verdict true) says OK for an expression that cel-go accepts, instead of its AST.
	// A file that holds expressions of cel-spec or of the tests of cel-go (upstream) refers to those by their hash
	// instead of holding their text, so that the text is not stored; the tests find it in the fetched sources.
	upstreamTexts := map[string]bool{}
	write := func(name string, inputs []string, upstream bool, verdict ...bool) {
		seen := map[string]bool{}
		var lines []string
		ok, bad := 0, 0
		for _, src := range inputs {
			if seen[src] {
				continue
			}
			seen[src] = true
			res, good := reference(p, src)
			if !good {
				continue // cel-go panicked: not a result
			}
			if strings.HasPrefix(res, "ERR") {
				bad++
			} else {
				ok++
				if len(verdict) > 0 && verdict[0] {
					res = "OK"
				}
			}
			key := strconv.Quote(src)
			if upstream && upstreamTexts[src] {
				key = upstreampkg.Hash(src)
			}
			lines = append(lines, key+"\t"+res)
		}
		sort.Strings(lines)
		path := filepath.Join(*out, name)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("%-16s %6d inputs: %6d accepted, %6d rejected\n", name, len(lines), ok, bad)
	}

	conf, err := upstreampkg.ConformanceExprs(*spec)
	if err != nil {
		fatal(err)
	}
	celgo, err := upstreampkg.GoTestInputs(celGoParserDir())
	if err != nil {
		fatal(err)
	}
	for _, src := range slices.Concat(conf, celgo) {
		upstreamTexts[src] = true
	}
	write("conformance.tsv", conf, true)
	write("celgo.tsv", celgo, true)
	write("edge.tsv", edgeCases, false)
	write("random.tsv", generate(*seed, *nrandom), false)
	write("literals.tsv", generateLiterals(*seed, *nlit), false)
	write("deep.tsv", generateDeep(*seed, *ndeep), false, true)
	// The verdicts of the parser with the standard macros, for what the other files hold and for macro calls.
	pm := newMacroParser()
	macroInputs := slices.Concat(conf, celgo, edgeCases, generateMacros(*seed, *nmacros), generate(*seed+1, 3000))
	writeLimits(p, filepath.Join(*out, "limits.tsv"))
	p = pm
	write("macros.tsv", macroInputs, true, true)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "refgen:", err)
	os.Exit(1)
}
