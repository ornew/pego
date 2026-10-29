package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ornew/pego/internal/bench/inputs"
)

// tsWorkload is a workload measured on the generated TypeScript parser: the grammar and the input
// that the Go benchmarks of the same name use. The recovery workload is left out because a
// recovering parse reports an error, which the measuring script treats as a failed run.
type tsWorkload struct {
	name, grammar string
	input         func() string
}

var tsWorkloads = []tsWorkload{
	{"JSON", "parsers/json/json.pego", func() string { return inputs.JSONInput(256 << 10) }},
	{"CSV", "parsers/csv/csv.pego", func() string { return inputs.CSVInput(5000) }},
	{"XML", "parsers/xml/xml.pego", func() string { return inputs.XMLInput(256 << 10) }},
	{"Arith_OperatorsRule", "examples/calculator/calc.pego", func() string { return inputs.ArithInput(20000) }},
	{"Arith_LeftRec", "examples/calculator/calc_lr.pego", func() string { return inputs.ArithInput(20000) }},
	{"Minilang", "examples/minilang/minilang.pego", func() string { return inputs.MinilangInput(300, 0) }},
	{"Outline", "examples/outline/outline.pego", func() string { return inputs.OutlineInput(5000) }},
}

// tsModes are the entry points of a generated TypeScript parser that are measured, by the name
// they have in the results.
var tsModes = []struct{ name, entry string }{
	{"Parse", "parse"},
	{"ParseAST", "parseAST"},
	{"Recognize", "recognize"},
}

// captureTS generates a TypeScript parser for every workload, measures each entry point in count
// rounds of fresh Node processes (bench/ts-parse.mjs) and returns the results with a header.
func captureTS(root, commit, work, node string, count int) ([]byte, error) {
	pego, err := buildPego(root, work)
	if err != nil {
		return nil, err
	}
	version, err := execOut(root, nil, node, "--version")
	if err != nil {
		return nil, err
	}
	v8, err := execOut(root, nil, node, "-p", "process.versions.v8")
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeHeader(&b, commit)
	fmt.Fprintf(&b, "# node: %s\n# v8: %s\n# count: %d\n", strings.TrimSpace(version), strings.TrimSpace(v8), count)
	inputs := filepath.Join(work, "ts-inputs")
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		return nil, err
	}
	for _, w := range tsWorkloads {
		if err := os.WriteFile(filepath.Join(inputs, w.name+".txt"), []byte(w.input()), 0o644); err != nil {
			return nil, err
		}
		args := []string{"gen", "-lang", "ts", "-types", "-recognize", "-g", filepath.Join(root, w.grammar), "-o", filepath.Join(work, "ts", w.name)}
		if _, err := execOut(root, nil, pego, args...); err != nil {
			return nil, err
		}
	}
	// Alternate the runs of the rounds so that drift of the machine affects every workload alike.
	script := filepath.Join(root, "bench", "ts-parse.mjs")
	for round := 0; round < count; round++ {
		for _, w := range tsWorkloads {
			for _, m := range tsModes {
				line, err := execOut(root, nil, node, "--experimental-strip-types", script,
					"--module", filepath.Join(work, "ts", w.name, "parser.ts"),
					"--input", filepath.Join(inputs, w.name+".txt"),
					"--name", m.name+"/"+w.name, "--entry", m.entry)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", m.name, w.name, err)
				}
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "BenchmarkTSParse/") {
					return nil, fmt.Errorf("%s %s: unexpected output %q", m.name, w.name, line)
				}
				fmt.Fprintf(os.Stderr, "%s\n", line)
				fmt.Fprintf(&b, "BenchmarkTS/%s\n", strings.TrimPrefix(line, "BenchmarkTSParse/"))
			}
		}
	}
	return b.Bytes(), nil
}

// sizeParsers are the parsers whose generated size is recorded, with their go:generate options
// read from parsers/generate.go.
var sizeParsers = []string{"json", "csv", "xml", "yaml", "cel", "cue", "golang", "python", "typescript", "duckdb"}

var genDirective = regexp.MustCompile(`^//go:generate go run \.\./cmd/pego gen (.*)$`)

