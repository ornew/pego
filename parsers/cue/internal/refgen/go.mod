module github.com/ornew/pego/parsers/cue/internal/refgen

go 1.27.1

require (
	cuelang.org/go v0.17.1
	github.com/cockroachdb/apd/v3 v3.2.3
	github.com/ornew/pego/parsers/cue v0.0.0-00010101000000-000000000000
)

replace github.com/ornew/pego/parsers/cue => ../..
