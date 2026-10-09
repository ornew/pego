// Command pgdev is a development tool (not committed): it parses the statements of a corpus
// (jsonl.gz written by parsers/postgresql/internal/refgen/gen.py) with a grammar through the engine and
// writes, one JSON object per line, whether each statement was accepted and the tree.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ornew/pego"
)

type rec struct {
	F    string `json:"f"`
	L    int    `json:"l"`
	SQL  string `json:"sql"`
	OK   bool   `json:"ok"`
	Err  string `json:"err"`
	Tree any    `json:"tree"`
}

type out struct {
	I    int             `json:"i"`
	OK   bool            `json:"ok"`
	Err  string          `json:"err,omitempty"`
	Tree json.RawMessage `json:"tree,omitempty"`
}

func main() {
	g := flag.String("g", "", "grammar")
	c := flag.String("c", "", "corpus (jsonl.gz)")
	o := flag.String("o", "", "output (jsonl)")
	files := flag.String("files", "", "only statements of these files (comma separated)")
	every := flag.Int("every", 1, "only every n-th statement")
	trees := flag.Bool("trees", true, "write trees")
	flag.Parse()
	src, err := os.ReadFile(*g)
	if err != nil {
		panic(err)
	}
	t0 := time.Now()
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "compiled in %v\n", time.Since(t0))
	f, err := os.Open(*c)
	if err != nil {
		panic(err)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		panic(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var w *bufio.Writer
	if *o != "" {
		of, err := os.Create(*o)
		if err != nil {
			panic(err)
		}
		defer of.Close()
		w = bufio.NewWriterSize(of, 1<<20)
		defer w.Flush()
	}
	only := map[string]bool{}
	if *files != "" {
		for _, x := range strings.Split(*files, ",") {
			only[x] = true
		}
	}
	i := -1
	var nAccept, nReject, nAgree, nDisagree int
	t1 := time.Now()
	var bytes int
	for sc.Scan() {
		i++
		if i%*every != 0 {
			continue
		}
		var r rec
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			panic(err)
		}
		if len(only) > 0 && !only[r.F] {
			continue
		}
		bytes += len(r.SQL)
		ts := time.Now()
		n, err := p.Parse(r.SQL)
		if d := time.Since(ts); d > 20*time.Millisecond {
			fmt.Fprintf(os.Stderr, "slow %v: %s:%d %.80q\n", d, r.F, r.L, r.SQL)
		}
		ok := err == nil
		if ok {
			nAccept++
		} else {
			nReject++
		}
		if ok == r.OK {
			nAgree++
		} else {
			nDisagree++
		}
		if w != nil {
			o := out{I: i, OK: ok}
			if err != nil {
				e := err.Error()
				if len(e) > 300 {
					e = e[:300]
				}
				o.Err = e
			}
			if ok && *trees {
				o.Tree, _ = json.Marshal(n)
			}
			b, _ := json.Marshal(o)
			w.Write(b)
			w.WriteByte('\n')
		}
	}
	fmt.Fprintf(os.Stderr, "accepted %d rejected %d; agree with the reference on %d, disagree on %d; %v for %d bytes\n", nAccept, nReject, nAgree, nDisagree, time.Since(t1), bytes)
}