// parserGenArgs returns the options with which parsers/generate.go generates parser name, adapted
// to TypeScript: the Go-only options (-module, -runtime import, -nodoc, -typed-only) are dropped.
func parserGenArgs(root, name string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "parsers", "generate.go"))
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		m := genDirective.FindStringSubmatch(line)
		if m == nil || !strings.Contains(m[1], "-g "+name+"/") {
			continue
		}
		f := strings.Fields(m[1])
		var out []string
		for i := 0; i < len(f); i++ {
			switch f[i] {
			case "-module", "-runtime", "-o":
				i++
			case "-nodoc", "-typed-only":
			case "-g":
				out = append(out, "-g", filepath.Join(root, "parsers", f[i+1]))
				i++
			default:
				out = append(out, f[i])
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("parsers/generate.go has no go:generate line for %s", name)
}

// captureSizes records, for each parser of sizeParsers, the size of the committed Go parser.go,
// of a binary that calls its ParseAST (built with -trimpath and stripped of symbols and DWARF), of
// the generated TypeScript parser.ts and of that file compressed with gzip -9. The embedded
// TypeScript runtime is common to all parsers and is recorded once as parser "runtime".
func captureSizes(root, commit, work string) ([]byte, error) {
	pego, err := buildPego(root, work)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeHeader(&b, commit)
	fmt.Fprintln(&b, "# go binary: a main that calls ParseAST; go build -trimpath -ldflags=\"-s -w\"")
	for _, name := range sizeParsers {
		st, err := os.Stat(filepath.Join(root, "parsers", name, "parser.go"))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "size %s go-source %d\n", name, st.Size())
		bin, err := buildBinary(root, work, name)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "size %s go-binary %d\n", name, bin)
		args, err := parserGenArgs(root, name)
		if err != nil {
			return nil, err
		}
		out := filepath.Join(work, "size-ts", name)
		if _, err := execOut(root, nil, pego, append(append([]string{"gen", "-lang", "ts"}, args...), "-o", out)...); err != nil {
			return nil, err
		}
		raw, gz, err := sizeOfFile(filepath.Join(out, "parser.ts"))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "size %s ts-raw %d\nsize %s ts-gzip %d\n", name, raw, name, gz)
		if name == sizeParsers[0] {
			raw, gz, err := sizeOfFile(filepath.Join(out, "runtime.ts"))
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "size runtime ts-raw %d\nsize runtime ts-gzip %d\n", raw, gz)
		}
	}
	return b.Bytes(), nil
}

// sizeOfFile returns the size of a file and of its gzip -9 compression.
func sizeOfFile(path string) (raw, gz int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := w.Write(data); err != nil {
		return 0, 0, err
	}
	if err := w.Close(); err != nil {
		return 0, 0, err
	}
	return len(data), buf.Len(), nil
}

// buildBinary builds a program that calls ParseAST of parsers/name in a module of its own in the
// work directory and returns the size of the binary.
func buildBinary(root, work, name string) (int, error) {
	dir := filepath.Join(work, "bin-"+name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	mod := fmt.Sprintf(`module sizeprobe

go 1.27.1

require (
	github.com/ornew/pego v0.0.0
	github.com/ornew/pego/parsers/%[1]s v0.0.0
)

replace github.com/ornew/pego => %[2]s

replace github.com/ornew/pego/parsers/%[1]s => %[2]s/parsers/%[1]s
`, name, root)
	src := fmt.Sprintf(`package main

import (
	"os"

	p "github.com/ornew/pego/parsers/%s"
)

func main() {
	if _, err := p.ParseAST(os.Args[1]); err != nil {
		os.Exit(1)
	}
}
`, name)
	for file, content := range map[string]string{"go.mod": mod, "main.go": src} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			return 0, err
		}
	}
	out := filepath.Join(dir, "probe")
	if _, err := execOut(dir, []string{"GOWORK=off", "GOFLAGS=-mod=mod"}, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "."); err != nil {
		return 0, err
	}
	st, err := os.Stat(out)
	if err != nil {
		return 0, err
	}
	return int(st.Size()), nil
}

// buildPego builds the pego command of the repository into the work directory.
func buildPego(root, work string) (string, error) {
	out := filepath.Join(work, "pego")
	if _, err := execOut(root, nil, "go", "build", "-trimpath", "-o", out, "./cmd/pego"); err != nil {
		return "", err
	}
	return out, nil
}

// execOut runs a command in dir, with extra environment variables, and returns its standard output.
func execOut(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(slices.Clone(args), " "), err, stderr.String())
	}
	return string(out), nil
}
