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

	"github.com/ornew/pego"
)

type legacyResult struct {
	version pego.DocumentVersion
	unit    pego.Unit
	text    string
	root    *pego.Node
	err     error
	stats   pego.ParseStats
}

type result struct {
	Mode            string  `json:"mode"`
	InputBytes      int     `json:"input_bytes"`
	Versions        int     `json:"versions"`
	Backend         string  `json:"backend"`
	Unit            string  `json:"unit"`
	RetainedBytes   int64   `json:"retained_bytes"`
	RetainedObjects int64   `json:"retained_objects"`
	BytesPerVersion float64 `json:"bytes_per_version"`
}

func run() error {
	mode := flag.String("mode", "snapshot", "snapshot or clone")
	size := flag.Int("size", 4096, "input size in bytes")
	versions := flag.Int("versions", 64, "number of retained versions")
	backendName := flag.String("backend", "closure", "closure, bytecode, or bytecode-iterative")
	unitName := flag.String("unit", "codepoints", "codepoints or bytes")
	flag.Parse()
	if *mode != "snapshot" && *mode != "clone" {
		return fmt.Errorf("invalid mode %q", *mode)
	}
	if *size < 1 || *versions < 1 {
		return fmt.Errorf("size and versions must be positive")
	}
	var backend pego.Backend
	switch *backendName {
	case "closure":
		backend = pego.Closure
	case "bytecode":
		backend = pego.Bytecode
	case "bytecode-iterative":
		backend = pego.BytecodeIterative
	default:
		return fmt.Errorf("invalid backend %q", *backendName)
	}
	var unit pego.Unit
	switch *unitName {
	case "codepoints":
		unit = pego.CodePoints
	case "bytes":
		unit = pego.Bytes
	default:
		return fmt.Errorf("invalid unit %q", *unitName)
	}
	parser, err := pego.CompileSource(`rule main = item* $$
rule item = [a-z]`, "main")
	if err != nil {
		return err
	}
	doc, err := parser.NewDocument(strings.Repeat("a", *size), pego.WithBackend(backend), pego.WithUnit(unit))
	if err != nil {
		return err
	}
	if _, err = doc.Parse(); err != nil {
		return err
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out := make([]any, 0, *versions)
	for i := 0; i < *versions; i++ {
		if i > 0 {
			letter := "b"
			if i%2 == 0 {
				letter = "a"
			}
			if err = doc.Edit(0, 1, letter); err != nil {
				return err
			}
		}
		if *mode == "snapshot" {
			s, parseErr := doc.Snapshot()
			if parseErr != nil {
				return parseErr
			}
			out = append(out, s)
		} else {
			n, parseErr := doc.Parse()
			if parseErr != nil {
				return parseErr
			}
			out = append(out, legacyResult{doc.Version(), doc.Unit(), doc.Text(), n.Clone(), parseErr, doc.Stats()})
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	r := result{Mode: *mode, InputBytes: *size, Versions: *versions, Backend: *backendName, Unit: *unitName,
		RetainedBytes:   int64(after.HeapAlloc) - int64(before.HeapAlloc),
		RetainedObjects: int64(after.HeapObjects) - int64(before.HeapObjects)}
	r.BytesPerVersion = float64(r.RetainedBytes) / float64(*versions)
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
