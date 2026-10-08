package duckdb_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/duckdb"
)

// BenchmarkStatements parses 2000 copies of a statement of each kind: the cost of each construct, and of the dispatch
// among the statements.
func BenchmarkStatements(b *testing.B) {
	for _, tc := range []struct{ name, stmt string }{
		{"select1", "SELECT 1;\n"},
		{"select_cols", "SELECT a, b, c FROM t;\n"},
		{"select_where", "SELECT a FROM t WHERE a = 1 AND b = 2;\n"},
		{"select_call", "SELECT sum(a), max(b) FROM t;\n"},
		{"select_alias", "SELECT a total, b AS other FROM t x;\n"},
		{"select_case", "SELECT CASE WHEN a > 1 THEN 'x' WHEN a > 2 THEN 'y' ELSE 'z' END FROM t;\n"},
		{"select_window", "SELECT row_number() OVER (PARTITION BY a ORDER BY b DESC) FROM t;\n"},
		{"select_join", "SELECT * FROM a JOIN b ON a.id = b.id LEFT JOIN c USING (id);\n"},
		{"select_group", "SELECT a, count(*) FROM t GROUP BY a HAVING count(*) > 1 ORDER BY 2 DESC LIMIT 10;\n"},
		{"select_sub", "SELECT * FROM (SELECT a FROM t) s WHERE a IN (SELECT b FROM u);\n"},
		{"select_cast", "SELECT a::INT, CAST(b AS VARCHAR), c::DECIMAL(10, 2) FROM t;\n"},
		{"select_literals", "SELECT 1, 2.5, 'x', TRUE, NULL, DATE '2020-01-01', INTERVAL 1 DAY;\n"},
		{"select_list_struct", "SELECT [1, 2, 3], {'a': 1, 'b': 2}, x[1], x.y;\n"},
		{"select_lambda", "SELECT list_transform(l, x -> x + 1), [x * 2 FOR x IN l IF x > 0];\n"},
		{"select_cte", "WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a, b;\n"},
		{"select_union", "SELECT 1 UNION ALL SELECT 2 UNION SELECT 3;\n"},
		{"select_from_first", "FROM t SELECT a, b WHERE c;\n"},
		{"select_pivot", "PIVOT t ON a USING sum(b) GROUP BY c;\n"},
		{"update", "UPDATE t SET a = 1 WHERE b = 2;\n"},
		{"insert", "INSERT INTO t VALUES (1, 2, 3);\n"},
		{"insert_select", "INSERT INTO t SELECT * FROM u ON CONFLICT DO NOTHING;\n"},
		{"delete", "DELETE FROM t WHERE a = 1;\n"},
		{"create", "CREATE TABLE t (a INT, b TEXT);\n"},
		{"create_constraints", "CREATE TABLE t (a INTEGER PRIMARY KEY, b VARCHAR NOT NULL DEFAULT 'x', FOREIGN KEY (a) REFERENCES u (id));\n"},
		{"create_as", "CREATE OR REPLACE TABLE t AS SELECT * FROM u;\n"},
		{"create_view", "CREATE VIEW v AS SELECT a FROM t;\n"},
		{"create_index", "CREATE INDEX i ON t (a, b);\n"},
		{"alter", "ALTER TABLE t ADD COLUMN c INT;\n"},
		{"drop", "DROP TABLE t;\n"},
		{"set", "SET threads = 4;\n"},
		{"copy", "COPY t TO 'out.csv' (FORMAT CSV, HEADER);\n"},
		{"attach", "ATTACH 'f.db' AS db;\n"},
		{"pragma", "PRAGMA table_info('t');\n"},
		{"explain", "EXPLAIN SELECT 1;\n"},
		{"begin", "BEGIN;\n"},
	} {
		src := strings.Repeat(tc.stmt, 2000)
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			for b.Loop() {
				if _, err := duckdb.ParseAST(src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
