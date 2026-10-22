// Package upstream collects the CUE sources of a checkout of cue-lang/cue that the reference results of the tests of
// package cue are about: its .cue files, the CUE inside its .txtar archives, the inputs of the tests of its parser and
// the examples of its specification. internal/refgen, which runs the reference parser over them, and the tests, which
// read the results, share it, so that the results can refer to the sources by name and hash and need not hold them.
package upstream

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A Source is a CUE text of the corpus.
type Source struct {
	Name string
	Data []byte
}

// Collect gathers the CUE of the cue repository checked out in dir: its .cue files, the CUE files
// inside its .txtar archives, the inputs of the parser's own tests and the examples of the
// specification. Names are slash-separated paths relative to dir; a file of an archive is named
// archive#file.
func Collect(dir string) ([]Source, error) {
	var out []Source
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasSuffix(path, ".cue"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out = append(out, Source{rel, data})
		case strings.HasSuffix(path, ".txtar"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, f := range txtarFiles(data) {
				if strings.HasSuffix(f.Name, ".cue") {
					out = append(out, Source{rel + "#" + f.Name, f.Data})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ts, err := parserTests(filepath.Join(dir, "cue/parser/parser_test.go"))
	if err != nil {
		return nil, err
	}
	out = append(out, ts...)
	out = append(out, specExamples(filepath.Join(dir, "doc/ref/spec.md"))...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Hash returns the form in which a result file records the content of a source: "sha256:" and the first 32
// hexadecimal digits of its SHA-256.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:16])
}

// txtarFiles splits a txtar archive into its files (the comment before the first file is dropped).
func txtarFiles(data []byte) []Source {
	var files []Source
	var cur *Source
	var buf bytes.Buffer
	flush := func() {
		if cur != nil {
			cur.Data = append([]byte(nil), buf.Bytes()...)
			files = append(files, *cur)
		}
		buf.Reset()
	}
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		l := bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(l, []byte("-- ")) && bytes.HasSuffix(l, []byte(" --")) && len(l) > 6 {
			flush()
			cur = &Source{Name: strings.TrimSpace(string(l[3 : len(l)-3]))}
			continue
		}
		buf.Write(line)
	}
	flush()
	return files
}

// parserTests extracts the string values of the "in" fields (and of the cases of the other tables)
// of cue/parser/parser_test.go.
func parserTests(path string) ([]Source, error) {
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []Source
	n := 0
	str := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != gotoken.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	ast.Inspect(f, func(node ast.Node) bool {
		kv, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok || id.Name != "in" {
			return true
		}
		if s, ok := str(kv.Value); ok {
			n++
			out = append(out, Source{"parser_test.go#in" + strconv.Itoa(n), []byte(s)})
		}
		return true
	})
	// Unkeyed tables: {"desc", `source`} in TestStrict and the like.
	ast.Inspect(f, func(node ast.Node) bool {
		cl, ok := node.(*ast.CompositeLit)
		if !ok || len(cl.Elts) != 2 {
			return true
		}
		if _, ok := cl.Elts[0].(*ast.KeyValueExpr); ok {
			return true
		}
		if _, ok := str(cl.Elts[0]); !ok {
			return true
		}
		if s, ok := str(cl.Elts[1]); ok {
			n++
			out = append(out, Source{"parser_test.go#in" + strconv.Itoa(n), []byte(s)})
		}
		return true
	})
	return out, nil
}

// specExamples extracts the fenced code blocks of the specification marked as CUE.
func specExamples(path string) []Source {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Source
	var buf bytes.Buffer
	in := false
	n := 0
	var tag string
	for _, line := range strings.SplitAfter(string(data), "\n") {
		l := strings.TrimRight(line, "\r\n")
		switch {
		case !in && strings.HasPrefix(l, "```"):
			tag = strings.TrimSpace(strings.TrimPrefix(l, "```"))
			in = true
			buf.Reset()
		case in && l == "```":
			in = false
			if tag == "cue" || strings.HasPrefix(tag, "cue ") {
				n++
				name := "spec.md#" + strconv.Itoa(n) + "." + strings.ReplaceAll(strings.TrimSpace(tag), " ", "_")
				out = append(out, Source{name, append([]byte(nil), buf.Bytes()...)})
			}
		case in:
			buf.WriteString(line)
		}
	}
	return out
}
