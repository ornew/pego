package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/editfuzz"
)

func editFuzzCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("edit-fuzz", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar source file (.zp); sequences record a hash of its exact bytes")
	start := fs.String("s", "", "start rule (default: the grammar's default start rule)")
	backends := fs.String("backend", "closure,bytecode,bytecode-iterative", "comma-separated backends to fuzz")
	units := fs.String("unit", "codepoints,bytes", "comma-separated position units to fuzz")
	seed := fs.Uint64("seed", 0, "seed of the generated sequences")
	corpus := fs.String("corpus", "", "initial texts: a file, or a directory whose files are read in name order (default: inputs generated from the grammar)")
	sequences := fs.Int("sequences", editfuzz.DefaultSequences, "sequences to run for each backend and unit")
	operations := fs.Int("operations", editfuzz.DefaultOperations, "operations in each sequence")
	save := fs.String("save-failures", "", "directory where a failing sequence is saved, required with -minimize (with -replay -minimize: default the replayed file's directory)")
	replay := fs.String("replay", "", "replay a saved sequence instead of generating sequences")
	minimize := fs.Bool("minimize", false, "minimize a failing sequence and save the result too")
	var lim limitFlags
	lim.register(fs)
	maxAttempts := fs.Int("max-attempts", editfuzz.DefaultMaxAttempts, "replays that -minimize tries at most")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	if *grammarPath == "" {
		return fmt.Errorf("-g is required")
	}
	if *sequences < 0 || *operations < 0 || *maxAttempts < 0 {
		return fmt.Errorf("-sequences, -operations and -max-attempts must not be negative")
	}
	src, err := os.ReadFile(*grammarPath)
	if err != nil {
		return err
	}
	if *replay != "" {
		// The sequence records its start rule, backend, unit and operations.
		var ignored []string
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "g", "replay", "minimize", "max-attempts", "save-failures", "budget", "max-input":
			default:
				ignored = append(ignored, "-"+f.Name)
			}
		})
		if len(ignored) > 0 {
			return fmt.Errorf("-replay does not take %s", strings.Join(ignored, ", "))
		}
		if *save != "" && !*minimize {
			return fmt.Errorf("-replay takes -save-failures only with -minimize")
		}
		return editFuzzReplay(src, *replay, *save, *minimize, *maxAttempts, lim.limits(), stdout)
	}
	if *minimize && *save == "" {
		return fmt.Errorf("-minimize needs -save-failures")
	}
	var texts [][]byte
	if *corpus != "" {
		if texts, err = readCorpus(*corpus); err != nil {
			return err
		}
	}
	var bs []zp.Backend
	for _, name := range strings.Split(*backends, ",") {
		b, err := editfuzz.ParseBackend(name)
		if err != nil {
			return err
		}
		bs = append(bs, b)
	}
	var us []zp.PositionUnit
	for _, name := range strings.Split(*units, ",") {
		u, err := editfuzz.ParseUnit(name)
		if err != nil {
			return err
		}
		us = append(us, u)
	}
	for _, b := range bs {
		for _, u := range us {
			err := editfuzz.Fuzz(src, editfuzz.Options{Start: *start, Backend: b, Unit: u, Seed: *seed, Corpus: texts,
				Limits: lim.limits(), Sequences: *sequences, Operations: *operations, SaveFailures: *save, Minimize: *minimize, MaxAttempts: *maxAttempts})
			if err != nil {
				return fmt.Errorf("%s (seed %d): %w", *grammarPath, *seed, err)
			}
			fmt.Fprintf(stdout, "ok %s/%s: %d sequences\n", b, u, cmp.Or(*sequences, editfuzz.DefaultSequences))
		}
	}
	return nil
}

func editFuzzReplay(src []byte, path, save string, minimize bool, maxAttempts int, limits zp.Limits, stdout io.Writer) error {
	seq, err := editfuzz.Load(path)
	if err != nil {
		return err
	}
	seq.Limits = limits
	err = editfuzz.Replay(src, seq)
	var m *editfuzz.Mismatch
	if !errors.As(err, &m) {
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if minimize {
			return fmt.Errorf("%s: the sequence does not fail", path)
		}
		fmt.Fprintf(stdout, "ok %s: %d operations\n", path, len(seq.Operations))
		return nil
	}
	if !minimize {
		return fmt.Errorf("%s: %w", path, err)
	}
	shortest, m, err := editfuzz.Minimize(src, seq, maxAttempts)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if save == "" {
		save = filepath.Dir(path)
	}
	saved, err := editfuzz.Save(save, shortest)
	if err != nil {
		data, _ := json.Marshal(shortest)
		return fmt.Errorf("%s: %w\nminimized to %d operations: %s\nsaving it: %v", path, m, len(shortest.Operations), data, err)
	}
	return fmt.Errorf("%s: %w\nminimized to %d operations in %s", path, m, len(shortest.Operations), saved)
}

// readCorpus reads the file at path, or the regular files in the directory at path that are not hidden,
// following symbolic links, in name order.
func readCorpus(path string) ([][]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return [][]byte{data}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var texts [][]byte
	// ReadDir returns the entries in name order.
	for _, e := range entries {
		// Skip hidden files such as .DS_Store.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := filepath.Join(path, e.Name())
		if info, err := os.Stat(name); err != nil {
			return nil, err
		} else if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		texts = append(texts, data)
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("%s: no files in the corpus directory", path)
	}
	return texts, nil
}
