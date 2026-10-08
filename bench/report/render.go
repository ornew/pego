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
}

var procsSuffix = regexp.MustCompile(`-\d+$`)

// parseResults reads the output of runBenchmarks.
func parseResults(data []byte) (*results, error) {
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
			if len(f) < 4 {
				continue
			}
			name := procsSuffix.ReplaceAllString(strings.TrimPrefix(f[0], "Benchmark"), "")
			vals := map[string]float64{}
			for i := 2; i+1 < len(f); i += 2 {
				v, err := strconv.ParseFloat(f[i], 64)
				if err != nil {
					return nil, fmt.Errorf("benchmark line %q: %v", line, err)
				}
				vals[f[i+1]] = v
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
	return r, nil
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

// render renders the document from the results.
func render(root string, data []byte) ([]byte, error) {
	r, err := parseResults(data)
	if err != nil {
		return nil, err
	}
	var missing []string
	get := func(name string) *result {
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
		// t is the median time per operation.
		"t": func(name string) string { return fmtTime(get(name).ns) },
		// rel is a time with its factor relative to base: "18.0 ms (1.26×)".
		"rel": func(name, base string) string {
			return fmt.Sprintf("%s (%s×)", fmtTime(get(name).ns), fmtFactor(ratio(name, base)))
		},
		// factor is the time of a relative to b: "1.26".
		"factor": func(a, b string) string { return fmtFactor(ratio(a, b)) },
		// speedup is how many times faster b is than a: "15".
		"speedup": func(a, b string) string { return fmtSig(ratio(a, b), 2) },
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
		// ratioRange is the range, over the items, of the time of a relative to b, where {w} in the
		// names stands for each item, and {s} for the part after a colon in it ("JSON:encoding_json"):
		// "0.67–0.87".
		"ratioRange": func(a, b string, items []string) string {
			lo, hi := ratioBounds(ratio, a, b, items)
			return rangeOf(fmtFactor(lo), fmtFactor(hi))
		},
		// speedupRange is the range of how many times faster b is than a: "3.1–8.0".
		"speedupRange": func(a, b string, items []string) string {
			lo, hi := ratioBounds(ratio, a, b, items)
			return rangeOf(fmtSig(lo, 2), fmtSig(hi, 2))
		},
		// mbsRatio is the throughput of a relative to b, as a fraction: "0.72".
		"mbsRatio": func(a, b string) string {
			ra, rb := get(a), get(b)
			if rb.mbs == 0 {
				return "?"
			}
			return fmtFactor(ra.mbs / rb.mbs)
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
			}
			return b
		},
		"list": func(xs ...string) []string { return xs },
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
	return out.Bytes(), nil
}

// ratioBounds returns the smallest and the largest ratio of a to b over the items (see ratioRange).
func ratioBounds(ratio func(a, b string) float64, a, b string, items []string) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, it := range items {
		w, sub, _ := strings.Cut(it, ":")
		r := strings.NewReplacer("{w}", w, "{s}", sub)
		x := ratio(r.Replace(a), r.Replace(b))
		lo, hi = min(lo, x), max(hi, x)
	}
	return lo, hi
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
