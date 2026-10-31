# Sum a Column of a Big CSV File

**Problem.** A CSV export is too big to read into memory. You want the total of one column and, if a record is bad, the
place where it is, without loading the file.

The CSV grammar of [parsers/csv](../../parsers/csv/csv.zp) marks its records with `#stream`, so `ParseStream` delivers
them one at a time. Copy `csv.zp` next to this program, which sums the third column and reports the first bad record
with its offset; with `-whole` it uses `Parse` instead, to compare. See [Process a file that does not fit in
memory](big-files.md) for the same method on a log.

```go
// main.go
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ornew/zeroparse"
)

//go:embed csv.zp
var grammar string

func main() {
	whole := flag.Bool("whole", false, "read all the input and call Parse instead of ParseStream")
	flag.Parse()

	p, err := zp.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var rows int
	var total float64
	// sum adds up the third column of a record. The first record is the header.
	sum := func(rec *zp.Node) error {
		rows++
		if rows == 1 {
			return nil
		}
		fields := rec.Field("Fields").(*zp.Node)
		if fields.Len() != 3 {
			return fmt.Errorf("record %d (offset %d): want 3 fields, got %d", rows, rec.Start(), fields.Len())
		}
		v, err := strconv.ParseFloat(fields.Elem(2).Text(), 64)
		if err != nil {
			return fmt.Errorf("record %d (offset %d): %w", rows, rec.Start(), err)
		}
		total += v
		return nil
	}

	if *whole {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// main returns a File whose Records field holds all the records.
		tree, err := p.Parse(string(data))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, rec := range tree.Field("Records").(*zp.Node).Elems() {
			if err := sum(rec); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	} else if err := p.ParseStream(os.Stdin, sum); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d records, total %.2f\n", rows-1, total)
}
```

## Expected Output

Make test data, a header and then 1,000,000 records such as `7,"item, no. 7",7.25` (27 MiB), and sum it. On macOS,
`/usr/bin/time -l` reports the peak memory as "maximum resident set size":

```bash
awk 'BEGIN { print "id,name,amount"; for (i = 1; i <= 1000000; i++) printf "%d,\"item, no. %d\",%d.25\n", i, i % 100, i % 1000 }' > big.csv
go build -o csvsum .
/usr/bin/time -l ./csvsum < big.csv
```

```text
1000000 records, total 499750000.00
        1.41 real         1.08 user         0.06 sys
            12910592  maximum resident set size
```

With `-whole`, the same total takes less time and about 80 times the memory (Apple M3 Max, Go 1.27.1; the figures vary
from run to run):

```bash
/usr/bin/time -l ./csvsum -whole < big.csv
```

```text
1000000 records, total 499750000.00
        0.75 real         0.78 user         0.06 sys
          1036173312  maximum resident set size
```

Errors carry enough to locate the bad input, both from the callback and from the parser:

```bash
printf 'id,name,amount\n1,a,2.5\n2,b,oops\n3,c,1\n' | ./csvsum
printf 'id,name,amount\n1,a,2.5\n2,"b\n' | ./csvsum
```

```text
record 3 (offset 23): strconv.ParseFloat: parsing "oops": invalid syntax
4:1: syntax error: expected "\"", "\"\"", [^"]
```

The record number counts the header. The callback's error is wrapped with the record's `Start()` offset; the parser's error
says the quoted field never ended.
## How It Works

- The first record is the header: `sum` skips it by counting. `ParseStream` does not return it separately.
- The callback validates each record. Its error stops the stream and `ParseStream` returns it unchanged, so the program
  wraps it with the record's `Start()` offset.
- A record that the grammar cannot read (here, a quoted field that never ends) is a `*zp.SyntaxError` with the line and
  column in the whole input, after the records before it were delivered.

## Variations and Limits

- Line-oriented formats work the same way. The parts that matter are a record rule that cannot match the empty string at
  the end of the input (`!$$`), an `eol` that also accepts `$$`, and `$$` after the repetition, as in `csv.zp`.
- Streaming trades a little time for memory: it takes about as long as `Parse` or somewhat longer, and its memory does not
  grow with the input.
- To read a header and then records of another shape, see [Read a header and then records](header-and-records.md).
