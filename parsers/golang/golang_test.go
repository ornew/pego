package golang_test

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// compare parses src with go/parser and with the package, and returns a description of the first
// difference: in acceptance, or in the trees when both accept.
func compare(name string, src []byte, mode golang.Mode) string {
	gmode := goparser.SkipObjectResolution
	if mode&golang.ParseComments != 0 {
		gmode |= goparser.ParseComments
	}
	fset := token.NewFileSet()
	want, werr := goparser.ParseFile(fset, name, src, gmode)
	ok := golang.Valid(string(src))
	fset2 := token.NewFileSet()
	got, gerr := golang.ParseFile(fset2, name, src, mode)
	if ok != (gerr == nil) {
		return fmt.Sprintf("Valid %v but ParseFile: %v", ok, gerr)
	}
	if (werr == nil) != (gerr == nil) {
		if werr != nil {
			return fmt.Sprintf("accepted; go/parser: %v", werr)
		}
		return fmt.Sprintf("rejected: %v", gerr)
	}
	if werr != nil {
		return ""
	}
	tf1 := fset.File(want.FileStart)
	tf2 := fset2.File(got.FileStart)
	if d := diffNodes("File", reflect.ValueOf(want), reflect.ValueOf(got), tf1, tf2); d != "" {
		return d
	}
	return ""
}

var posType = reflect.TypeOf(token.Pos(0))

// diffNodes compares two go/ast values, comparing positions by offset and line information.
func diffNodes(path string, a, b reflect.Value, tf1, tf2 *token.File) string {
	if a.Kind() != b.Kind() {
		return fmt.Sprintf("%s: kind %v vs %v", path, a.Kind(), b.Kind())
	}
	if a.Type() == posType {
		pa, pb := token.Pos(a.Int()), token.Pos(b.Int())
		if pa.IsValid() != pb.IsValid() {
			return fmt.Sprintf("%s: position %v vs %v", path, describe(tf1, pa), describe(tf2, pb))
		}
		if pa.IsValid() && (tf1.Offset(pa) != tf2.Offset(pb) || tf1.Position(pa) != tf2.Position(pb)) {
			return fmt.Sprintf("%s: position %v vs %v", path, describe(tf1, pa), describe(tf2, pb))
		}
		return ""
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: %v vs %v", path, short(a), short(b))
			}
			return ""
		}
		if a.Kind() == reflect.Interface && a.Elem().Type() != b.Elem().Type() {
			return fmt.Sprintf("%s: %v vs %v", path, a.Elem().Type(), b.Elem().Type())
		}
		if a.Kind() == reflect.Pointer && a.Type() == reflect.TypeOf((*ast.Object)(nil)) {
			return ""
		}
		return diffNodes(path, a.Elem(), b.Elem(), tf1, tf2)
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			f := a.Type().Field(i)
			// Identifiers are not resolved, and comments are only collected in File.Comments.
			if f.Name == "Scope" || f.Name == "Unresolved" || f.Name == "Obj" || f.Name == "Doc" || f.Name == "Comment" {
				continue
			}
			if d := diffNodes(path+"."+f.Name, a.Field(i), b.Field(i), tf1, tf2); d != "" {
				return d
			}
		}
		return ""
	case reflect.Slice:
		if a.Len() != b.Len() || a.IsNil() != b.IsNil() {
			return fmt.Sprintf("%s: length %d (nil %v) vs %d (nil %v)", path, a.Len(), a.IsNil(), b.Len(), b.IsNil())
		}
		for i := 0; i < a.Len(); i++ {
			if d := diffNodes(fmt.Sprintf("%s[%d]", path, i), a.Index(i), b.Index(i), tf1, tf2); d != "" {
				return d
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			return fmt.Sprintf("%s: %#v vs %#v", path, a.Interface(), b.Interface())
		}
		return ""
	}
}

func short(v reflect.Value) string {
	if v.IsNil() {
		return "nil"
	}
	return v.Elem().Type().String()
}

func describe(tf *token.File, p token.Pos) string {
	if !p.IsValid() {
		return "NoPos"
	}
	return fmt.Sprintf("%d (%v)", tf.Offset(p), tf.Position(p))
}

// corpus returns the .go files under GOROOT/src, sorted.
func corpus(t *testing.T) []string {
	root := filepath.Join(runtime.GOROOT(), "src")
	if _, err := os.Stat(filepath.Join(root, "go", "parser", "parser.go")); err != nil {
		t.Skipf("GOROOT sources are not available: %v", err)
	}
	var files []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// checkFiles compares the package with go/parser on each file, in parallel.
func checkFiles(t *testing.T, files []string, mode golang.Mode) {
	var mu sync.Mutex
	var failures []string
	var accepted, rejected int
	work := make(chan string)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				d := compare(path, data, mode)
				_, werr := goparser.ParseFile(token.NewFileSet(), path, data, goparser.SkipObjectResolution)
				mu.Lock()
				if d != "" {
					failures = append(failures, path+": "+d)
				}
				if werr == nil {
					accepted++
				} else {
					rejected++
				}
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		work <- f
	}
	close(work)
	wg.Wait()
	sort.Strings(failures)
	for i, f := range failures {
		if i == 50 {
			t.Errorf("... and %d more", len(failures)-50)
			break
		}
		t.Error(f)
	}
	t.Logf("%d files (%d accepted and %d rejected by go/parser), %d differ", len(files), accepted, rejected, len(failures))
}

// TestGOROOT compares the package with go/parser on the Go files of GOROOT/src (every 16th file
// with -short).
func TestGOROOT(t *testing.T) {
	checkFiles(t, sample(corpus(t), 16), 0)
}

// TestGOROOTComments compares the comments that ParseFile collects with ParseComments with those
// of go/parser (every 64th file with -short, every 4th otherwise).
func TestGOROOTComments(t *testing.T) {
	n := 4
	if testing.Short() {
		n = 64
	}
	checkFiles(t, every(corpus(t), n), golang.ParseComments)
}

// sample returns every nth file with -short, and all of them otherwise.
func sample(files []string, n int) []string {
	if !testing.Short() {
		return files
	}
	return every(files, n)
}

func every(files []string, n int) []string {
	var sub []string
	for i := 0; i < len(files); i += n {
		sub = append(sub, files[i])
	}
	return sub
}
