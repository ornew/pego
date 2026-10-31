package editfuzz

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

// TestMinimize injects each mismatch category into Documents whose text contains "é" or the invalid
// byte "\xff", and checks that minimization keeps the fingerprint, shrinks the sequence to the
// essentials and leaves the original unchanged.
func TestMinimize(t *testing.T) {
	defer func() { corruptDocumentResult = nil }()
	src := testGrammars[2]
	for _, tc := range []struct {
		name    string
		corrupt func(map[string]any)
		path    string
	}{
		{"value", func(tree map[string]any) {
			if v, ok := tree["value"].(map[string]any); ok {
				v["end"] = -1
			}
		}, "value.end"},
		{"error", func(tree map[string]any) { tree["error"] = errorTree(errors.New("injected")) }, "error"},
	} {
		for _, u := range units {
			corruptDocumentResult = func(text string, tree map[string]any) {
				if strings.Contains(text, "é") {
					tc.corrupt(tree)
				}
			}
			ops := []Operation{edit(0, 0, "abc "), checkpoint, edit(4, 4, "x\xffyz"), checkpoint, undo, checkpoint, redo,
				edit(0, 1, "q"), checkpoint, edit(2, 2, "wordé!"), edit(0, 0, " "), checkpoint, checkpoint, edit(0, 1, "")}
			seq := sequence(src, zp.Closure, u, "start text", ops...)
			original := clone(seq)
			p, err := zp.CompileSource(src, "main")
			if err != nil {
				t.Fatal(err)
			}
			got, m, attempts, err := minimize(p, seq, DefaultMaxAttempts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(seq, original) {
				t.Fatalf("%s %v: the original changed", tc.name, u)
			}
			if m.Path != tc.path || attempts == 0 || attempts > DefaultMaxAttempts {
				t.Fatalf("%s %v: mismatch at %s after %d attempts", tc.name, u, m.Path, attempts)
			}
			// A single insertion of "é" into an empty text, then the checkpoint, remains.
			want := []Operation{edit(0, 0, "é"), checkpoint}
			if len(got.Initial) != 0 || !reflect.DeepEqual(got.Operations, want) {
				t.Fatalf("%s %v: minimized to %q %+v", tc.name, u, got.Initial, got.Operations)
			}
			var again *Mismatch
			if err := Replay([]byte(src), got); !errors.As(err, &again) || again.Fingerprint() != m.Fingerprint() {
				t.Fatalf("%s %v: minimized sequence replays to %v", tc.name, u, err)
			}
		}
	}
}

func TestMinimizeAttemptCeiling(t *testing.T) {
	defer func() { corruptDocumentResult = nil }()
	corruptDocumentResult = func(text string, tree map[string]any) {
		if strings.Contains(text, "b") {
			tree["error"] = errorTree(errors.New("injected"))
		}
	}
	src := testGrammars[0]
	seq := sequence(src, zp.Bytecode, zp.Bytes, "aaaa", checkpoint, edit(1, 2, "b"), checkpoint, edit(0, 0, "cc"), checkpoint)
	p, _ := zp.CompileSource(src, "main")
	for _, ceiling := range []int{1, 3} {
		got, _, attempts, err := minimize(p, seq, ceiling)
		if err != nil || attempts != ceiling || len(got.Operations) > 3 {
			t.Fatalf("ceiling %d: %d attempts, %+v, %v", ceiling, attempts, got, err)
		}
	}
	if _, _, err := Minimize([]byte(src), sequence(src, zp.Bytecode, zp.Bytes, "a", checkpoint), 0); err == nil ||
		!strings.Contains(err.Error(), "does not fail") {
		t.Fatalf("passing sequence: %v", err)
	}
	if _, _, err := Minimize([]byte("rule main = ."), seq, 0); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("other grammar: %v", err)
	}
}

// TestFuzzMinimizes checks that Fuzz saves the original and the minimized sequence.
func TestFuzzMinimizes(t *testing.T) {
	defer func() { corruptDocumentResult = nil }()
	corruptDocumentResult = func(text string, tree map[string]any) {
		if strings.Contains(text, "é") {
			tree["error"] = errorTree(errors.New("injected"))
		}
	}
	dir := t.TempDir()
	src := []byte(testGrammars[2])
	err := Fuzz(src, Options{Start: "main", Seed: 1, SaveFailures: dir, Minimize: true})
	var f *Failure
	if !errors.As(err, &f) || f.Path == "" || f.MinimizedPath == "" || f.Path == f.MinimizedPath ||
		len(f.Minimized.Operations) >= len(f.Sequence.Operations) || !strings.Contains(err.Error(), "minimized to") {
		t.Fatalf("got %v", err)
	}
	for _, path := range []string{f.Path, f.MinimizedPath} {
		seq, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		var m *Mismatch
		if err := Replay(src, seq); !errors.As(err, &m) {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

// TestMinimizeFailureMovesEarlier injects a mismatch that a shorter replacement makes appear at an
// earlier checkpoint, so that keeping the candidate drops the operations after it.
func TestMinimizeFailureMovesEarlier(t *testing.T) {
	defer func() { corruptDocumentResult = nil }()
	seen := false
	corruptDocumentResult = func(text string, tree map[string]any) {
		switch {
		case text == "":
			seen = false
		case strings.Contains(text, "éZ"):
			seen = true
		case strings.Contains(text, "é") && !strings.Contains(text, "Z") && (text == "é" || seen):
			tree["error"] = errorTree(errors.New("injected"))
		}
	}
	src := testGrammars[2]
	seq := sequence(src, zp.Closure, zp.CodePoints, "", edit(0, 0, "éZ"), checkpoint, edit(1, 2, "x"), checkpoint)
	got, m, err := Minimize([]byte(src), seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Operation{edit(0, 0, "é"), checkpoint}; !reflect.DeepEqual(got.Operations, want) || m.Event != 1 {
		t.Fatalf("minimized to %+v, mismatch %v", got.Operations, m)
	}
}
