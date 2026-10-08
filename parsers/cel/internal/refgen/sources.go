package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// conformanceExprs returns the expressions of the tests of the CEL conformance suite (tests/simple/testdata/*.textproto
// of cel-spec): the string of each expr field.
func conformanceExprs(specDir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(specDir, "tests", "simple", "testdata", "*.textproto"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no textproto files in %s: %v", specDir, err)
	}
	sort.Strings(files)
	var out []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		es, err := textprotoExprs(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %v", f, err)
		}
		out = append(out, es...)
	}
	return out, nil
}

// textprotoExprs returns the value of each expr field of a file in the protobuf text format. A string value may be
// several string literals on several lines, which are concatenated.
func textprotoExprs(data string) ([]string, error) {
	var out []string
	lines := strings.Split(data, "\n")
	for i := 0; i < len(lines); i++ {
		rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "expr:")
		if !ok {
			continue
		}
		var val string
		for {
			rest = strings.TrimSpace(rest)
			for rest != "" {
				if rest[0] != '"' && rest[0] != '\'' {
					return nil, fmt.Errorf("line %d: not a string literal: %s", i+1, rest)
				}
				end := 1
				for end < len(rest) && rest[end] != rest[0] {
					if rest[end] == '\\' {
						end++
					}
					end++
				}
				if end >= len(rest) {
					return nil, fmt.Errorf("line %d: unterminated string", i+1)
				}
				s, err := textprotoString(rest[:end+1])
				if err != nil {
					return nil, err
				}
				val += s
				rest = strings.TrimSpace(rest[end+1:])
			}
			// The value goes on in the next lines while they start with a quote.
			j := i + 1
			for j < len(lines) && (strings.TrimSpace(lines[j]) == "" || strings.HasPrefix(strings.TrimSpace(lines[j]), "#")) {
				j++
			}
			if j < len(lines) && (strings.HasPrefix(strings.TrimSpace(lines[j]), "\"") || strings.HasPrefix(strings.TrimSpace(lines[j]), "'")) {
				i, rest = j, lines[j]
				continue
			}
			break
		}
		out = append(out, val)
	}
	return out, nil
}

// textprotoString decodes a string literal of the protobuf text format, with single or double quotes.
func textprotoString(lit string) (string, error) {
	if len(lit) < 2 || (lit[0] != '"' && lit[0] != '\'') || lit[len(lit)-1] != lit[0] {
		return "", fmt.Errorf("not a string literal: %s", lit)
	}
	body := lit[1 : len(lit)-1]
	var b []byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b = append(b, c)
			continue
		}
		i++
		if i >= len(body) {
			return "", fmt.Errorf("bad escape in %s", lit)
		}
		switch c = body[i]; c {
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'a':
			b = append(b, '\a')
		case 'b':
			b = append(b, '\b')
		case 'f':
			b = append(b, '\f')
		case 'v':
			b = append(b, '\v')
		case '\\', '\'', '"', '?':
			b = append(b, c)
		case 'x', 'X':
			j := i + 1
			for j < len(body) && j < i+3 && strings.IndexByte("0123456789abcdefABCDEF", body[j]) >= 0 {
				j++
			}
			n, err := strconv.ParseUint(body[i+1:j], 16, 8)
			if err != nil {
				return "", fmt.Errorf("bad escape in %s", lit)
			}
			b = append(b, byte(n))
			i = j - 1
		case 'u', 'U':
			n := 4
			if c == 'U' {
				n = 8
			}
			if i+1+n > len(body) {
				return "", fmt.Errorf("bad escape in %s", lit)
			}
			v, err := strconv.ParseUint(body[i+1:i+1+n], 16, 32)
			if err != nil {
				return "", fmt.Errorf("bad escape in %s", lit)
			}
			b = utf8.AppendRune(b, rune(v))
			i += n
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(body) && j < i+3 && body[j] >= '0' && body[j] <= '7' {
				j++
			}
			n, _ := strconv.ParseUint(body[i:j], 8, 16)
			b = append(b, byte(n))
			i = j - 1
		default:
			return "", fmt.Errorf("bad escape \\%c in %s", c, lit)
		}
	}
	return string(b), nil
}

// celGoTestInputs returns the expressions in the tests of the parser of cel-go (parser_test.go: the I fields of
// the cases; unparser_test.go: the in fields; unescape_test.go: the string literals, as a string and as bytes).
func celGoTestInputs(parserDir string) ([]string, error) {
	var out []string
	fset := token.NewFileSet()
	for _, f := range []struct{ file, key string }{{"parser_test.go", "I"}, {"unparser_test.go", "in"}, {"unescape_test.go", "in"}} {
		file, err := parser.ParseFile(fset, filepath.Join(parserDir, f.file), nil, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			k, ok := kv.Key.(*ast.Ident)
			if !ok || k.Name != f.key {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			out = append(out, s)
			if f.file == "unescape_test.go" {
				out = append(out, "b"+s)
			}
			return true
		})
	}
	return out, nil
}
