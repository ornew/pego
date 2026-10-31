package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

//go:embed benchmarks.md.tmpl
var docTemplate string

// result is the median of the runs of one benchmark.
type result struct {
	ns, bytesPerOp, allocs, mbs float64
	metrics                     map[string]float64 // custom metrics (b.ReportMetric)
	runs                        int
}

// results holds a benchmark run: its description and the median of each benchmark, by name
// without the "Benchmark" prefix and the GOMAXPROCS suffix (for example "Parse/JSON/closure/codepoints").
type results struct {
	meta  map[string]string // date, commit, go, cores, load (from the header), cpu, goos, goarch (from go test)
	bench map[string]*result
	order []string // names in the order of the run
}

var procsSuffix = regexp.MustCompile(`-\d+$`)

// savedLabel maps a benchmark name recorded in a saved results file to its current name. Saved
// measurements retain their recorded names; the unchanged arithmetic workload is rendered under its
// current construct name.
//
// The compiled-grammar label is mapped only because the saved raw results predate the rename of the
// compiled-grammar extension (the sub-benchmark name is the extension, now "zpc"). The next results capture
// records the new name, and the second mapping is then removed.
func savedLabel(name string) string {
	name = strings.ReplaceAll(name, "/Arith_Pratt/", "/Arith_OperatorsRule/")
	return strings.ReplaceAll(name, "/pegoc/", "/zpc/") // keep-old-name: saved raw results predate the rename
}

// parseResults reads the output of runBenchmarks.
func parseResults(data []byte) (*results, error) { return parseBenchLines(data, true) }

// parseBenchLines reads benchmark lines in the format of go test -bench. With benchmem the lines
// must carry B/op and allocs/op; the TypeScript lines (captured from Node) carry only the time.
func parseBenchLines(data []byte, benchmem bool) (*results, error) {
	r := &results{meta: map[string]string{}, bench: map[string]*result{}}
	runs := map[string][]map[string]float64{}
	var order []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# "):
			if k, v, ok := strings.Cut(line[2:], ": "); ok {
				r.meta[k] = v
			}
		case strings.HasPrefix(line, "cpu: "), strings.HasPrefix(line, "goos: "), strings.HasPrefix(line, "goarch: "):
			k, v, _ := strings.Cut(line, ": ")
			r.meta[k] = v
		case strings.HasPrefix(line, "Benchmark"):
			f := strings.Fields(line)
			if len(f) < 4 || len(f)%2 != 0 {
				return nil, fmt.Errorf("benchmark line %q: want iteration count and complete metric pairs", line)
			}
			iterations, err := strconv.ParseUint(f[1], 10, 64)
			if err != nil || iterations == 0 {
				return nil, fmt.Errorf("benchmark line %q: invalid iteration count %q", line, f[1])
			}
			name := procsSuffix.ReplaceAllString(strings.TrimPrefix(f[0], "Benchmark"), "")
			name = savedLabel(name)
			vals := map[string]float64{}
			for i := 2; i+1 < len(f); i += 2 {
				v, err := strconv.ParseFloat(f[i], 64)
				if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("benchmark line %q: invalid %s value %q", line, f[i+1], f[i])
				}
				if _, exists := vals[f[i+1]]; exists {
					return nil, fmt.Errorf("benchmark line %q: duplicate %s metric", line, f[i+1])
				}
				vals[f[i+1]] = v
			}
			for _, unit := range []string{"ns/op", "B/op", "allocs/op"} {
				v, ok := vals[unit]
				if !ok && !benchmem && unit != "ns/op" {
					continue
				}
				if !ok {
					return nil, fmt.Errorf("benchmark line %q: missing required %s metric", line, unit)
				}
				if v < 0 || unit == "ns/op" && v == 0 {
					return nil, fmt.Errorf("benchmark line %q: invalid %s value %v", line, unit, v)
				}
			}
			if previous := runs[name]; len(previous) > 0 && !sameMetricSet(previous[0], vals) {
				return nil, fmt.Errorf("benchmark line %q: metric units differ between runs of %s", line, name)
			}
			if _, ok := runs[name]; !ok {
				order = append(order, name)
			}
			runs[name] = append(runs[name], vals)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("no benchmark results")
	}
	for _, name := range order {
		rs := runs[name]
		res := &result{metrics: map[string]float64{}, runs: len(rs)}
		units := map[string]bool{}
		for _, v := range rs {
			for u := range v {
				units[u] = true
			}
		}
		for u := range units {
			var xs []float64
			for _, v := range rs {
				if x, ok := v[u]; ok {
					xs = append(xs, x)
				}
			}
			m := median(xs)
			switch u {
			case "ns/op":
				res.ns = m
			case "B/op":
				res.bytesPerOp = m
			case "allocs/op":
				res.allocs = m
			case "MB/s":
				res.mbs = m
			default:
				res.metrics[u] = m
			}
		}
		r.bench[name] = res
	}
	r.order = order
	return r, nil
}

