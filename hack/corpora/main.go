// Command corpora fetches the third-party test corpora listed in internal/corpora/manifest.json into the cache that
// the tests of the ready-made parsers read. Run it from the root of the repository:
//
//	go run ./hack/corpora fetch [name...]   download what is missing and check its digest (all if no name)
//	go run ./hack/corpora verify [name...]  check the cached files against the digests
//	go run ./hack/corpora list              the sources, and whether each is cached
//	go run ./hack/corpora pin name          download a source and print the digest for the manifest
//	go run ./hack/corpora dir name          print the directory of a fetched source
//
// The cache is $ZP_CORPORA or zp/corpora in the user cache directory. The tests never download anything.
package main

import (
	"fmt"
	"os"

	"github.com/ornew/zeroparse/internal/corpora"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, names := os.Args[1], os.Args[2:]
	var sources []corpora.Source
	if len(names) == 0 && cmd != "pin" && cmd != "dir" {
		sources = corpora.Manifest()
	}
	for _, n := range names {
		s, ok := corpora.Lookup(n)
		if !ok {
			fatalf("unknown corpus %q (see: list)", n)
		}
		sources = append(sources, s)
	}
	failed := false
	switch cmd {
	case "list":
		for _, s := range sources {
			state := "missing"
			if _, ok := corpora.Dir(s); ok {
				state = "cached"
			}
			fmt.Printf("%-18s %-8s %s %s\n", s.Name, state, s.Revision, s.License)
		}
	case "fetch":
		for _, s := range sources {
			dir, err := corpora.Fetch(s) // also checks what is cached
			if err != nil {
				fmt.Fprintln(os.Stderr, "corpora:", err)
				failed = true
				continue
			}
			fmt.Printf("%-18s %s\n", s.Name, dir)
		}
	case "verify":
		for _, s := range sources {
			if err := corpora.Verify(s); err != nil {
				fmt.Fprintln(os.Stderr, "corpora:", err)
				failed = true
				continue
			}
			fmt.Printf("%-18s ok\n", s.Name)
		}
	case "pin":
		if len(sources) != 1 {
			usage()
		}
		sum, n, err := corpora.Pin(sources[0])
		if err != nil {
			fatalf("%v", err)
		}
		fmt.Printf("%d files, sha256 %s\n", n, sum)
	case "dir":
		if len(sources) != 1 {
			usage()
		}
		dir, ok := corpora.Dir(sources[0])
		if !ok {
			fatalf("%s is not fetched", sources[0].Name)
		}
		fmt.Println(dir)
	default:
		usage()
	}
	if failed {
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./hack/corpora fetch|verify|list [name...] | pin name | dir name")
	os.Exit(2)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "corpora: "+format+"\n", args...)
	os.Exit(1)
}
