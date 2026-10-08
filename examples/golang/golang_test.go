// Tests that the Go grammar (go.pego) parses real Go source files: the non-test files of this
// repository and a selection of files from the Go standard library.
package golang_test

import (
	"fmt"
	"go/ast"
	"go/parser"
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
	"time"

	"github.com/ornew/pego"
)

var (
	compileOnce sync.Once
	goParser    *pego.Parser
	compileErr  error
)

func compile(t *testing.T) *pego.Parser {
	t.Helper()
	compileOnce.Do(func() {
		src, err := os.ReadFile("go.pego")
		if err != nil {
			compileErr = err
			return
		}
		goParser, compileErr = pego.CompileSource(string(src), "main")
	})
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	return goParser
}

// parseFiles parses each file with go/parser and with the grammar, and reports the files that
// go/parser accepts but the grammar does not.
func parseFiles(t *testing.T, files []string) {
	p := compile(t)
	var total int
	start := time.Now()
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Error(err)
			continue
		}
		fset := token.NewFileSet()
		want, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("go/parser rejects %s: %v", path, err)
			continue
		}
		fileStart := time.Now()
		got, err := p.Parse(string(data), pego.WithUnit(pego.Bytes))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		compareAST(t, path, fset.File(want.Pos()), want, got)
		t.Logf("%s: %d bytes in %v", path, len(data), time.Since(fileStart).Round(time.Millisecond))
		total += len(data)
	}
	d := time.Since(start)
	t.Logf("%d files, %d bytes in %v (%.2f MB/s)", len(files), total, d.Round(time.Millisecond),
		float64(total)/d.Seconds()/1e6)
}

// TestTestdata checks the trees of the valid golden test inputs against go/ast.
func TestTestdata(t *testing.T) {
	parseFiles(t, []string{"testdata/generics.txt", "testdata/statements.txt"})
}

// TestRepository parses the non-test Go files of this repository.
// Generated parsers (large and repetitive) are skipped in short mode.
func TestRepository(t *testing.T) {
	root := filepath.Join("..", "..")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			slash := filepath.ToSlash(path)
			if testing.Short() && (strings.HasSuffix(slash, "bench/gen") || strings.HasPrefix(slash, "../../parsers/")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files")
	}
	parseFiles(t, files)
}

// stdlibShort is parsed in every run; stdlibLong only without -short.
var stdlibShort = []string{
	"container/list/list.go",
	"container/ring/ring.go",
	"container/heap/heap.go",
	"sort/sort.go",
	"sort/search.go",
	"sort/slice.go",
	"strings/builder.go",
	"strings/reader.go",
	"strings/replace.go",
	"strings/search.go",
	"bytes/buffer.go",
	"bytes/reader.go",
	"unicode/utf8/utf8.go",
	"errors/wrap.go",
	"slices/sort.go",
	"maps/maps.go",
	"cmp/cmp.go",
	"sync/once.go",
	"sync/waitgroup.go",
	"go/ast/walk.go",
	"text/template/parse/lex.go",
	"net/url/url.go",
	"encoding/json/scanner.go",
	"encoding/json/indent.go",
	"bufio/scan.go",
	"path/path.go",
	"io/io.go",
}

var stdlibLong = []string{
	"strings/strings.go",
	"bytes/bytes.go",
	"strconv/quote.go",
	"strconv/number.go",
	"slices/slices.go",
	"slices/iter.go",
	"iter/iter.go",
	"errors/join.go",
	"fmt/print.go",
	"fmt/scan.go",
	"fmt/format.go",
	"encoding/json/decode.go",
	"encoding/json/encode.go",
	"encoding/json/stream.go",
	"encoding/base64/base64.go",
	"encoding/csv/reader.go",
	"go/ast/ast.go",
	"go/ast/filter.go",
	"go/token/position.go",
	"go/token/token.go",
	"go/scanner/scanner.go",
	"text/template/parse/node.go",
	"text/template/parse/parse.go",
	"text/template/exec.go",
	"text/tabwriter/tabwriter.go",
	"bufio/bufio.go",
	"regexp/syntax/parse.go",
	"regexp/regexp.go",
	"go/parser/parser.go",
	"sync/map.go",
	"context/context.go",
	"time/format.go",
	"math/big/nat.go",
	"math/bits/bits.go",
	"net/http/header.go",
	"net/http/cookie.go",
	"os/exec/exec.go",
	"path/filepath/path.go",
	"flag/flag.go",
	"io/fs/walk.go",
	"unicode/letter.go",
	"html/escape.go",
	"log/log.go",
	"os/file.go",
}