func sameMetricSet(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for unit := range a {
		if _, ok := b[unit]; !ok {
			return false
		}
	}
	return true
}

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// render renders the document from the results of the Go benchmarks alone.
func render(root string, data []byte) ([]byte, error) { return renderAll(root, data, nil, nil) }

// renderAll renders the document from the Go benchmark results and, when they are not nil, the
// TypeScript results (see tsLines) and the generated sizes (see sizeLines).
func renderAll(root string, data, tsData, sizeData []byte) ([]byte, error) {
	r, err := parseResults(data)
	if err != nil {
		return nil, err
	}
	if tsData != nil {
		ts, err := parseBenchLines(tsData, false)
		if err != nil {
			return nil, fmt.Errorf("TypeScript results: %w", err)
		}
		for k, v := range ts.meta {
			r.meta["ts."+k] = v
		}
		for _, name := range ts.order {
			if !strings.HasPrefix(name, "TS/") {
				return nil, fmt.Errorf("TypeScript results: benchmark %q does not start with TS/", name)
			}
			if _, dup := r.bench[name]; dup {
				return nil, fmt.Errorf("TypeScript results: duplicate benchmark %q", name)
			}
			r.bench[name] = ts.bench[name]
			r.order = append(r.order, name)
		}
	}
	var sizes map[string]map[string]float64
	if sizeData != nil {
		if sizes, err = parseSizes(sizeData, r.meta); err != nil {
			return nil, fmt.Errorf("size results: %w", err)
		}
	}
	var missing []string
	used := map[string]bool{}
	get := func(name string) *result {
		used[name] = true
		if b := r.bench[name]; b != nil {
			return b
		}
		missing = append(missing, name)
		return &result{metrics: map[string]float64{}}
	}
	// ratio returns the time of a relative to b.
	ratio := func(a, b string) float64 {
		ra, rb := get(a), get(b)
		if rb.ns == 0 {
			return 0
		}
		return ra.ns / rb.ns
	}
	funcs := template.FuncMap{
		"meta": func(k string) string {
			if v, ok := r.meta[k]; ok {
				return v
			}
			missing = append(missing, "meta "+k)
			return ""
		},
		"hasMeta": func(k string) bool { _, ok := r.meta[k]; return ok },
		// hasTS and hasSizes report whether the TypeScript results and the sizes were given.
		"hasTS":    func() bool { return tsData != nil },
		"hasSizes": func() bool { return sizeData != nil },
		// tsCell is the time of a TypeScript benchmark with its throughput: "21.4 ms (12 MB/s)".
		"tsCell": func(name string) string {
			b := get(name)
			return fmt.Sprintf("%s (%s MB/s)", fmtTime(b.ns), fmtSig(b.mbs, 2))
		},
		// tsRel is the factor of the time of a TypeScript benchmark relative to a Go benchmark.
		"tsRel": func(name, base string) string { return fmtFactor(ratio(name, base)) },
		// sizeParsers are the parsers of the size table.
		"sizeParsers": func() []string { return sizeParsers },
		// sizeOf is a size of a generated parser: metric is go-source, go-binary, ts-raw or ts-gzip.
		"sizeOf": func(parser, metric string) string {
			v, ok := sizes[parser][metric]
			if !ok {
				missing = append(missing, "size "+parser+" "+metric)
			}
			return fmtBytes(v)
		},
		// t is the median time per operation.
		"t": func(name string) string { return fmtTime(get(name).ns) },
		// rel is a time with its factor relative to base: "18.0 ms (1.26×)".
		"rel": func(name, base string) string {
			return fmt.Sprintf("%s (%s×)", fmtTime(get(name).ns), fmtFactor(ratio(name, base)))
		},
		// mem is bytes and allocations per operation: "10.6 MB / 0.8 k".
		"mem": func(name string) string {
			b := get(name)
			return fmt.Sprintf("%s / %s", fmtBytes(b.bytesPerOp), fmtCount(b.allocs))
		},
		"bytes": func(name string) string { return fmtBytes(get(name).bytesPerOp) },
		// mbs is the throughput: "22 MB/s".
		"mbs": func(name string) string { return fmtSig(get(name).mbs, 2) + " MB/s" },
		// size is the input size, from the throughput and the time.
		"size": func(name string) string {
			b := get(name)
			return fmtBytes(b.mbs * 1e6 * b.ns / 1e9)
		},
		"metric": func(name, unit string) float64 {
			v, ok := get(name).metrics[unit]
			if !ok {
				missing = append(missing, name+" "+unit)
			}
			return v
		},
		"fmtBytes": fmtBytes,
		// fileSize is the size of a file of the repository.
		"fileSize": func(path string) string {
			st, err := os.Stat(filepath.Join(root, path))
			if err != nil {
				missing = append(missing, path)
				return ""
			}
			return fmtBytes(float64(st.Size()))
		},
		// runs is the number of runs of a benchmark.
		"runs": func(name string) int { return get(name).runs },
		// stdCell is the time of the standard-library parser of workload w (from items "W:name"), or "–".
		"stdCell": func(w string, items []string) string {
			for _, it := range items {
				if wn, name, _ := strings.Cut(it, ":"); wn == w {
					return fmt.Sprintf("%s (`%s`)", fmtTime(get("Parse/"+w+"/"+name).ns), stdPackage(name))
				}
			}
			return "–"
		},
		// stdMem is the allocation of the standard-library parser of workload w, or "–".
		"stdMem": func(w string, items []string) string {
			for _, it := range items {
				if wn, name, _ := strings.Cut(it, ":"); wn == w {
					b := get("Parse/" + w + "/" + name)
					return fmt.Sprintf("%s / %s", fmtBytes(b.bytesPerOp), fmtCount(b.allocs))
				}
			}
			return "–"
		},
		"backendName": func(b string) string {
			switch b {
			case "closure":
				return "Closure"
			case "bytecode":
				return "Bytecode (recursive)"
			case "iterative":
				return "Bytecode (iterative)"
			case "generated":
				return "Generated Go"
			}
			return b
		},
		"list":   func(xs ...string) []string { return xs },
		"inList": func(x string, xs []string) bool { return slices.Contains(xs, x) },
		// has reports whether the results include a benchmark.
		"has": func(name string) bool { return r.bench[name] != nil },
		// allocs is the number of allocations per operation.
		"allocs": func(name string) string { return fmtCount(get(name).allocs) },
		// all lists every benchmark of the run, in its order, for the table of all results.
		"all": func() []string {
			var names []string
			for _, n := range r.order {
				if !strings.HasPrefix(n, "TS/") {
					names = append(names, n)
				}
			}
			return names
		},
		// row is the row of a benchmark in the table of all results.
		"row": func(name string) string {
			b := get(name)
			mbs := "–"
			if b.mbs > 0 {
				mbs = fmtSig(b.mbs, 3)
			}
			var extra []string
			for k, v := range b.metrics {
				extra = append(extra, fmt.Sprintf("%s %s", fmtSig(v, 3), k))
			}
			slices.Sort(extra)
			other := "–"
			if len(extra) > 0 {
				other = strings.Join(extra, ", ")
			}
			return fmt.Sprintf("| `%s` | %s | %s | %s | %s | %s |", name, fmtTime(b.ns), mbs, fmtBytes(b.bytesPerOp), fmtCount(b.allocs), other)
		},
	}
	tmpl, err := template.New("benchmarks.md").Funcs(funcs).Parse(docTemplate)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, nil); err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("the results lack %s", strings.Join(slices.Compact(missing), ", "))
	}
	// The Go results and the TypeScript results are separate runs; each must be complete.
	wantRuns := map[bool]int{}
	for name := range used {
		if b := r.bench[name]; b != nil {
			ts := strings.HasPrefix(name, "TS/")
			wantRuns[ts] = max(wantRuns[ts], b.runs)
		}
	}
	for name := range used {
		if b := r.bench[name]; b != nil && b.runs != wantRuns[strings.HasPrefix(name, "TS/")] {
			return nil, fmt.Errorf("benchmark %s has %d runs, want %d to match the complete saved run", name, b.runs, wantRuns[strings.HasPrefix(name, "TS/")])
		}
	}
	return out.Bytes(), nil
}

