package postgresql_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/postgresql"
)

// benchInput is a script of about 256 KB of queries and data changes of the kinds that applications send,
// like the SQL workload of the benchmarks of PEGO (bench/ in github.com/ornew/pego).
var benchInput = func() string {
	tables := []string{"orders", "customers", "products", "events", "invoices", "sessions"}
	cols := []string{"id", "created_at", "amount", "status", "region", "owner_id", "name", "score"}
	r := rand.New(rand.NewPCG(1, 2))
	t := func() string { return tables[r.IntN(len(tables))] }
	c := func() string { return cols[r.IntN(len(cols))] }
	var b strings.Builder
	for i := 0; b.Len() < 256<<10; i++ {
		switch i % 5 {
		case 0:
			fmt.Fprintf(&b, "SELECT a.%s, b.%s AS \"B Col\", count(*) FILTER (WHERE a.%s > %d) AS n, coalesce(sum(a.%s), 0)::numeric(12, 2)\n"+
				"  FROM %s AS a LEFT JOIN %s b ON b.id = a.%s AND b.%s IS NOT NULL\n"+
				"  WHERE a.%s BETWEEN %d AND %d AND a.%s IN ('x', 'y', 'z') AND NOT a.%s LIKE 'p%%'\n"+
				"  GROUP BY 1, 2 HAVING count(*) > %d ORDER BY n DESC, 1 LIMIT %d OFFSET %d;\n",
				c(), c(), c(), r.IntN(1000), c(), t(), t(), c(), c(), c(), r.IntN(100), r.IntN(1000)+100, c(), c(), r.IntN(10), r.IntN(100), r.IntN(1000))
		case 1:
			fmt.Fprintf(&b, "INSERT INTO %s (%s, %s, %s) VALUES (%d, 'text %d', now() + interval '%d days'), (%d, E'it''s\\n', NULL)\n"+
				"  ON CONFLICT (id) DO UPDATE SET %s = EXCLUDED.%s RETURNING id;\n",
				t(), c(), c(), c(), r.IntN(1e6), r.IntN(1e6), r.IntN(30), r.IntN(1e6), c(), c())
		case 2:
			fmt.Fprintf(&b, "UPDATE %s SET %s = %s + %d, %s = CASE WHEN %s > %d THEN 'big' ELSE 'small' END WHERE id = $1 AND %s <> ALL (ARRAY[%d, %d, %d]);\n",
				t(), c(), c(), r.IntN(100), c(), c(), r.IntN(1000), c(), r.IntN(10), r.IntN(10), r.IntN(10))
		case 3:
			fmt.Fprintf(&b, "WITH recent AS (SELECT * FROM %s WHERE %s > current_date - %d), ranked AS (\n"+
				"  SELECT *, row_number() OVER (PARTITION BY %s ORDER BY %s DESC) AS rn FROM recent)\n"+
				"SELECT r.*, (SELECT max(%s) FROM %s x WHERE x.id = r.id) FROM ranked r WHERE rn <= %d\n"+
				"UNION ALL SELECT * FROM %s WHERE EXISTS (SELECT 1 FROM %s WHERE %s = $2);\n",
				t(), c(), r.IntN(90), c(), c(), c(), t(), r.IntN(10), t(), t(), c())
		case 4:
			fmt.Fprintf(&b, "DELETE FROM %s USING %s WHERE %s.%s = %s.id AND %s.%s < now() - interval '1 year' RETURNING *;\n",
				t(), t(), "orders", c(), "orders", "orders", c())
		}
	}
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := postgresql.ParseAST(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := postgresql.Parse(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecognize(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if err := postgresql.Recognize(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSplit(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if len(postgresql.Split(benchInput)) == 0 {
			b.Fatal("no statements")
		}
	}
}

// BenchmarkRegress parses the statements of PostgreSQL's regression scripts that the parser accepts, one at a
// time, as a tool that reads a SQL file statement by statement would.
func BenchmarkRegress(b *testing.B) {
	var stmts []string
	size := 0
	for _, s := range loadCorpus(b) {
		if s.OK {
			if _, err := postgresql.ParseAST(s.SQL); err == nil {
				stmts = append(stmts, s.SQL)
				size += len(s.SQL)
			}
		}
	}
	b.SetBytes(int64(size))
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range stmts {
			if _, err := postgresql.ParseAST(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}
