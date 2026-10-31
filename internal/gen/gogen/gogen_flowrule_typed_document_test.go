package gogen_test

import (
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGenerateDirectTypedDocument(t *testing.T) {
	t.Parallel()
	gentest.DirectTypedDocument(t, gen.GenOptions{})
}