// stdPackage names the standard-library parser of a benchmark.
func stdPackage(name string) string {
	switch name {
	case "encoding_json":
		return "encoding/json"
	case "encoding_csv":
		return "encoding/csv"
	case "encoding_xml":
		return "encoding/xml"
	case "go_parser":
		return "go/parser"
	}
	return name
}

func rangeOf(lo, hi string) string {
	if lo == hi {
		return lo
	}
	return lo + "–" + hi
}

// fmtSig formats x with n significant digits, without an exponent.
func fmtSig(x float64, n int) string {
	if x == 0 {
		return "0"
	}
	d := n - 1 - int(math.Floor(math.Log10(math.Abs(x))))
	if d < 0 {
		p := math.Pow(10, float64(-d))
		return strconv.FormatFloat(math.Round(x/p)*p, 'f', 0, 64)
	}
	return strconv.FormatFloat(x, 'f', d, 64)
}

// fmtTime formats nanoseconds with three significant digits: "14.3 ms", "730 µs".
func fmtTime(ns float64) string {
	switch {
	case ns >= 1e9:
		return fmtSig(ns/1e9, 3) + " s"
	case ns >= 1e6:
		return fmtSig(ns/1e6, 3) + " ms"
	case ns >= 1e3:
		return fmtSig(ns/1e3, 3) + " µs"
	}
	return fmtSig(ns, 3) + " ns"
}

