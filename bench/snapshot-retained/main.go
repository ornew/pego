// Command snapshot-retained measures post-GC heap held by Document versions.
// It compares immutable Snapshot results with a legacy Parse plus Clone bundle.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/ornew/zeroparse"
)

type legacyResult struct {
	version zeroparse.DocumentVersion
	unit    zeroparse.PositionUnit
	text    string
	root    *zeroparse.Node
	err     error
	stats   zeroparse.ParseStats
}

type result struct {
	Mode             string  `json:"mode"`
	Edit             string  `json:"edit"`
	InputBytes       int     `json:"input_bytes"`
	Versions         int     `json:"versions"`
	RetainedVersions int     `json:"retained_versions"`
	Backend          string  `json:"backend"`
	Unit             string  `json:"unit"`
	RetainedBytes    int64   `json:"retained_bytes"`
	RetainedObjects  int64   `json:"retained_objects"`
	BytesPerVersion  float64 `json:"bytes_per_version"`
}

func run() error {
	mode := flag.String("mode", "snapshot", "snapshot or clone")
	edit := flag.String("edit", "replace-first", "replace-first, replace-middle, prepend, or append between versions")
	size := flag.Int("size", 4096, "input size in bytes")
	versions := flag.Int("versions", 64, "number of captured versions")
	retain := flag.Bool("retain", true, "keep captured versions until measurement")
	window := flag.Int("window", 0, "retain only the newest N versions; zero retains all")
	backendName := flag.String("backend", "closure", "closure, bytecode, or bytecode-iterative")
	unitName := flag.String("unit", "codepoints", "codepoints or bytes")
	flag.Parse()
	if *mode != "snapshot" && *mode != "clone" {
		return fmt.Errorf("invalid mode %q", *mode)
	}
	if *edit != "replace-first" && *edit != "replace-middle" && *edit != "prepend" && *edit != "append" {
		return fmt.Errorf("invalid edit %q", *edit)
	}
	if *size < 1 || *versions < 1 || *window < 0 {
		return fmt.Errorf("size and versions must be positive; window must be nonnegative")
	}
	var backend zeroparse.Backend
	switch *backendName {
	case "closure":
		backend = zeroparse.Closure
	case "bytecode":
		backend = zeroparse.Bytecode
	case "bytecode-iterative":
		backend = zeroparse.BytecodeIterative
	default:
		return fmt.Errorf("invalid backend %q", *backendName)
	}
	var unit zeroparse.PositionUnit
	switch *unitName {
	case "codepoints":
		unit = zeroparse.CodePoints
	case "bytes":
		unit = zeroparse.Bytes
	default:
		return fmt.Errorf("invalid unit %q", *unitName)
	}
	parser, err := zeroparse.CompileSource(`@start
rule main = item* $$
rule item = [a-z]`, "main")
	if err != nil {
		return err
	}
	doc, err := parser.NewDocument(strings.Repeat("a", *size), zeroparse.WithBackend(backend), zeroparse.WithUnit(unit))
	if err != nil {
		return err
	}
	if _, err = doc.Parse(); err != nil {
		return err
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var out []any
	if *retain {
		capacity := *versions
		if *window > 0 {
			capacity = min(capacity, *window)
		}
		out = make([]any, 0, capacity)
	}
	keep := func(value any) {
		if !*retain {
			return
		}
		if *window > 0 && len(out) == *window {
			copy(out, out[1:])
			out[len(out)-1] = value
		} else {
			out = append(out, value)
		}
	}
	for i := 0; i < *versions; i++ {
		if i > 0 {
			letter := "b"
			if i%2 == 0 {
				letter = "a"
			}
			start, end := 0, 1
			switch *edit {
			case "replace-middle":
				start = len(doc.Text()) / 2
				end = start + 1
			case "prepend":
				end = 0
			case "append":
				start, end = len(doc.Text()), len(doc.Text())
			}
			if err = doc.Edit(start, end, letter); err != nil {
				return err
			}
		}
		if *mode == "snapshot" {
			s, parseErr := doc.Snapshot()
			if parseErr != nil {
				return parseErr
			}
			keep(s)
		} else {
			n, parseErr := doc.Parse()
			if parseErr != nil {
				return parseErr
			}
			if *retain {
				keep(legacyResult{doc.Version(), doc.Unit(), doc.Text(), n.Clone(), parseErr, doc.Stats()})
			}
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	r := result{Mode: *mode, Edit: *edit, InputBytes: *size, Versions: *versions, Backend: *backendName, Unit: *unitName,
		RetainedBytes:   int64(after.HeapAlloc) - int64(before.HeapAlloc),
		RetainedObjects: int64(after.HeapObjects) - int64(before.HeapObjects)}
	if *retain {
		r.RetainedVersions = len(out)
	}
	if *retain {
		r.BytesPerVersion = float64(r.RetainedBytes) / float64(r.RetainedVersions)
	}
	if err = json.NewEncoder(os.Stdout).Encode(r); err != nil {
		return err
	}
	runtime.KeepAlive(out)
	runtime.KeepAlive(doc)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
