package pego_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

// unformattedFixtures are checked-in grammars that are unformatted on purpose.
// There are none; add a file here with the reason when a test needs one.
var unformattedFixtures = map[string]string{}

// Every checked-in grammar is what `pego fmt` writes. Run `pego fmt -w` on a
// file reported here.
func TestCheckedInGrammarsAreFormatted(t *testing.T) {
	found := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".pego" {
			return nil
		}
		found++
		if _, ok := unformattedFixtures[filepath.ToSlash(path)]; ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		g, err := pego.ParseGrammar(string(src))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		if grammar.Format(g) != string(src) {
			t.Errorf("%s is not formatted: run pego fmt -w %s", path, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no grammars found")
	}
}
