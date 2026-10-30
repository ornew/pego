// Command report runs the benchmarks of package bench and writes docs/benchmarks.md from the
// results, so that the document is never edited by hand:
//
//	go run ./bench/report            # capture everything (about 20 minutes) and rewrite the document
//	go run ./bench/report -in FILE   # rewrite the document from saved results
//
// A capture saves three files in bench/ (or the directory of -dir), each with a header giving the date,
// commit and toolchain of its run:
//
//	results.txt      raw `go test -bench` output of the Go benchmarks
//	results-ts.txt   parse, typed and recognition times of the generated TypeScript parsers under Node
//	                 (bench/ts-parse.mjs, one fresh process per measurement)
//	results-size.txt sizes of the generated Go and TypeScript parsers: parser.go, a built binary,
//	                 parser.ts and its gzip -9 compression
//
// -capture selects the parts to capture (go, ts, size; default all) and keeps the files of the others.
// The document is rendered from the files and the template benchmarks.md.tmpl; it holds results only
// (their analysis is in docs/performance.md), and every figure in it is computed from the results.
// TestDocumentIsUpToDate checks that it matches them.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ornew/pego/internal/ir/exec/core"
)

func main() {
	in := flag.String("in", "", "render from this results file (and results-ts.txt, results-size.txt next to it) instead of capturing")
	dir := flag.String("dir", "", "directory of the results files (default: bench/ of the repository)")
	doc := flag.String("doc", "", "document to write (default: docs/benchmarks.md of the repository)")
	capture := flag.String("capture", "go,ts,size", "parts to capture: go, ts and size, comma-separated")
	count := flag.Int("count", 3, "runs of each benchmark (the median is reported)")
	node := flag.String("node", "node", "the Node.js executable for the TypeScript measurements (24 or later)")
	flag.Parse()
	if err := run(*in, *dir, *doc, *capture, *count, *node); err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
		os.Exit(1)
	}
}

func run(in, dir, docPath, capture string, count int, node string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if in != "" {
		dir = filepath.Dir(in)
	} else if dir == "" {
		dir = filepath.Join(root, "bench")
	}
	resultsPath := filepath.Join(dir, "results.txt")
	if in != "" {
		resultsPath = in
	} else {
		parts := map[string]bool{}
		for _, p := range strings.Split(capture, ",") {
			if p = strings.TrimSpace(p); p != "go" && p != "ts" && p != "size" {
				return fmt.Errorf("-capture: unknown part %q (want go, ts or size)", p)
			}
			parts[p] = true
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		commit := describeCommit(root)
		work, err := os.MkdirTemp("", "pego-report-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		if parts["go"] {
			data, err := runBenchmarks(root, commit, count)
			if err != nil {
				return err
			}
			if err := os.WriteFile(resultsPath, data, 0o644); err != nil {
				return err
			}
		}
		if parts["ts"] {
			data, err := captureTS(root, commit, work, node, count)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "results-ts.txt"), data, 0o644); err != nil {
				return err
			}
		}
		if parts["size"] {
			data, err := captureSizes(root, commit, work)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "results-size.txt"), data, 0o644); err != nil {
				return err
			}
		}
	}
	data, tsData, sizeData, err := loadResults(resultsPath)
	if err != nil {
		return err
	}
	doc, err := renderAll(root, data, tsData, sizeData)
	if err != nil {
		return err
	}
	out := docPath
	if out == "" {
		out = filepath.Join(root, "docs", "benchmarks.md")
	}
	if err := os.WriteFile(out, doc, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", out)
	return nil
}

// loadResults reads the Go results file and, next to it, the TypeScript and size results if they
// exist (nil otherwise: the document then leaves out their sections).
func loadResults(goPath string) (data, tsData, sizeData []byte, err error) {
	if data, err = os.ReadFile(goPath); err != nil {
		return nil, nil, nil, err
	}
	optional := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(goPath), name))
		if os.IsNotExist(err) {
			return nil, nil
		}
		return b, err
	}
	if tsData, err = optional("results-ts.txt"); err != nil {
		return nil, nil, nil, err
	}
	if sizeData, err = optional("results-size.txt"); err != nil {
		return nil, nil, nil, err
	}
	return data, tsData, sizeData, nil
}

// repoRoot returns the root of the repository (the directory of the module's go.mod).
func repoRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/ornew/pego").Output()
	if err != nil {
		return "", fmt.Errorf("finding the repository (run inside it): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// runBenchmarks runs the benchmarks and returns their output after a header describing the run.
func runBenchmarks(root, commit string, count int) ([]byte, error) {
	var b bytes.Buffer
	writeHeader(&b, commit)
	cmd := exec.Command("go", "test", "./bench", "-run", "^$", "-bench", ".", "-benchmem",
		"-count", fmt.Sprint(count), "-timeout", "0")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags(os.Getenv("GOFLAGS")))
	cmd.Stdout = io.MultiWriter(&b, os.Stderr)
	cmd.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "running %s\n", strings.Join(cmd.Args, " "))
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running the benchmarks: %w", err)
	}
	return b.Bytes(), nil
}

// describeCommit names the commit of the repository for the headers of the results, with a note
// if tracked files have uncommitted changes. It is determined once, before a capture writes any
// result file, so that all the files of one capture name the same state.
func describeCommit(root string) string {
	commit := gitOutput(root, "rev-parse", "--short", "HEAD")
	if gitOutput(root, "status", "--porcelain", "--untracked-files=no") != "" {
		commit += " (with uncommitted changes)"
	}
	return commit
}

// writeHeader writes the description of a run: date, the commit (see describeCommit), Go version,
// cores and load.
func writeHeader(b *bytes.Buffer, commit string) {
	goVersion, _ := exec.Command("go", "env", "GOVERSION").Output()
	fmt.Fprintf(b, "# date: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(b, "# commit: %s\n", commit)
	fmt.Fprintf(b, "# go: %s\n", strings.TrimSpace(string(goVersion)))
	fmt.Fprintf(b, "# cores: %d\n", runtime.NumCPU())
	if load := loadAverage(); load != "" {
		fmt.Fprintf(b, "# load: %s\n", load)
	}
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// loadAverage returns the 1, 5 and 15 minute load averages before the run, if the system tells.
func loadAverage() string {
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(data))
		if len(f) >= 3 {
			return strings.Join(f[:3], " ")
		}
	}
	if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
		return strings.Trim(strings.TrimSpace(string(out)), "{ }")
	}
	return ""
}
