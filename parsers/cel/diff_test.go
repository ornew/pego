package cel_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/cel"
)

// refCase is a line of testdata/ref/*.tsv: an expression and what cel-go makes of it (see internal/refgen).
type refCase struct {
	src, want string
}

func readRef(t testing.TB, name string) []refCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "ref", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []refCase
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		q, want, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			t.Fatalf("%s: bad line %.60q", name, sc.Text())
		}
		src, err := strconv.Unquote(q)
		if err != nil {
			t.Fatalf("%s: bad line %.60q: %v", name, sc.Text(), err)
		}
		cases = append(cases, refCase{src, want})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s: no cases", name)
	}
	return cases
}

// result parses src as ParseExpr does and returns the canonical form of the expression, or ERR.
func result(src string) string {
	e, err := cel.ParseExpr(src)
	if err != nil {
		return "ERR"
	}
	return canonical(src, e)
}

// TestDifferential compares the AST of every expression of testdata/ref/*.tsv with the one cel-go returns for it
// (and the rejected ones with the errors of cel-go): the conformance tests of cel-spec, the cases of cel-go's own
// parser tests, hand-picked edge cases, and random expressions and literals.
func TestDifferential(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "ref", "*.tsv"))
	for _, f := range files {
		name := filepath.Base(f)
		if name == "limits.tsv" {
			continue
		}
		t.Run(strings.TrimSuffix(name, ".tsv"), func(t *testing.T) {
			bad := 0
			for _, c := range readRef(t, name) {
				got := result(c.src)
				if c.want == "OK" && got != "ERR" { // a file of verdicts: the tree is not compared
					got = "OK"
				}
				if got != c.want {
					bad++
					if bad <= 12 {
						t.Errorf("%q\n got  %s\n want %s", c.src, got, c.want)
					}
				}
			}
			if bad > 12 {
				t.Errorf("%d differences in all", bad)
			}
		})
	}
}
