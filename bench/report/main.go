// Command report runs the benchmarks of package bench and writes docs/benchmarks.md from the
// results, so that the document is never edited by hand:
//
//	go run ./bench/report            # run the benchmarks (about 15 minutes) and rewrite the document
//	go run ./bench/report -in FILE   # rewrite the document from saved results
//
// It saves the raw `go test` output, with the date, commit, Go version and load average of the run
// in a header, to bench/results.txt, and renders the document from that file and the template
// benchmarks.md.tmpl. Every figure in the document, including the ranges in its analysis, is
// computed from the results; TestDocumentIsUpToDate checks that the document matches them.
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
)

func main() {
	in := flag.String("in", "", "render from this results file instead of running the benchmarks")
	count := flag.Int("count", 3, "runs of each benchmark (the median is reported)")
	flag.Parse()
	if err := run(*in, *count); err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
		os.Exit(1)
	}
}

func run(in string, count int) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	resultsPath := filepath.Join(root, "bench", "results.txt")
	if in == "" {
		data, err := runBenchmarks(root, count)
		if err != nil {
			return err
		}
		if err := os.WriteFile(resultsPath, data, 0o644); err != nil {
			return err
		}
		in = resultsPath
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	doc, err := render(root, data)
	if err != nil {
		return err
	}
	out := filepath.Join(root, "docs", "benchmarks.md")
	if err := os.WriteFile(out, doc, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", out)
	return nil
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
func runBenchmarks(root string, count int) ([]byte, error) {
	var b bytes.Buffer
	commit := gitOutput(root, "rev-parse", "--short", "HEAD")
	if gitOutput(root, "status", "--porcelain", "--untracked-files=no") != "" {
		commit += " (with uncommitted changes)"
	}
	goVersion, _ := exec.Command("go", "env", "GOVERSION").Output()
	fmt.Fprintf(&b, "# date: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "# commit: %s\n", commit)
	fmt.Fprintf(&b, "# go: %s\n", strings.TrimSpace(string(goVersion)))
	fmt.Fprintf(&b, "# cores: %d\n", runtime.NumCPU())
	if load := loadAverage(); load != "" {
		fmt.Fprintf(&b, "# load: %s\n", load)
	}
	cmd := exec.Command("go", "test", "./bench", "-run", "^$", "-bench", ".", "-benchmem",
		"-count", fmt.Sprint(count), "-timeout", "0")
	cmd.Dir = root
	cmd.Stdout = io.MultiWriter(&b, os.Stderr)
	cmd.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "running %s\n", strings.Join(cmd.Args, " "))
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running the benchmarks: %w", err)
	}
	return b.Bytes(), nil
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
