// Command retained measures the post-GC heap owned by compiled parsers.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/ornew/pego"
	"github.com/ornew/pego/bench"
)

const forcedGCs = 3

type options struct {
	repo   string
	corpus string
	count  int
	warmup int
}

type report struct {
	Corpus            string   `json:"corpus"`
	Count             int      `json:"count"`
	Warmup            int      `json:"warmup"`
	ForcedGCs         int      `json:"forced_gcs"`
	GoVersion         string   `json:"go_version"`
	GOOS              string   `json:"goos"`
	GOARCH            string   `json:"goarch"`
	GOMAXPROCS        int      `json:"gomaxprocs"`
	BuildTags         string   `json:"build_tags"`
	SourceSHA256      string   `json:"source_sha256"`
	InputSHA256       string   `json:"input_sha256"`
	ResultSHA256      string   `json:"result_sha256"`
	FingerprintFormat string   `json:"fingerprint_format"`
	BeforeHeapAlloc   uint64   `json:"before_heap_alloc"`
	AfterHeapAlloc    uint64   `json:"after_heap_alloc"`
	BeforeHeapObjects uint64   `json:"before_heap_objects"`
	AfterHeapObjects  uint64   `json:"after_heap_objects"`
	BeforeNumGC       uint32   `json:"before_num_gc"`
	AfterNumGC        uint32   `json:"after_num_gc"`
	RetainedBytes     int64    `json:"retained_bytes"`
	RetainedObjects   int64    `json:"retained_objects"`
	BytesPerParser    *float64 `json:"bytes_per_parser"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	o := options{}
	fs := flag.NewFlagSet("retained", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.repo, "repo", ".", "repository containing the benchmark grammars")
	fs.StringVar(&o.corpus, "corpus", "json", "json, xml, arithmetic or minilang")
	fs.IntVar(&o.count, "count", 128, "compiled parsers to retain; zero is the empty control")
	fs.IntVar(&o.warmup, "warmup", 4, "discarded compilations before measurement")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || o.count < 0 || o.warmup < 1 {
		return fmt.Errorf("expected no positional arguments, count >= 0 and warmup >= 1")
	}
	source, input, err := corpus(o.repo, o.corpus)
	if err != nil {
		return err
	}
	result, err := warm(source, input, o.warmup)
	if err != nil {
		return err
	}
	r := report{
		Corpus: o.corpus, Count: o.count, Warmup: o.warmup, ForcedGCs: forcedGCs,
		GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		GOMAXPROCS: runtime.GOMAXPROCS(0), SourceSHA256: digest(source),
		InputSHA256: digest(input), ResultSHA256: result, FingerprintFormat: fingerprintFormat,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "-tags" {
				r.BuildTags = setting.Value
			}
		}
	}

	// Allocate the owner slice before the baseline so its backing storage is
	// excluded. Input and source remain alive across both snapshots.
	owners := make([]*pego.Parser, o.count)
	before := liveHeap()
	for i := range owners {
		owners[i], err = pego.CompileSource(source, "main")
		if err != nil {
			return fmt.Errorf("compilation %d: %w", i, err)
		}
	}
	after := liveHeap()
	// These calls must follow the last snapshot; otherwise the compiler may
	// make owners or the validation input dead before the forced collections.
	runtime.KeepAlive(owners)
	runtime.KeepAlive(input)
	runtime.KeepAlive(source)
	r.BeforeHeapAlloc, r.AfterHeapAlloc = before.HeapAlloc, after.HeapAlloc
	r.BeforeHeapObjects, r.AfterHeapObjects = before.HeapObjects, after.HeapObjects
	r.BeforeNumGC, r.AfterNumGC = before.NumGC, after.NumGC
	r.RetainedBytes = int64(after.HeapAlloc) - int64(before.HeapAlloc)
	r.RetainedObjects = int64(after.HeapObjects) - int64(before.HeapObjects)
	if o.count != 0 {
		per := float64(r.RetainedBytes) / float64(o.count)
		r.BytesPerParser = &per
	}
	return json.NewEncoder(out).Encode(r)
}

func corpus(repo, name string) (source, input string, err error) {
	var path string
	switch name {
	case "json":
		path, input = "parsers/json/json.pego", bench.JSONInput(256<<10)
	case "xml":
		path, input = "parsers/xml/xml.pego", bench.XMLInput(256<<10)
	case "arithmetic":
		path, input = "examples/calculator/calc.pego", bench.ArithInput(20000)
	case "minilang":
		path, input = "examples/minilang/minilang.pego", bench.MinilangInput(300, 0)
	default:
		return "", "", fmt.Errorf("unknown corpus %q", name)
	}
	data, err := os.ReadFile(filepath.Join(repo, path))
	return string(data), input, err
}

//go:noinline
func warm(source, input string, count int) (string, error) {
	var result string
	for i := 0; i < count; i++ {
		p, err := pego.CompileSource(source, "main")
		if err != nil {
			return "", fmt.Errorf("warmup compilation %d: %w", i, err)
		}
		if i == 0 {
			n, err := p.Parse(input, pego.WithBackend(pego.Closure), pego.WithUnit(pego.CodePoints))
			if err != nil {
				return "", fmt.Errorf("full-input validation: %w", err)
			}
			result, err = fingerprint(n)
			if err != nil {
				return "", err
			}
		}
	}
	return result, nil
}

func liveHeap() runtime.MemStats {
	for range forcedGCs {
		runtime.GC()
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
