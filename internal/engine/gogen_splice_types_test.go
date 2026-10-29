package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// spliceTypeCases are grammars whose hidden rule calls itself, in every place that takes one
// element. Their typed values are checked against the node tree, which they must not lose data
// from.
func spliceTypeCases(t *testing.T) []genCase {
	t.Helper()
	files, _ := filepath.Glob("testdata/typed/splice_recursive_*.pego")
	if len(files) == 0 {
		t.Fatal("no recursive splice grammars")
	}
	var cases []genCase
	for _, gp := range files {
		src, err := os.ReadFile(gp)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(strings.TrimSuffix(gp, ".pego") + ".json")
		if err != nil {
			t.Fatal(err)
		}
		c := genCase{name: strings.TrimSuffix(filepath.Base(gp), ".pego"), src: string(src)}
		if err := json.Unmarshal(data, &c.inputs); err != nil {
			t.Fatalf("%s: %v", gp, err)
		}
		cases = append(cases, c)
	}
	return cases
}

var quoted = regexp.MustCompile(`"([^"\\]*)"`)

// leaves returns the strings of a value in order: the quoted tokens of a node tree, or the string
// values of the JSON of a typed value.
func jsonLeaves(v any, out *[]string) {
	switch v := v.(type) {
	case string:
		*out = append(*out, v)
	case []any:
		for _, e := range v {
			jsonLeaves(e, out)
		}
	case map[string]any:
		// Typed tuples are objects E0, E1, ...; order by the key, with a node's Value last.
		for i := 0; ; i++ {
			e, ok := v[fmt.Sprintf("E%d", i)]
			if !ok {
				break
			}
			jsonLeaves(e, out)
		}
		if e, ok := v["Value"]; ok {
			jsonLeaves(e, out)
		}
		// The fields of a struct have names; they follow.
		var fields []string
		for k := range v {
			if k != "Value" && k != "Start" && k != "End" && !tupleKey.MatchString(k) {
				fields = append(fields, k)
			}
		}
		sort.Strings(fields)
		for _, k := range fields {
			jsonLeaves(v[k], out)
		}
	}
}

var tupleKey = regexp.MustCompile(`^E\d+$`)

// spliceWantLeaves returns the strings the typed value of a grammar must hold given the tokens of
// the node tree. Most keep every token, in order; the grammar with an action holds the text of the
// part it captured, which is the input without the "!" that follows it.
func spliceWantLeaves(name, input string, tokens []string) []string {
	if name == "splice_recursive_capture_action" {
		return []string{strings.TrimSuffix(input, "!")}
	}
	return tokens
}

// A hidden rule that calls itself where it takes one element has the node of the call as the
// element. The typed values of every generated route keep the tokens of the node tree, in order.
func TestGeneratedRecursiveSpliceTypesKeepTheTree(t *testing.T) {
	eachMode(t, testGeneratedRecursiveSpliceTypesKeepTheTree)
}

func testGeneratedRecursiveSpliceTypesKeepTheTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	cases := spliceTypeCases(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("splicefix")), 0o644); err != nil {
		t.Fatal(err)
	}
	var imports, table strings.Builder
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		generatePackageIn(t, dir, "splicefix", fmt.Sprintf("g%d", i), g, gen.GenOptions{Start: "main", Types: true})
		fmt.Fprintf(&imports, "\tg%d \"splicefix/g%[1]d\"\n", i)
		fmt.Fprintf(&table, "\tfunc(s string) [4]string { n, e1 := g%d.Parse(s); v, e2 := g%[1]d.ParseAST(s); b, _ := json.Marshal(v); return [4]string{fmt.Sprint(n), string(b), fmt.Sprint(e1 != nil), fmt.Sprint(e2 != nil)} },\n", i)
	}
	driver := "package main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"os\"\n" + imports.String() + ")\n\n" +
		"var parsers = []func(string) [4]string{\n" + table.String() + "}\n\n" +
		`func main() {
	var inputs [][]string
	if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {
		panic(err)
	}
	var out [][][4]string
	for i, in := range inputs {
		var rows [][4]string
		for _, s := range in {
			rows = append(rows, parsers[i](s))
		}
		out = append(out, rows)
	}
	json.NewEncoder(os.Stdout).Encode(out)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	var inputs [][]string
	for _, c := range cases {
		inputs = append(inputs, c.inputs)
	}
	in, _ := json.Marshal(inputs)
	cmd := exec.Command("go", "run", "-trimpath", ".")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(string(in))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	var out [][][4]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		for j, s := range c.inputs {
			row := out[i][j]
			if row[2] != row[3] {
				t.Errorf("%s %q: Parse failed %s, ParseAST failed %s", c.name, s, row[2], row[3])
			}
			if row[2] == "true" {
				continue // no tree
			}
			var tokens []string
			for _, m := range quoted.FindAllStringSubmatch(row[0], -1) {
				tokens = append(tokens, m[1])
			}
			want := spliceWantLeaves(c.name, s, tokens)
			var v any
			if err := json.Unmarshal([]byte(row[1]), &v); err != nil {
				t.Fatalf("%s %q: %v", c.name, s, err)
			}
			var got []string
			jsonLeaves(v, &got)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("%s %q: the typed value has the tokens %q, the tree %s\n%s", c.name, s, got, row[0], row[1])
			}
		}
	}
}
