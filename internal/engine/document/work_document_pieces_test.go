//go:build !pego_work_controls_reference

package document_test

import (
	"errors"
	"strings"
	"testing"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/limits"
)

func TestWorkDocumentPieceLineTableRetry(t *testing.T) {
	prog := mustCompile(t, "var line int\nrule main = {line == 2000} .* $$")
	workDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", strings.Repeat("x\n", 4096), o)
		if err != nil {
			t.Fatal(err)
		}
		o.WorkLimits.MaxInputWork = 100
		if _, err = d.ParseWith(o); !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("line table interruption: %v", err)
		}
		if d.Input().Lines != nil {
			t.Fatal("interrupted scan retained a partial line table")
		}
		workDocumentAssertRetry(t, d)
	})
}

func TestWorkDocumentPieceLiteralRetry(t *testing.T) {
	text := strings.Repeat("a", 8192)
	prog := mustCompile(t, "rule main = "+`"`+text+`" $$`)
	workDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", text, o)
		if err != nil {
			t.Fatal(err)
		}
		o.WorkLimits.MaxInputWork = 100
		if _, err = d.ParseWith(o); !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("piece literal bypassed the limit: %v", err)
		}
		workDocumentAssertRetry(t, d)
	})
}
