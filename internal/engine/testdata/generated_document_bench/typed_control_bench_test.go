package generateddocumentbench

import (
	"reflect"
	"strconv"
	"testing"
	"unicode/utf8"

	typedbatch "generateddocumentbench/typedbatch"
	typeddocumentconv "generateddocumentbench/typeddocumentconv"
)

var typedControlResult []*typedbatch.Item
var typedConversionControlResult []*typeddocumentconv.Item

// BenchmarkTypedNearEOFControl is the batch control for the typed near-EOF edit path: the batch
// ParseAST of the edited input, generated without Document support. It is not the O37 control,
// which is BenchmarkTypedNearEOFConversionControl.
func BenchmarkTypedNearEOFControl(b *testing.B) {
	for _, records := range []int{256, 4096} {
		base := typedBenchmarkInput(records)
		alternate := base[:len(base)-2] + "4;"
		for _, unit := range []struct {
			name string
			unit typedbatch.PositionUnit
		}{
			{"CodePoints", typedbatch.CodePoints},
			{"Bytes", typedbatch.Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+unit.name, func(b *testing.B) {
				option := typedbatch.WithUnit(unit.unit)
				if _, err := typedbatch.ParseAST(alternate, option); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					input := alternate
					if i&1 != 0 {
						input = base
					}
					items, err := typedbatch.ParseAST(input, option)
					if err != nil {
						b.Fatal(err)
					}
					typedControlResult = items
				}
			})
		}
	}
}

// TestGeneratedTypedConversionControlPreflight checks that the conversion control returns the same
// typed values as a fresh parse after the same edit.
func TestGeneratedTypedConversionControlPreflight(t *testing.T) {
	for _, records := range []int{256, 4096} {
		base := typedBenchmarkInput(records)
		alternate := base[:len(base)-2] + "4;"
		for _, unit := range []typeddocumentconv.PositionUnit{typeddocumentconv.CodePoints, typeddocumentconv.Bytes} {
			pos := len(base) - 2
			if unit == typeddocumentconv.CodePoints {
				pos = utf8.RuneCountInString(base) - 2
			}
			d, err := typeddocumentconv.NewDocument(base, typeddocumentconv.WithUnit(unit))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.ParseAST(); err != nil {
				t.Fatal(err)
			}
			if err := d.Edit(pos, pos+1, "4"); err != nil {
				t.Fatal(err)
			}
			got, gotErr := d.ParseAST()
			want, wantErr := typeddocumentconv.ParseAST(alternate, typeddocumentconv.WithUnit(unit))
			if gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) || d.Stats().Reused == 0 {
				t.Fatalf("%d/%v: conversion control differs from fresh parse: %v / %v, stats %+v", records, unit, gotErr, wantErr, d.Stats())
			}
		}
	}
}

// BenchmarkTypedNearEOFConversionControl is the same-source disabled control of direct typed Document
// matching (docs/design/044): the same edit and parse as BenchmarkTypedDocumentNearEOF, with the
// generated Document.ParseAST converting the Node result.
func BenchmarkTypedNearEOFConversionControl(b *testing.B) {
	for _, records := range []int{256, 4096} {
		base := typedBenchmarkInput(records)
		for _, unit := range []struct {
			name string
			unit typeddocumentconv.PositionUnit
		}{
			{"CodePoints", typeddocumentconv.CodePoints},
			{"Bytes", typeddocumentconv.Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+unit.name, func(b *testing.B) {
				pos := len(base) - 2
				if unit.unit == typeddocumentconv.CodePoints {
					pos = utf8.RuneCountInString(base) - 2
				}
				d, err := typeddocumentconv.NewDocument(base, typeddocumentconv.WithUnit(unit.unit))
				if err != nil {
					b.Fatal(err)
				}
				if got, err := d.ParseAST(); err != nil || len(got) != records {
					b.Fatalf("preflight: %d items, %v", len(got), err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					replacement := "4"
					if i&1 != 0 {
						replacement = "3"
					}
					if err := d.Edit(pos, pos+1, replacement); err != nil {
						b.Fatal(err)
					}
					got, err := d.ParseAST()
					if err != nil {
						b.Fatal(err)
					}
					typedConversionControlResult = got
				}
			})
		}
	}
}