// TestStdlib parses files from the Go standard library in GOROOT.
func TestStdlib(t *testing.T) {
	src := filepath.Join(runtime.GOROOT(), "src")
	if _, err := os.Stat(filepath.Join(src, "strings", "strings.go")); err != nil {
		t.Skipf("GOROOT sources are not available: %v", err)
	}
	names := stdlibShort
	if !testing.Short() {
		names = append(append([]string{}, stdlibShort...), stdlibLong...)
	}
	var files []string
	for _, name := range names {
		path := filepath.Join(src, filepath.FromSlash(name))
		if _, err := os.Stat(path); err != nil {
			t.Logf("skip %s: %v", name, err)
			continue
		}
		files = append(files, path)
	}
	parseFiles(t, files)
}

// compareAST checks that the grammar builds the same tree as go/parser: both trees must have the
// same nodes, identified by their go/ast type name and byte range. Binary and unary expressions
// also record the range of their operator, which checks precedence and associativity.
func compareAST(t *testing.T, path string, tf *token.File, want *ast.File, got *pego.Node) {
	t.Helper()
	off := func(p token.Pos) int { return tf.Offset(p) }
	wantKeys := map[string]int{}
	funcDeclTypes := map[*ast.FuncType]bool{}
	for _, d := range want.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			funcDeclTypes[fd.Type] = true
		}
	}
	add := func(m map[string]int, kind string, start, end int) {
		m[fmt.Sprintf("%s %d %d", kind, start, end)]++
	}
	ast.Inspect(want, func(n ast.Node) bool {
		switch n := n.(type) {
		case nil, *ast.File, *ast.Comment, *ast.CommentGroup, *ast.FieldList, *ast.EmptyStmt:
			return true
		case *ast.FuncType:
			// The type of a function declaration starts at the "func" keyword in go/ast,
			// but the grammar builds it in the declaration's action, so it is not compared.
			if funcDeclTypes[n] {
				return true
			}
		case *ast.BinaryExpr:
			add(wantKeys, "op", off(n.OpPos), off(n.OpPos)+len(n.Op.String()))
		case *ast.UnaryExpr:
			add(wantKeys, "op", off(n.OpPos), off(n.OpPos)+len(n.Op.String()))
		}
		add(wantKeys, reflect.TypeOf(n).Elem().Name(), off(n.Pos()), off(n.End()))
		return true
	})

	gotKeys := map[string]int{}
	var walk func(n *pego.Node, parent string)
	walk = func(n *pego.Node, parent string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "Match", "Seq", "List", "File", "EmptyStmt":
		case "FuncType":
			if parent != "FuncDecl" {
				add(gotKeys, n.Type(), int(n.Start), int(n.End))
			}
		case "IntLit", "FloatLit", "ImagLit", "CharLit", "StringLit":
			add(gotKeys, "BasicLit", int(n.Start), int(n.End))
		case "BinaryExpr", "UnaryExpr":
			if op, ok := n.Field("Op").(*pego.Node); ok {
				add(gotKeys, "op", int(op.Start), int(op.End))
			}
			add(gotKeys, n.Type(), int(n.Start), int(n.End))
		default:
			add(gotKeys, n.Type(), int(n.Start), int(n.End))
		}
		for _, c := range n.Children {
			walk(c, n.Type())
		}
		for _, f := range n.Fields {
			if c, ok := f.Value.(*pego.Node); ok {
				walk(c, n.Type())
			}
		}
	}
	walk(got, "")

	var diffs []string
	for k, n := range wantKeys {
		if gotKeys[k] < n {
			diffs = append(diffs, "missing "+describe(tf, k))
		}
	}
	for k, n := range gotKeys {
		if wantKeys[k] < n {
			diffs = append(diffs, "unexpected "+describe(tf, k))
		}
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		if len(diffs) > 10 {
			diffs = append(diffs[:10], "...")
		}
		t.Errorf("%s: the tree differs from go/ast:\n\t%s", path, strings.Join(diffs, "\n\t"))
	}
}

// describe formats a node key with line and column numbers.
func describe(tf *token.File, key string) string {
	var kind string
	var start, end int
	fmt.Sscanf(key, "%s %d %d", &kind, &start, &end)
	pos := func(o int) string {
		if o < 0 || o > tf.Size() {
			return "?"
		}
		p := tf.Position(tf.Pos(o))
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	return fmt.Sprintf("%s %s-%s", kind, pos(start), pos(end))
}
