// Command refgen runs the reference implementation of CUE (cuelang.org/go/cue/parser) over a corpus
// and writes its results: acceptance, the first error, and the syntax tree in a canonical form.
// The results are vendored in the testdata directory of the parent module and checked by its tests.
//
// Usage:
//
//	refgen -probe file...       print the tree or the first error of each file
//	refgen -cue DIR -stats      statistics of the corpus (the sources of a checkout of cue-lang/cue)
//	refgen -cue DIR -o OUTDIR   write OUTDIR/corpus.tar.gz, OUTDIR/mutants.txt.gz, OUTDIR/generated.txt.gz and
//	                            OUTDIR/literals.txt.gz
//	refgen -bench FILE          time the reference parser on a file (the default mode, without comments)
//	refgen -cue DIR -mutants FILE -per N -seed S   write the results of more mutations
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	probe := flag.Bool("probe", false, "print the tree and the error of each file argument")
	cue := flag.String("cue", "", "directory of a checkout of github.com/cue-lang/cue")
	stats := flag.Bool("stats", false, "print statistics of the corpus")
	listRejected := flag.Bool("rejected", false, "with -stats: list the rejected sources")
	out := flag.String("o", "", "directory to write the vendored results to")
	per := flag.Int("per", 8, "with -o: the number of mutated inputs made from each source")
	seed := flag.Uint64("seed", 0, "with -o: the first seed of the mutations")
	mutantsOnly := flag.String("mutants", "", "write only this file of results of mutations (with -per and -seed)")
	generated := flag.String("generated", "", "write only this file of results of generated inputs (with -count and -seed)")
	bench := flag.String("bench", "", "time the reference parser on this file")
	literals := flag.String("literals", "", "write only this file of decoded literals (with -cue and -seed)")
	count := flag.Int("count", 5000, "with -o or -generated: the number of inputs of each family")
	flag.Parse()
	switch {
	case *probe:
		for _, name := range flag.Args() {
			src, err := os.ReadFile(name)
			if err != nil {
				fatal(err)
			}
			r := run(name, src)
			fmt.Print(r.dump())
			fmt.Println()
		}
	case *stats:
		srcs, err := collect(*cue)
		if err != nil {
			fatal(err)
		}
		total, acc, accBytes, treeBytes := 0, 0, 0, 0
		for _, s := range srcs {
			total += len(s.Data)
			r := run(s.Name, s.Data)
			if !r.Accepted && *listRejected {
				fmt.Printf("%s\t%s\n", s.Name, r.Err)
			}
			if r.Accepted {
				acc++
				accBytes += len(s.Data)
				treeBytes += len(r.Tree)
			}
		}
		fmt.Printf("sources %d (%d bytes), accepted %d (%d bytes, trees %d bytes)\n", len(srcs), total, acc, accBytes, treeBytes)
	case *bench != "":
		src, err := os.ReadFile(*bench)
		if err != nil {
			fatal(err)
		}
		runBench(src)
	case *literals != "":
		srcs, err := collect(*cue)
		if err != nil {
			fatal(err)
		}
		if err := writeLiterals(*literals, srcs, *seed); err != nil {
			fatal(err)
		}
	case *generated != "":
		if err := writeGenerated(*generated, *count, *seed); err != nil {
			fatal(err)
		}
	case *mutantsOnly != "":
		srcs, err := collect(*cue)
		if err != nil {
			fatal(err)
		}
		if err := writeMutants(*mutantsOnly, srcs, *per, *seed); err != nil {
			fatal(err)
		}
	case *out != "":
		srcs, err := collect(*cue)
		if err != nil {
			fatal(err)
		}
		if err := writeCorpus(filepath.Join(*out, "corpus.tar.gz"), srcs); err != nil {
			fatal(err)
		}
		if err := writeMutants(filepath.Join(*out, "mutants.txt.gz"), srcs, *per, *seed); err != nil {
			fatal(err)
		}
		if err := writeGenerated(filepath.Join(*out, "generated.txt.gz"), *count, *seed); err != nil {
			fatal(err)
		}
		if err := writeLiterals(filepath.Join(*out, "literals.txt.gz"), srcs, *seed); err != nil {
			fatal(err)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "refgen:", err)
	os.Exit(1)
}

// writeCorpus writes a tar archive, compressed, with for each source the file name and the file name.ref,
// which holds the reference result: "error: " and the first error of a rejected source, or the tree and the
// comments of an accepted one.
func writeCorpus(path string, srcs []source) error {
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	for _, s := range srcs {
		if err := add(s.Name, s.Data); err != nil {
			return err
		}
		if err := add(s.Name+".ref", []byte(run(s.Name, s.Data).dump())); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// writeMutants writes, compressed, a header line "# per=N seed=S" and a line for each mutated input: the name
// of its source, its number, the FNV-1a hash of the input, 1 or 0 for accepted or rejected, and for an accepted
// input the beginning of the SHA-256 hash of the first line of its canonical tree (the comments are not compared). The sources are sorted by name, and the
// mutant m of the source i is the mutation with the seed S + i*N + m.
func writeMutants(path string, srcs []source, per int, seed uint64) error {
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	fmt.Fprintf(gz, "# per=%d seed=%d\n", per, seed)
	sorted := append([]source(nil), srcs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for i, s := range sorted {
		for m := range per {
			in := mutate(s.Data, seed+uint64(i*per+m))
			h := fnv.New64a()
			h.Write(in)
			r := run(s.Name, in)
			if r.Accepted {
				tree, _, _ := strings.Cut(r.Tree, "\n")
				sum := sha256.Sum256([]byte(tree))
				fmt.Fprintf(gz, "%s\t%d\t%016x\t1\t%x\n", s.Name, m, h.Sum64(), sum[:8])
			} else {
				fmt.Fprintf(gz, "%s\t%d\t%016x\t0\n", s.Name, m, h.Sum64())
			}
		}
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// writeGenerated writes, compressed, a header line "# count=N seed=S" and a line for each generated input:
// its family, its number, the FNV-1a hash of the input, 1 or 0 for accepted or rejected, and for an accepted
// input the beginning of the SHA-256 hash of the first line of its canonical tree. The input i of a family is
// generate(family, S+i).
func writeGenerated(path string, count int, seed uint64) error {
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	fmt.Fprintf(gz, "# count=%d seed=%d\n", count, seed)
	for _, fam := range families {
		for i := range count {
			in := generate(fam, seed+uint64(i))
			h := fnv.New64a()
			h.Write([]byte(in))
			r := run(fam, []byte(in))
			if r.Accepted {
				tree, _, _ := strings.Cut(r.Tree, "\n")
				sum := sha256.Sum256([]byte(tree))
				fmt.Fprintf(gz, "%s\t%d\t%016x\t1\t%x\n", fam, i, h.Sum64(), sum[:8])
			} else {
				fmt.Fprintf(gz, "%s\t%d\t%016x\t0\n", fam, i, h.Sum64())
			}
		}
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
