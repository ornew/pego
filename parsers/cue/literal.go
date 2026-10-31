package cue

import (
	"strings"
	"unicode"

	"github.com/ornew/zeroparse/parsers/cue/types"
)

// The decoding of string literals is adapted from cuelang.org/go/cue/literal (Apache License 2.0,
// Copyright The CUE Authors).

// isValidImport reports whether an import path literal denotes a valid path (parser.isValidImport): graphic
// characters other than a set that paths cannot contain, ignoring the package qualifier after the last ":".
func isValidImport(path *types.String) bool {
	const illegalChars = `!"#$%&'()*,:;<=>?[\]^{|}` + "`�"
	s, _ := path.Unquote()
	if p := strings.LastIndexByte(s, ':'); p >= 0 {
		s = s[:p]
	}
	for _, r := range s {
		if !unicode.IsGraphic(r) || unicode.IsSpace(r) || strings.ContainsRune(illegalChars, r) {
			return false
		}
	}
	return s != ""
}
