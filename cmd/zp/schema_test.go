package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/schema"
)

const schemaGrammar = `type Pair = { Key node[string]; Value node[string] }

@start(default)
rule main = ws ps:pair+ $$ => $ps
@start
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-z]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
rule unused = "u"
`

func TestSchemaExport(t *testing.T) {
	g := writeFile(t, "g.zp", schemaGrammar)
	out, err := runCLI(t, "", "schema", "-g", g)
	if err != nil {
		t.Fatal(err)
	}
	s, err := zp.CompileSource(schemaGrammar, "main")
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	var got schema.Output
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not a valid schema: %v\n%s", err, out)
	}
	if got.Start != "main" || !strings.HasSuffix(out, "\n") {
		t.Errorf("start %q, output %q", got.Start, out)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(&got)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Errorf("CLI schema differs from the Go export:\n%s\n%s", gotJSON, wantJSON)
	}
	if strings.Contains(out, `"unused"`) || strings.Contains(out, `"inferred/unused"`) {
		t.Error("the default export includes a rule the start rule does not call")
	}

	// -all-rules includes it.
	all, err := runCLI(t, "", "schema", "-g", g, "-all-rules")
	if err != nil || !strings.Contains(all, `"name": "unused"`) {
		t.Errorf("-all-rules: %v\n%s", err, all)
	}

	// -s selects another start rule; the empty name selects the default.
	pairOut, err := runCLI(t, "", "schema", "-g", g, "-s", "pair")
	if err != nil || !strings.Contains(pairOut, `"start": "pair"`) {
		t.Errorf("-s pair: %v\n%s", err, pairOut)
	}
	def, err := runCLI(t, "", "schema", "-g", g, "-s", "")
	if err != nil || def != out {
		t.Errorf("empty -s: %v\n%s", err, def)
	}
	if _, err := runCLI(t, "", "schema", "-g", g, "-s", "absent"); err == nil {
		t.Error("unknown start rule accepted")
	}

	// -o writes the file instead of standard output.
	file := filepath.Join(t.TempDir(), "out.json")
	if out, err := runCLI(t, "", "schema", "-g", g, "-o", file); err != nil || out != "" {
		t.Errorf("-o: %q %v", out, err)
	}
	written, err := os.ReadFile(file)
	if err != nil || string(written) != out {
		t.Errorf("-o wrote %q, %v; want the standard output", written, err)
	}
}

func TestSchemaErrors(t *testing.T) {
	if _, err := runCLI(t, "", "schema"); err == nil || err.Error() != "-g is required" {
		t.Errorf("no grammar: %v", err)
	}
	g := writeFile(t, "g.zp", schemaGrammar)
	if _, err := runCLI(t, "", "schema", "-g", g, "extra.zp"); err == nil {
		t.Error("positional argument accepted")
	}
}