// fmtFactor formats a ratio: two decimals.
func fmtFactor(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }

// fmtBytes formats a size with three significant digits: "20.0 MB", "262 KB".
func fmtBytes(b float64) string {
	switch {
	case b >= 1e6:
		return fmtSig(b/1e6, 3) + " MB"
	case b >= 1e3:
		return fmtSig(b/1e3, 3) + " KB"
	}
	return fmtSig(b, 3) + " B"
}

// fmtCount formats a number of allocations: "767", "1.2 k", "53 k".
func fmtCount(n float64) string {
	if n >= 1e3 {
		return fmtSig(n/1e3, 2) + " k"
	}
	return strconv.Itoa(int(math.Round(n)))
}

// parseSizes reads the lines "size PARSER METRIC BYTES" of the size results; header lines
// "# key: value" go to meta under the prefix "size.". Metric "runtime" holds the shared TypeScript runtime.
func parseSizes(data []byte, meta map[string]string) (map[string]map[string]float64, error) {
	sizes := map[string]map[string]float64{}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			if k, v, ok := strings.Cut(line[2:], ": "); ok {
				meta["size."+k] = v
			}
		case strings.HasPrefix(line, "size "):
			f := strings.Fields(line)
			if len(f) != 4 {
				return nil, fmt.Errorf("line %q: want size PARSER METRIC BYTES", line)
			}
			n, err := strconv.ParseUint(f[3], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %q: invalid byte count", line)
			}
			if sizes[f[1]] == nil {
				sizes[f[1]] = map[string]float64{}
			}
			if _, dup := sizes[f[1]][f[2]]; dup {
				return nil, fmt.Errorf("line %q: duplicate metric", line)
			}
			sizes[f[1]][f[2]] = float64(n)
		}
	}
	if len(sizes) == 0 {
		return nil, fmt.Errorf("no sizes")
	}
	return sizes, nil
}
