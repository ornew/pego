#!/bin/sh
# Preliminary source-parser benchmark. The result ASTs are not equivalent:
# manual parsing returns grammar.Grammar; generated parsing has no adapter.
set -eu
sourcebench_repo=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
sourcebench_dir=$(mktemp -d "${TMPDIR:-/tmp}/pego-sourcebench.XXXXXX")
trap 'rm -rf "$sourcebench_dir"' 0
sourcebench_count=${COUNT:-5}
sourcebench_time=${BENCHTIME:-200ms}
case "$sourcebench_count" in ''|*[!0-9]*|0) echo 'COUNT must be a positive integer.' >&2; exit 1;; esac
[ "$sourcebench_count" -gt 0 ] || { echo 'COUNT must be a positive integer.' >&2; exit 1; }
GOMAXPROCS=${GOMAXPROCS:-16}
export GOMAXPROCS
export PEGO_SOURCE_ROOT="$sourcebench_repo"
mkdir "$sourcebench_dir/generated"
cd "$sourcebench_repo"
go run ./cmd/pego gen -g pego.pego -module sourcebaseline/generated -types -o "$sourcebench_dir/generated"
cat > "$sourcebench_dir/go.mod" <<'MOD'
module sourcebaseline

go 1.27.1
MOD
cat > "$sourcebench_dir/source_test.go" <<'GO'
package sourcebaseline

import (
	"github.com/ornew/pego"
	"os"
	"path/filepath"
	"sourcebaseline/generated"
	"testing"
)

var sink any
var samples = []struct{ Name, Src string }{
	{"Small", `rule main = [a-z]+ #atomic`},
	{"Self", read("pego.pego")},
	{"Go", read("parsers/golang/golang.pego")},
	{"Python", read("parsers/python/python.pego")},
}

func read(p string) string {
	b, e := os.ReadFile(filepath.Join(os.Getenv("PEGO_SOURCE_ROOT"), p))
	if e != nil {
		panic(e)
	}
	return string(b)
}
func TestParityCounts(t *testing.T) {
	for _, s := range samples {
		a, e := pego.ParseGrammar(s.Src)
		if e != nil {
			t.Fatal(e)
		}
		b, e := generated.ParseAST(s.Src)
		if e != nil {
			t.Fatal(e)
		}
		if len(a.Statements) != len(b.Defs) {
			t.Fatalf("%s: manual %d vs generated %d", s.Name, len(a.Statements), len(b.Defs))
		}
	}
}
func BenchmarkSource(b *testing.B) {
	for _, s := range samples {
		b.Run(s.Name, func(b *testing.B) {
			b.Run("Manual", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					n, e := pego.ParseGrammar(s.Src)
					if e != nil {
						b.Fatal(e)
					}
					sink = n
				}
			})
			b.Run("GeneratedTyped", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					n, e := generated.ParseAST(s.Src)
					if e != nil {
						b.Fatal(e)
					}
					sink = n
				}
			})
		})
	}
}
GO
cd "$sourcebench_dir"
GOWORK=off go mod edit -require github.com/ornew/pego@v0.0.0 -replace "github.com/ornew/pego=$sourcebench_repo"
GOWORK=off go test -c -o sourcebaseline.test .
./sourcebaseline.test -test.run '^TestParityCounts$'
sourcebench_round=0
while [ "$sourcebench_round" -lt "$sourcebench_count" ]; do
  for sourcebench_sample in Small Self Go Python; do
    if [ "$((sourcebench_round % 2))" -eq 0 ]; then
      sourcebench_modes='Manual GeneratedTyped'
    else
      sourcebench_modes='GeneratedTyped Manual'
    fi
    for sourcebench_mode in $sourcebench_modes; do
      ./sourcebaseline.test -test.run '^$' \
        -test.bench "^BenchmarkSource$/^$sourcebench_sample$/^$sourcebench_mode$" \
        -test.benchtime "$sourcebench_time" -test.count 1
    done
  done
  sourcebench_round=$((sourcebench_round + 1))
done
