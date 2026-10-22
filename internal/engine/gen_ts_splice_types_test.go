package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The typed values of a TypeScript parser keep the tokens of its own node tree for a hidden rule that
// calls itself where it takes one element, as the Go ones do (TestGeneratedRecursiveSpliceTypesKeepTheTree).
// Comparing TypeScript with Go alone would not notice a type that both get wrong.
func TestGeneratedTSRecursiveSpliceTypesKeepTheTree(t *testing.T) {
	if testing.Short() {
		t.Skip("runs node")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	cases := spliceTypeCases(t)
	dir := t.TempDir()
	var imports, list strings.Builder
	var inputs [][]string
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		files, err := GenerateTS(g, GenOptions{Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		writeTSFiles(t, filepath.Join(dir, fmt.Sprintf("g%d", i)), files)
		fmt.Fprintf(&imports, "import * as g%d from \"./g%[1]d/parser.ts\";\n", i)
		fmt.Fprintf(&list, "g%d, ", i)
		inputs = append(inputs, c.inputs)
	}
	data, _ := json.Marshal(inputs)
	if err := os.WriteFile(filepath.Join(dir, "inputs.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	consumer := `import {readFileSync} from "node:fs";
` + imports.String() + `const parsers: any[] = [` + list.String() + `];
const inputs: string[][] = JSON.parse(readFileSync("inputs.json", "utf8"));
// The strings of a typed value in order: a node's value, a tuple's elements, a struct's fields by name.
const leaves = (v: any, out: string[]): string[] => {
  if (v === null || v === undefined) return out;
  if (typeof v === "string") out.push(v);
  else if (Array.isArray(v)) for (const e of v) leaves(e, out);
  else if (typeof v === "object") {
    if ("value" in v) leaves(v.value, out);
    else for (const k of Object.keys(v).sort()) if (k !== "start" && k !== "end" && k !== "type") leaves(v[k], out);
  }
  return out;
};
const rows = inputs.map((ins, i) => ins.map(s => {
  const n = parsers[i].parse(s), a = parsers[i].parseAST(s);
  return [n.node === null ? "" : String(n.node), leaves(a.value, []), n.error !== null, a.error !== null];
}));
console.log(JSON.stringify(rows));
`
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, nodeTSArgs("consumer.ts")...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var rows [][][4]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	checked := 0
	for i, c := range cases {
		for j, s := range c.inputs {
			row := rows[i][j]
			if row[2] != row[3] {
				t.Errorf("%s %q: parse failed %v, parseAST failed %v", c.name, s, row[2], row[3])
			}
			if row[2] == true {
				continue
			}
			var tokens, got []string
			for _, m := range quoted.FindAllStringSubmatch(row[0].(string), -1) {
				tokens = append(tokens, m[1])
			}
			for _, l := range row[1].([]any) {
				got = append(got, l.(string))
			}
			if want := spliceWantLeaves(c.name, s, tokens); strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("%s %q: the typed value has the strings %q, want %q (tree %s)", c.name, s, got, want, row[0])
			}
			checked++
		}
	}
	if checked < 15 {
		t.Errorf("only %d results compared", checked)
	}
}
