package main

import "testing"

func TestSavedConstructLabelKeepsMeasurements(t *testing.T) {
	r, err := parseResults([]byte("# commit: original\nBenchmarkParse/Arith_Pratt/closure/codepoints-8 1 123 ns/op 456 B/op 7 allocs/op\n"))
	if err != nil {
		t.Fatal(err)
	}
	value := r.bench["Parse/Arith_OperatorsRule/closure/codepoints"]
	if r.meta["commit"] != "original" || value == nil || value.ns != 123 || value.bytesPerOp != 456 || value.allocs != 7 || len(r.order) != 1 {
		t.Fatalf("changed saved measurements: %+v", r)
	}
}
