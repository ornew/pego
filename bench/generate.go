// Package bench benchmarks the ZeroParse backends against each other and against Go standard library parsers.
//
//	go test ./bench -bench . -benchmem
//
// docs/benchmarks.md is generated from a run of these benchmarks: go run ./bench/report measures them
// and rewrites it (see package report).
//
// The generated parsers that the benchmarks run are in internal/bench/gen, written by zp gen;
// regenerate them with go generate ./bench. Document (minilangdoc, csvdoc) and stream (csvstream)
// parsers are separate packages, so the batch parsers keep their generated form. The inputs come from internal/bench/inputs.
package bench

//go:generate go run ../cmd/zp gen -g ../parsers/json/json.zp -module github.com/ornew/zeroparse/internal/bench/gen/json -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/json
//go:generate go run ../cmd/zp gen -g ../parsers/csv/csv.zp -module github.com/ornew/zeroparse/internal/bench/gen/csv -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/csv
//go:generate go run ../cmd/zp gen -g ../parsers/xml/xml.zp -module github.com/ornew/zeroparse/internal/bench/gen/xml -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/xml
//go:generate go run ../cmd/zp gen -g ../examples/calculator/calc.zp -module github.com/ornew/zeroparse/internal/bench/gen/calc -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/calc
//go:generate go run ../cmd/zp gen -g ../examples/calculator/calc_lr.zp -module github.com/ornew/zeroparse/internal/bench/gen/calclr -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/calclr
//go:generate go run ../cmd/zp gen -g ../examples/minilang/minilang.zp -module github.com/ornew/zeroparse/internal/bench/gen/minilang -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/minilang
//go:generate go run ../cmd/zp gen -g ../examples/outline/outline.zp -module github.com/ornew/zeroparse/internal/bench/gen/outline -runtime import -recognize -types -budget-controls -o ../internal/bench/gen/outline
//go:generate go run ../cmd/zp gen -g ../examples/minilang/minilang.zp -module github.com/ornew/zeroparse/internal/bench/gen/minilangdoc -runtime import -budget-controls -document -o ../internal/bench/gen/minilangdoc
//go:generate go run ../cmd/zp gen -g ../parsers/csv/csv.zp -module github.com/ornew/zeroparse/internal/bench/gen/csvdoc -runtime import -budget-controls -document -o ../internal/bench/gen/csvdoc
//go:generate go run ../cmd/zp gen -g ../parsers/csv/csv.zp -module github.com/ornew/zeroparse/internal/bench/gen/csvstream -runtime import -budget-controls -stream -o ../internal/bench/gen/csvstream
