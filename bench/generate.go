// Package bench benchmarks the PEGO backends against each other and against Go standard library parsers.
//
//	go test ./bench -bench . -benchmem
//
// docs/benchmarks.md is generated from a run of these benchmarks: go run ./bench/report measures them
// and rewrites it (see package report).
//
// The generated parsers that the benchmarks run are in internal/bench/gen, written by pego gen;
// regenerate them with go generate ./bench. Document (minilangdoc, csvdoc) and stream (csvstream)
// parsers are separate packages, so the batch parsers keep their generated form. The inputs come from internal/bench/inputs.
package bench

//go:generate go run ../cmd/pego gen -g ../parsers/json/json.pego -module github.com/ornew/pego/internal/bench/gen/json -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/json
//go:generate go run ../cmd/pego gen -g ../parsers/csv/csv.pego -module github.com/ornew/pego/internal/bench/gen/csv -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/csv
//go:generate go run ../cmd/pego gen -g ../parsers/xml/xml.pego -module github.com/ornew/pego/internal/bench/gen/xml -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/xml
//go:generate go run ../cmd/pego gen -g ../examples/calculator/calc.pego -module github.com/ornew/pego/internal/bench/gen/calc -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/calc
//go:generate go run ../cmd/pego gen -g ../examples/calculator/calc_lr.pego -module github.com/ornew/pego/internal/bench/gen/calclr -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/calclr
//go:generate go run ../cmd/pego gen -g ../examples/minilang/minilang.pego -module github.com/ornew/pego/internal/bench/gen/minilang -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/minilang
//go:generate go run ../cmd/pego gen -g ../examples/outline/outline.pego -module github.com/ornew/pego/internal/bench/gen/outline -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/outline
//go:generate go run ../cmd/pego gen -g ../examples/minilang/minilang.pego -module github.com/ornew/pego/internal/bench/gen/minilangdoc -runtime import -budget-controls -document -o ../internal/bench/gen/minilangdoc
//go:generate go run ../cmd/pego gen -g ../parsers/csv/csv.pego -module github.com/ornew/pego/internal/bench/gen/csvdoc -runtime import -budget-controls -document -o ../internal/bench/gen/csvdoc
//go:generate go run ../cmd/pego gen -g ../parsers/csv/csv.pego -module github.com/ornew/pego/internal/bench/gen/csvstream -runtime import -budget-controls -stream -o ../internal/bench/gen/csvstream
