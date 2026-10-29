package gogen_test

import (
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGenerateDirectTypedDocument(t *testing.T) {
	t.Parallel()
	gentest.DirectTypedDocument(t, gen.GenOptions{})
}
