package duckdb_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/duckdb"
)

// benchScript is an analytical script of about 256 KB: tables, CTEs with joins, aggregates, window functions,
// subqueries, CASE expressions, casts, lists and structs, set operations and some DML, as an analyst writes them.
var benchScript = func() string {
	r := rand.New(rand.NewPCG(1, 2))
	tables := []string{"orders", "customers", "lineitem", "events", "sessions", "payments"}
	cols := []string{"id", "amount", "created_at", "country", "status", "quantity", "price", "user_id", "category"}
	pick := func(xs []string) string { return xs[r.IntN(len(xs))] }
	var b strings.Builder
	for i := 0; b.Len() < 256<<10; i++ {
		t1, t2 := pick(tables), pick(tables)
		c1, c2, c3 := pick(cols), pick(cols), pick(cols)
		switch i % 6 {
		case 0:
			fmt.Fprintf(&b, `-- report %d
CREATE OR REPLACE TABLE report_%d AS
WITH base AS (
    SELECT o.%s, o.%s, c.%s AS customer_%s, sum(o.%s * 1.1)::DECIMAL(18, 2) AS total,
           count(*) FILTER (WHERE o.%s > %d) AS big,
           row_number() OVER (PARTITION BY c.%s ORDER BY o.%s DESC) AS rn
    FROM %s AS o
    LEFT JOIN %s c ON c.id = o.%s AND c.%s IS NOT NULL
    WHERE o.%s BETWEEN DATE '2024-01-01' AND DATE '2024-12-31' AND o.%s NOT IN ('a', 'b', 'c')
    GROUP BY ALL
    HAVING total > %d
), ranked AS (
    SELECT *, rank() OVER (ORDER BY total DESC) AS r FROM base QUALIFY rn <= 3
)
SELECT * EXCLUDE (rn) FROM ranked ORDER BY r LIMIT %d;
`, i, i, c1, c2, c3, c1, c2, c1, r.IntN(1000), c3, c2, t1, t2, c3, c1, c2, c3, r.IntN(100), r.IntN(50)+1)
		case 1:
			fmt.Fprintf(&b, `SELECT %s, list_transform(array_agg(%s ORDER BY %s), x -> x * 2) AS doubled,
       {'name': %s, 'values': [1, 2, 3]} AS s, map {'k': %s} AS m,
       CASE WHEN %s > %d THEN 'high' WHEN %s > %d THEN 'mid' ELSE 'low' END AS bucket,
       strftime(%s::TIMESTAMP, '%%Y-%%m') AS month, coalesce(%s, 0) + abs(%s) AS x
FROM %s
WHERE %s LIKE 'x%%' OR regexp_matches(%s, '^[a-z]+$')
GROUP BY 1, 4, 5 ORDER BY 1 NULLS LAST;
`, c1, c2, c3, c1, c2, c3, r.IntN(100), c2, r.IntN(10), c1, c2, c3, t1, c1, c2)
		case 2:
			fmt.Fprintf(&b, `INSERT INTO %s (%s, %s, %s) SELECT %s, %s, %s FROM %s WHERE %s > (SELECT avg(%s) FROM %s) ON CONFLICT DO NOTHING;
`, t1, c1, c2, c3, c1, c2, c3, t2, c2, c2, t1)
		case 3:
			fmt.Fprintf(&b, `SELECT a.%s, b.%s, sum(a.%s) OVER (ORDER BY a.%s ROWS BETWEEN 6 PRECEDING AND CURRENT ROW) AS moving
FROM %s a ASOF JOIN %s b ON a.%s >= b.%s
UNION ALL
SELECT %s, %s, 0 FROM %s WHERE EXISTS (SELECT 1 FROM %s WHERE %s = %s.%s);
`, c1, c2, c3, c1, t1, t2, c1, c1, c1, c2, t1, t2, c1, t1, c1)
		case 4:
			fmt.Fprintf(&b, `UPDATE %s SET %s = %s + 1, %s = upper(%s) WHERE %s IN (SELECT %s FROM %s WHERE %s IS NULL);
DELETE FROM %s WHERE %s < now() - INTERVAL 30 DAY;
`, t1, c1, c1, c2, c2, c3, c3, t2, c1, t1, c2)
		case 5:
			fmt.Fprintf(&b, `PIVOT %s ON %s USING sum(%s) GROUP BY %s;
CREATE VIEW v_%d AS SELECT * FROM read_parquet('data/%s_%d.parquet') WHERE %s = $1;
COPY (SELECT * FROM %s) TO 'out_%d.csv' (FORMAT CSV, HEADER);
`, t1, c1, c2, c3, i, t2, i, c1, t1, i)
		}
	}
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	b.SetBytes(int64(len(benchScript)))
	for b.Loop() {
		if _, err := duckdb.ParseAST(benchScript); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchScript)))
	for b.Loop() {
		if _, err := duckdb.Parse(benchScript); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecognize(b *testing.B) {
	b.SetBytes(int64(len(benchScript)))
	for b.Loop() {
		if err := duckdb.Recognize(benchScript); err != nil {
			b.Fatal(err)
		}
	}
}

// TestWriteBenchScript writes the script of the benchmarks to the file named by BENCH_SCRIPT, to time another
// parser on it (the README gives the command for DuckDB's).
func TestWriteBenchScript(t *testing.T) {
	path := os.Getenv("BENCH_SCRIPT")
	if path == "" {
		t.Skip("set BENCH_SCRIPT to a file name")
	}
	if err := os.WriteFile(path, []byte(benchScript), 0o644); err != nil {
		t.Fatal(err)
	}
}
