package cel_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/cel"
)

// FuzzParse checks the properties of the parser on arbitrary input (go test -fuzz FuzzParse): ParseAST, Parse and
// Recognize accept the same inputs and in either position unit, the spans of the values of an accepted input are
// consistent, and what Format prints for it parses to the same expression.
//
// Unlike the differential test, it needs no cel-go; the seeds are the expressions of the conformance tests and of the
// reference corpora.
func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "cel-spec", "*.textproto"))
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		for _, src := range textprotoExprs(f, string(data)) {
			f.Add(src)
		}
	}
	for i, src := range refSources(f) {
		if i%7 == 0 {
			f.Add(src)
		}
	}
	nolimits := cel.WithLimits(cel.Limits{})
	f.Fuzz(func(t *testing.T, src string) {
		e, aerr := cel.ParseAST(src)
		rerr := cel.Recognize(src)
		_, nerr := cel.Parse(src)
		_, berr := cel.ParseAST(src, cel.Bytes)
		if (aerr == nil) != (rerr == nil) || (aerr == nil) != (nerr == nil) || (aerr == nil) != (berr == nil) {
			t.Fatalf("ParseAST: %v, Recognize: %v, Parse: %v, in bytes: %v", aerr, rerr, nerr, berr)
		}
		if aerr != nil {
			return
		}
		if utf8.ValidString(src) {
			checkSpans(t, src, e, false)
			if b, err := cel.ParseAST(src, cel.Bytes); err == nil {
				checkSpans(t, src, b, true)
			}
		}
		if _, err := cel.ParseExpr(src, nolimits); err != nil {
			return // a literal out of range
		}
		out := cel.Format(e)
		e2, err := cel.ParseExpr(out, nolimits)
		if err != nil {
			t.Fatalf("formatted as %q: %v", out, err)
		}
		if a, b := stripPos.ReplaceAllString(canonical(src, e), ""), stripPos.ReplaceAllString(canonical(out, e2), ""); a != b {
			t.Fatalf("formatted as %q\n got  %s\n want %s", out, b, a)
		}
		if strings.Contains(out, "\x00") && !strings.Contains(src, "\x00") {
			t.Fatalf("formatted as %q", out)
		}
	})
}
