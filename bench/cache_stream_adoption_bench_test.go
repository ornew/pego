package bench

import (
	"github.com/ornew/zeroparse/internal/bench/inputs"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ornew/zeroparse"
)

const streamAdoptionElements = 50001 // inputs.CSVInput(50000) includes the header.

var (
	cacheAdoptionStats zeroparse.CacheStats
	cacheAdoptionNode  *zeroparse.Node
)

// BenchmarkStreamDeliveryAdoption compares the two public delivery interfaces
// on BenchmarkStream's unchanged CSV grammar and input. Both run to clean EOF.
// Count ok rather than non-nil nodes: a successful stream element may be nil.
func BenchmarkStreamDeliveryAdoption(b *testing.B) {
	p := load(b, "../parsers/csv/csv.zp")
	input := inputs.CSVInput(50000)
	for _, be := range backends {
		for _, u := range units {
			for _, mode := range []string{"callback", "iterator"} {
				b.Run(be.name+"/"+u.name+"/"+mode, func(b *testing.B) {
					opts := []zeroparse.ParseOption{zeroparse.WithBackend(be.b), zeroparse.WithUnit(u.u)}
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						reader := strings.NewReader(input)
						if mode == "callback" {
							err := p.ParseStream(reader, func(*zeroparse.Node) error {
								count++
								return nil
							}, opts...)
							if err != nil {
								b.Fatal(err)
							}
						} else {
							it, err := p.Iterate(reader, opts...)
							if err != nil {
								b.Fatal(err)
							}
							for {
								_, ok, err := it.Next()
								if err != nil {
									b.Fatal(err)
								}
								if !ok {
									break
								}
								count++
							}
							it.Close()
						}
						if count != streamAdoptionElements {
							b.Fatalf("delivered %d elements, want %d", count, streamAdoptionElements)
						}
					}
					b.ReportMetric(streamAdoptionElements, "elements/op")
				})
			}
		}
	}
}

// BenchmarkStreamFirstElementAdoption reports elapsed time from the public call
// until its first committed element. Parsing continues to clean EOF so this
// measurement does not include a callback-error or early-close path.
func BenchmarkStreamFirstElementAdoption(b *testing.B) {
	p := load(b, "../parsers/csv/csv.zp")
	input := inputs.CSVInput(50000)
	for _, be := range backends {
		for _, u := range units {
			for _, mode := range []string{"callback", "iterator"} {
				b.Run(be.name+"/"+u.name+"/"+mode, func(b *testing.B) {
					opts := []zeroparse.ParseOption{zeroparse.WithBackend(be.b), zeroparse.WithUnit(u.u)}
					var firstTotal time.Duration
					for b.Loop() {
						count := 0
						reader := strings.NewReader(input)
						start := time.Now()
						if mode == "callback" {
							err := p.ParseStream(reader, func(*zeroparse.Node) error {
								count++
								if count == 1 {
									firstTotal += time.Since(start)
								}
								return nil
							}, opts...)
							if err != nil {
								b.Fatal(err)
							}
						} else {
							it, err := p.Iterate(reader, opts...)
							if err != nil {
								b.Fatal(err)
							}
							for {
								_, ok, err := it.Next()
								if err != nil {
									b.Fatal(err)
								}
								if !ok {
									break
								}
								count++
								if count == 1 {
									firstTotal += time.Since(start)
								}
							}
							it.Close()
						}
						if count != streamAdoptionElements {
							b.Fatalf("delivered %d elements, want %d", count, streamAdoptionElements)
						}
					}
					b.ReportMetric(float64(firstTotal.Nanoseconds())/float64(b.N), "ns/first")
				})
			}
		}
	}
}

// BenchmarkDocumentCacheAdoption uses BenchmarkIncremental's editor workload.
// ResetAndParse measures the explicit reset plus the next parse; FreshDocument
// includes constructing and parsing a new Document with the same options.
func BenchmarkDocumentCacheAdoption(b *testing.B) {
	p := load(b, "../examples/minilang/minilang.zp")
	input := inputs.MinilangInput(300, 0)
	for _, be := range backends {
		for _, u := range units {
			for _, mode := range []string{"CacheStats", "ResetAndParse", "FreshDocument"} {
				b.Run(be.name+"/"+u.name+"/"+mode, func(b *testing.B) {
					opts := []zeroparse.ParseOption{zeroparse.WithBackend(be.b), zeroparse.WithUnit(u.u)}
					if mode == "FreshDocument" {
						b.ReportAllocs()
						for b.Loop() {
							d, err := p.NewDocument(input, opts...)
							if err != nil {
								b.Fatal(err)
							}
							cacheAdoptionNode, err = d.Parse()
							if err != nil {
								b.Fatal(err)
							}
						}
						return
					}
					d, err := p.NewDocument(input, opts...)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := d.Parse(); err != nil {
						b.Fatal(err)
					}
					before := d.CacheStats()
					if before.LiveMemoEntries == 0 {
						b.Fatal("fixture has no reusable memo entries")
					}
					b.ReportAllocs()
					b.ResetTimer()
					if mode == "CacheStats" {
						for b.Loop() {
							cacheAdoptionStats = d.CacheStats()
						}
					} else {
						for b.Loop() {
							d.ResetHistory()
							cacheAdoptionNode, err = d.Parse()
							if err != nil {
								b.Fatal(err)
							}
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(before.EstimatedBytes), "cached-B")
					if mode == "CacheStats" && cacheAdoptionStats != before {
						b.Fatalf("CacheStats changed the cache: got %+v, want %+v", cacheAdoptionStats, before)
					}
					if mode == "ResetAndParse" {
						d.ResetHistory()
						if after := d.CacheStats(); after != (zeroparse.CacheStats{}) {
							b.Fatalf("reset retained cache state: %+v", after)
						}
					}
					runtime.KeepAlive(d)
				})
			}
		}
	}
}
