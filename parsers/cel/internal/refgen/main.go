// Command refgen writes the reference results of the tests of package cel: the AST (or the error) that cel-go
// returns for a set of expressions, which testdata/ref/*.tsv vendors so that the tests need no cel-go.
//
// It is a module of its own because it depends on cel-go. Run it from this directory:
//
//	go run . -spec /path/to/cel-spec -out ../../testdata/ref
//
// It reads the expressions of the conformance tests from cel-spec (tests/simple/testdata/*.textproto), those in the
// tests of cel-go's parser, a list of hand-picked edge cases and random expressions, parses each with cel-go
// (see canon.go for the settings and the form of the output) and writes one file per set. A line of a file is the
// expression as a Go string literal, a tab, and the canonical form of its AST or ERR.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	spec := flag.String("spec", "", "directory of a checkout of github.com/google/cel-spec")
	out := flag.String("out", "../../testdata/ref", "output directory")
	nrandom := flag.Int("n", 12000, "number of random expressions")
	nlit := flag.Int("literals", 4000, "number of random literals")
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
	write := func(name string, inputs []string, verdict ...bool) {
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
			lines = append(lines, strconv.Quote(src)+"\t"+res)
		}
		sort.Strings(lines)
		path := filepath.Join(*out, name)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("%-16s %6d inputs: %6d accepted, %6d rejected\n", name, len(lines), ok, bad)
	}

	conf, err := conformanceExprs(*spec)
	if err != nil {
		fatal(err)
	}
	write("conformance.tsv", conf)
	celgo, err := celGoTestInputs(celGoParserDir())
	if err != nil {
		fatal(err)
	}
	write("celgo.tsv", celgo)
	write("edge.tsv", edgeCases)
	write("random.tsv", generate(*seed, *nrandom))
	write("literals.tsv", generateLiterals(*seed, *nlit))
	write("deep.tsv", generateDeep(*seed, *ndeep), true)
	writeLimits(p, filepath.Join(*out, "limits.tsv"))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "refgen:", err)
	os.Exit(1)
}
