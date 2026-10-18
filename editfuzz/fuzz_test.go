package editfuzz

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func generated(t *testing.T, src string, opts Options) (seqs []string, err error) {
	t.Helper()
	sequenceDone = func(seq *Sequence) {
		data, err := json.Marshal(seq)
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, string(data))
	}
	defer func() { sequenceDone = nil }()
	return seqs, Fuzz([]byte(src), opts)
}

func TestFuzzIsDeterministic(t *testing.T) {
	opts := Options{Start: "main", Backend: pego.Bytecode, Unit: pego.Bytes, Seed: 7, Sequences: 5, Operations: 40}
	a, err := generated(t, testGrammars[1], opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := generated(t, testGrammars[1], opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 5 || !reflect.DeepEqual(a, b) {
		t.Fatalf("the same seed generated different sequences:\n%v\n%v", a, b)
	}
	opts.Seed++
	if c, _ := generated(t, testGrammars[1], opts); reflect.DeepEqual(a, c) {
		t.Fatal("another seed generated the same sequences")
	}
	kinds := map[Kind]int{}
	for _, s := range a {
		var seq Sequence
		if err := json.Unmarshal([]byte(s), &seq); err != nil {
			t.Fatal(err)
		}
		for _, op := range seq.Operations {
			kinds[op.Kind]++
		}
		if last := seq.Operations[len(seq.Operations)-1]; last.Kind != Checkpoint {
			t.Fatalf("sequence ends with %v", last.Kind)
		}
	}
	for _, k := range []Kind{Edit, Undo, Redo, Checkpoint} {
		if kinds[k] == 0 {
			t.Errorf("no %s operations in %v", k, kinds)
		}
	}
}

// TestFuzzGrammars fuzzes the test grammars on every backend and unit, from generated texts and from a
// corpus.
func TestFuzzGrammars(t *testing.T) {
	sequences := 40
	if testing.Short() {
		sequences = 5
	}
	for gi, src := range testGrammars {
		for _, b := range backends {
			for _, u := range units {
				for _, corpus := range [][][]byte{nil, {[]byte("let a=(1+2)+3;"), []byte("a+a+a"), []byte("wörd! \xff")}} {
					opts := Options{Start: "main", Backend: b, Unit: u, Seed: uint64(gi), Sequences: sequences, Corpus: corpus}
					if err := Fuzz([]byte(src), opts); err != nil {
						t.Errorf("grammar %d %v %v: %v", gi, b, u, err)
					}
				}
			}
		}
	}
}

func TestFuzzUsesCorpus(t *testing.T) {
	corpus := [][]byte{[]byte("ab"), []byte("\xffc")}
	seqs, err := generated(t, testGrammars[0], Options{Start: "main", Seed: 3, Sequences: 20, Corpus: corpus})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	joined := 0
	for _, s := range seqs {
		var seq Sequence
		json.Unmarshal([]byte(s), &seq)
		if len(seq.Initial) < largeText {
			seen[string(seq.Initial)] = true
			continue
		}
		// A joined text consists of corpus texts.
		joined++
		if rest := strings.ReplaceAll(strings.ReplaceAll(string(seq.Initial), "ab", ""), "\xffc", ""); rest != "" {
			t.Fatalf("joined initial text with %q", rest)
		}
	}
	if len(seen) != 2 || !seen["ab"] || !seen["\xffc"] || joined == 0 {
		t.Fatalf("initial texts %v, %d joined", seen, joined)
	}
}

// TestFuzzSavesFailures injects a mismatch and checks the saved sequence: it replays to the same
// mismatch in a new replay, and saving it again names the same file.
func TestFuzzSavesFailures(t *testing.T) {
	defer func() { corruptDocumentResult = nil }()
	corruptDocumentResult = func(text string, tree map[string]any) {
		if strings.Contains(text, "é") {
			tree["error"] = errorTree(errors.New("injected"))
		}
	}
	for _, u := range units {
		dir := filepath.Join(t.TempDir(), "failures")
		src := []byte(testGrammars[2])
		err := Fuzz(src, Options{Start: "main", Backend: pego.BytecodeIterative, Unit: u, Seed: 1, SaveFailures: dir})
		var f *Failure
		var m *Mismatch
		if !errors.As(err, &f) || !errors.As(err, &m) || f.Path == "" || !strings.Contains(err.Error(), f.Path) {
			t.Fatalf("%v: got %v", u, err)
		}
		seq, err := Load(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(seq, f.Sequence) {
			t.Fatalf("%v: saved %+v, failed %+v", u, seq, f.Sequence)
		}
		var again *Mismatch
		if err := Replay(src, seq); !errors.As(err, &again) || again.Fingerprint() != m.Fingerprint() || again.Event != m.Event || again.Text != m.Text {
			t.Fatalf("%v: replay gave %v, want %v", u, err, m)
		}
		if path, err := Save(dir, seq); err != nil || path != f.Path {
			t.Fatalf("%v: saved again to %s, %v", u, path, err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Fatalf("%v: %d files in the failure directory", u, len(entries))
		}
	}
}

func TestFuzzErrors(t *testing.T) {
	for _, tc := range []struct {
		src  string
		opts Options
		want string
	}{
		{testGrammars[0], Options{}, "no start rule"},
		{testGrammars[0], Options{Start: "nope"}, "nope"},
		{"rule main =", Options{Start: "main"}, ""},
		{testGrammars[0], Options{Start: "main", Backend: pego.Backend(9)}, "unknown backend"},
		{testGrammars[0], Options{Start: "main", Unit: pego.PositionUnit(7)}, "unknown unit"},
	} {
		if err := Fuzz([]byte(tc.src), tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: got %v", tc.opts, err)
		}
	}
	// Without a failure directory, the message holds the sequence.
	defer func() { corruptDocumentResult = nil }()
	corruptDocumentResult = func(text string, tree map[string]any) { tree["error"] = errorTree(errors.New("injected")) }
	err := Fuzz([]byte(testGrammars[0]), Options{Start: "main", Minimize: true})
	var f *Failure
	if !errors.As(err, &f) || f.Path != "" || f.Minimized == nil || !strings.Contains(err.Error(), "\nsequence: {\"version\":1,") ||
		!strings.Contains(err.Error(), "\nminimized to 0 operations: {") {
		t.Errorf("got %v", err)
	}
	corruptDocumentResult = nil
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o644)
	if _, err := Save(filepath.Join(file, "dir"), sequence("g", pego.Closure, pego.Bytes, "")); err == nil {
		t.Error("saved under a file")
	}
}

// TestSaveNeverReplaces checks that Save does not replace a file with other contents and writes readable
// files.
func TestSaveNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	seq := sequence("g", pego.Closure, pego.Bytes, "a", checkpoint)
	path, err := Save(dir, seq)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("saved file: %v, %v", info.Mode(), err)
	}
	os.WriteFile(path, []byte("edited"), 0o644)
	if _, err := Save(dir, seq); err == nil || !strings.Contains(err.Error(), "other contents") {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "edited" {
		t.Fatalf("file replaced with %q", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files left", len(entries))
	}
}