func TestSchemaDiff(t *testing.T) {
	before := writeFile(t, "before.zp", "@start\nrule main = [a-z]+ #atomic\n")
	after := writeFile(t, "after.zp", "@start\nrule main = [a-z]+ #atomic\nrule extra = \"x\"\n")
	out, err := runCLI(t, "", "schema", "diff", before, before)
	if err != nil || out != "no changes\n" {
		t.Errorf("identical grammars: %q %v", out, err)
	}
	// extra is unreachable from main, so the default export does not include it.
	out, err = runCLI(t, "", "schema", "diff", "-all-rules", before, after)
	if err != nil {
		t.Fatal(err)
	}
	if out != "added type inferred/extra: compatible (type-added)\nadded rule extra: compatible (rule-added)\n" {
		t.Errorf("text diff: %q", out)
	}
	jsonOut, err := runCLI(t, "", "schema", "diff", "-all-rules", "-f", "json", before, after)
	if err != nil {
		t.Fatal(err)
	}
	var doc schemaDiffOutput
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("%v\n%s", err, jsonOut)
	}
	if len(doc.Types) != 1 || len(doc.Rules) != 1 || doc.Rules[0].ID != "extra" || doc.Rules[0].Kind != schema.Added || doc.Rules[0].Before != nil {
		t.Errorf("rules: %+v", doc.Rules)
	}
	if doc.Rules[0].Class != schema.Compatible || len(doc.Rules[0].Impacts) != 1 || doc.Rules[0].Impacts[0] != (schemaImpactOutput{Path: "$", Class: schema.Compatible, Rule: schema.RuleRuleAdded}) {
		t.Errorf("rule class: %+v", doc.Rules[0])
	}
	if doc.Types == nil || doc.Rules == nil {
		t.Errorf("empty lists must be arrays: %s", jsonOut)
	}

	// A change to an existing type is classified with the paths of its impacts, in both formats.
	const declTail = "@start\nrule main P = k:key => P{Key: $k}\nrule key = [a-z]+ #atomic\n"
	declBefore := writeFile(t, "declbefore.zp", "type P = { Key node[string]; Old node[string] }\n"+strings.Replace(declTail, "$k}", "$k, Old: $k}", 1))
	declAfter := writeFile(t, "declafter.zp", "type P = { Key node[string]; New node[string] }\n"+strings.Replace(declTail, "$k}", "$k, New: $k}", 1))
	out, err = runCLI(t, "", "schema", "diff", declBefore, declAfter)
	if err != nil || !strings.Contains(out, "changed type declared/P: breaking (field-added at $.fields.New; field-removed at $.fields.Old)\n") {
		t.Errorf("classified text diff: %q %v", out, err)
	}
	jsonOut, err = runCLI(t, "", "schema", "diff", "-f", "json", declBefore, declAfter)
	if err != nil || !strings.Contains(jsonOut, `"class": "breaking"`) || !strings.Contains(jsonOut, `"rule": "field-removed"`) || !strings.Contains(jsonOut, `"path": "$.fields.Old"`) {
		t.Errorf("classified JSON diff: %s %v", jsonOut, err)
	}

	// A schema file is an operand too; its own start rule is used.
	schemaFile := filepath.Join(t.TempDir(), "before.json")
	if _, err := runCLI(t, "", "schema", "-g", before, "-o", schemaFile); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "", "schema", "diff", schemaFile, after)
	if err != nil || out != "no changes\n" {
		t.Errorf("default export of a grammar with an unreachable rule: %v\n%s", err, out)
	}
	out, err = runCLI(t, "", "schema", "diff", "-all-rules", schemaFile, after)
	if err != nil || !strings.Contains(out, "added rule extra") {
		t.Errorf("schema file operand: %v\n%s", err, out)
	}
	out, err = runCLI(t, "", "schema", "diff", schemaFile, before)
	if err != nil || out != "no changes\n" {
		t.Errorf("schema file against its grammar: %q %v", out, err)
	}

	bad := writeFile(t, "bad.json", `{"format":"zp-output-schema","version":1,"start":"main","positionUnitIndependent":true,"types":[],"rules":[]}`)
	if _, err := runCLI(t, "", "schema", "diff", bad, before); err == nil {
		t.Error("unsupported schema version accepted")
	}
	if _, err := runCLI(t, "", "schema", "diff", before); err == nil {
		t.Error("one operand accepted")
	}
	if _, err := runCLI(t, "", "schema", "diff", "-f", "yaml", before, after); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestSchemaFromCompiledGrammar(t *testing.T) {
	g := writeFile(t, "g.zp", schemaGrammar)
	want, err := runCLI(t, "", "schema", "-g", g, "-all-rules")
	if err != nil {
		t.Fatal(err)
	}
	if want == "" {
		t.Fatal("no schema from the source")
	}
	// A compiled grammar holds no schema in any mode: the schema comes from the source, also as
	// an operand of schema diff.
	dir := t.TempDir()
	for _, mode := range []string{"parse", "recognize", "document"} {
		path := filepath.Join(dir, mode+".zpc")
		if _, err := runCLI(t, "", "compile", "-g", g, "-mode", mode, "-o", path); err != nil {
			t.Fatal(err)
		}
		if _, err := runCLI(t, "", "schema", "-g", path, "-all-rules"); err == nil || !strings.Contains(err.Error(), "schema needs the grammar source") {
			t.Errorf("%s mode: %v", mode, err)
		}
	}
	for _, flag := range []string{"-no-ast", "-schema"} {
		if _, err := runCLI(t, "", "compile", "-g", g, flag, "-o", filepath.Join(dir, "x.zpc")); err == nil {
			t.Errorf("compile accepted %s", flag)
		}
	}
}
