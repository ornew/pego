package gentest

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// GrammarCases returns the ten checked-in grammars with the inputs of their testdata directories (the .txt
// files of up to 3 KB, at most six each).
func GrammarCases(t *testing.T) []Case {
	t.Helper()
	var cases []Case
	for _, name := range []string{"json", "cel", "cue", "duckdb", "golang", "python", "xml", "yaml", "csv", "typescript"} {
		src, err := os.ReadFile(path(t, "../../parsers/"+name+"/"+name+".pego"))
		if err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(path(t, "../../parsers/"+name+"/testdata/*.txt"))
		sort.Strings(files)
		c := Case{Name: name, Src: string(src)}
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil && len(b) <= 3<<10 && len(c.Inputs) < 6 {
				c.Inputs = append(c.Inputs, string(b))
			}
		}
		c.Inputs = append(c.Inputs, GrammarOperatorInputs[name]...)
		if len(c.Inputs) == 0 {
			t.Fatalf("%s: no inputs in testdata", name)
		}
		cases = append(cases, c)
	}
	return cases
}

// GrammarOperatorInputs are short inputs that use many operators of the grammars with an Operators Rule, so
// that the guards of the choices of operator tokens skip some alternatives and the lines match others.
var GrammarOperatorInputs = map[string][]string{
	"duckdb":     {"SELECT a BETWEEN 1 + 2 AND 3 - 4, b BETWEEN 7 / 8 % 9 AND 5 * 6, c BETWEEN 1 AND 2 // 3, d BETWEEN 4 ^ 5 AND 6 ** 7;\n", "SELECT 1 / 2, 3 % 4, 5 * 6, 7 - 8, 9 + 10, 2 ^ 3, 6 // 4, 1 < 2, 1 >= 2, a AND b OR NOT c;\n"},
	"cel":        {"1 / 2 % 3 * 4 - 5 + 6 < 7 && 8 >= 9 || !x", "a ? b : c", "-x + y * z"},
	"cue":        {"x: 1 / 2 * 3 - 4 + 5 & !true | 6 < 7 && 8 || 9\n"},
	"golang":     {"package p\nfunc f() {\n\tx = a / b * c % d - e + f << 1 >> 2 & g | h ^ i &^ j && !k || l < m\n}\n"},
	"python":     {"x = a / b * c % d - e + f ** 2 // 3 << 1 >> 2 & g | h ^ i and not j or k < l\n"},
	"typescript": {"x = a / b * c % d - e + f ** 2 << 1 >> 2 >>> 3 & g | h ^ i && !j || k < l ?? m;\n"},
}
