package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/editfuzz"
)

const editFuzzGrammar = "@start\nrule main = (word / other)* $$\nrule word = [a-z]+ #atomic\nrule other = .\n"

func TestEditFuzz(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.zp")
	os.WriteFile(g, []byte(editFuzzGrammar), 0o644)
	var out strings.Builder
	if err := run([]string{"edit-fuzz", "-g", g, "-sequences", "3", "-operations", "8"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	want := "ok closure/codepoints: 3 sequences\nok closure/bytes: 3 sequences\nok bytecode/codepoints: 3 sequences\n" +
		"ok bytecode/bytes: 3 sequences\nok bytecode-iterative/codepoints: 3 sequences\nok bytecode-iterative/bytes: 3 sequences\n"
	if out.String() != want {
		t.Fatalf("got:\n%s", out.String())
	}

	corpus := filepath.Join(dir, "corpus")
	os.Mkdir(corpus, 0o755)
	os.WriteFile(filepath.Join(corpus, "b"), []byte("x\xff"), 0o644)
	os.WriteFile(filepath.Join(corpus, "a"), []byte("é!"), 0o644)
	os.Mkdir(filepath.Join(corpus, "sub"), 0o755)
	os.Symlink(filepath.Join(corpus, "a"), filepath.Join(corpus, "c"))
	os.WriteFile(filepath.Join(corpus, ".hidden"), []byte("no"), 0o644)
	out.Reset()
	if err := run([]string{"edit-fuzz", "-g", g, "-backend", "bytecode-iterative", "-unit", "bytes", "-corpus", corpus, "-seed", "4"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok bytecode-iterative/bytes: 100 sequences\n" {
		t.Fatalf("got:\n%s", out.String())
	}
	texts, err := readCorpus(corpus)
	if err != nil || len(texts) != 3 || string(texts[0]) != "é!" || string(texts[1]) != "x\xff" || string(texts[2]) != "é!" {
		t.Fatalf("corpus %q, %v", texts, err)
	}
}

func TestEditFuzzReplay(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.zp")
	os.WriteFile(g, []byte(editFuzzGrammar), 0o644)
	seq := &editfuzz.Sequence{GrammarSHA256: editfuzz.GrammarHash([]byte(editFuzzGrammar)), Start: "main",
		Backend: zeroparse.Bytecode, Unit: zeroparse.Bytes, Initial: []byte("ab"), Operations: []editfuzz.Operation{
			{Kind: editfuzz.Edit, Start: 2, End: 2, Text: []byte("\xc3")}, {Kind: editfuzz.Checkpoint},
			{Kind: editfuzz.Edit, Start: 3, End: 3, Text: []byte("\xa9")}, {Kind: editfuzz.Undo}, {Kind: editfuzz.Checkpoint}}}
	path, err := editfuzz.Save(dir, seq)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"edit-fuzz", "-g", g, "-replay", path}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok "+path+": 5 operations\n" {
		t.Fatalf("got %q", out.String())
	}
	// A passing sequence cannot be minimized.
	if err := run([]string{"edit-fuzz", "-g", g, "-replay", path, "-minimize"}, nil, &out); err == nil || !strings.Contains(err.Error(), "does not fail") {
		t.Fatalf("got %v", err)
	}
	other := filepath.Join(dir, "other.zp")
	os.WriteFile(other, []byte(editFuzzGrammar+"\n"), 0o644)
	if err := run([]string{"edit-fuzz", "-g", other, "-replay", path}, nil, &out); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("other grammar: %v", err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	raw["version"] = 2
	data, _ = json.Marshal(raw)
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, data, 0o644)
	if err := run([]string{"edit-fuzz", "-g", g, "-replay", bad}, nil, &out); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("version 2: %v", err)
	}
}

func TestEditFuzzErrors(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.zp")
	os.WriteFile(g, []byte(editFuzzGrammar), 0o644)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "-g is required"},
		{[]string{"-g", g, "x"}, "unexpected arguments"},
		{[]string{"-g", filepath.Join(dir, "none.zp")}, "no such file"},
		{[]string{"-g", g, "-backend", "default"}, "unknown backend"},
		{[]string{"-g", g, "-unit", "rune"}, "unknown unit"},
		{[]string{"-g", g, "-sequences", "-1"}, "negative"},
		{[]string{"-g", g, "-s", "nope"}, "nope"},
		{[]string{"-g", g, "-corpus", filepath.Join(dir, "none")}, "no such file"},
		{[]string{"-g", g, "-corpus", t.TempDir()}, "no files"},
		{[]string{"-g", g, "-replay", filepath.Join(dir, "none.json")}, "no such file"},
		{[]string{"-g", g, "-minimize"}, "-minimize needs -save-failures"},
		{[]string{"-g", g, "-replay", "x.json", "-backend", "closure", "-seed", "2"}, "-replay does not take -backend, -seed"},
		{[]string{"-g", g, "-replay", "x.json", "-save-failures", dir}, "only with -minimize"},
	} {
		var out strings.Builder
		if err := run(append([]string{"edit-fuzz"}, tc.args...), nil, &out); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want an error containing %q", tc.args, err, tc.want)
		}
	}
}
