package golang_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// embeddedSources returns the Go source files embedded as string literals in the tests of
// GOROOT/src (such as the valid and invalid programs of go/parser's short_test.go): the literals
// that begin with a package clause.
func embeddedSources(t *testing.T, files []string) []string {
	seen := map[string]bool{}
	var mu sync.Mutex
	work := make(chan string)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				f, err := goparser.ParseFile(token.NewFileSet(), path, nil, goparser.SkipObjectResolution)
				if err != nil {
					continue
				}
				ast.Inspect(f, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil || !strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "package") {
						return true
					}
					mu.Lock()
					seen[s] = true
					mu.Unlock()
					return true
				})
			}
		}()
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			work <- f
		}
	}
	close(work)
	wg.Wait()
	srcs := make([]string, 0, len(seen))
	for s := range seen {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	return srcs
}

// TestEmbeddedSources compares the package with go/parser on the Go programs embedded in the
// tests of GOROOT/src, many of them invalid on purpose.
func TestEmbeddedSources(t *testing.T) {
	srcs := embeddedSources(t, corpus(t))
	srcs = sample(srcs, 8)
	var failures []string
	var accepted int
	var mu sync.Mutex
	work := make(chan string)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for src := range work {
				d := compare("x.go", []byte(src), 0)
				ok := goparserAccepts(src)
				mu.Lock()
				if d != "" {
					failures = append(failures, strconv.Quote(src)+": "+d)
				}
				if ok {
					accepted++
				}
				mu.Unlock()
			}
		}()
	}
	for _, s := range srcs {
		work <- s
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
	t.Logf("%d programs (%d accepted by go/parser), %d differ", len(srcs), accepted, len(failures))
}

// TestOtherSources compares the package with go/parser on the Go sources in testdata directories
// that do not end in .go: the .go2 and .src files of go/parser and the inputs and golden files of
// go/printer and gofmt.
func TestOtherSources(t *testing.T) {
	root := filepath.Join(runtime.GOROOT(), "src")
	var files []string
	for _, dir := range []string{"go/parser/testdata", "go/printer/testdata", "cmd/gofmt/testdata", "go/format/testdata"} {
		filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && !strings.HasSuffix(p, ".go") {
				files = append(files, p)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Skip("GOROOT sources are not available")
	}
	checkFiles(t, files, 0)
}
