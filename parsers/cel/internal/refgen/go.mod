module github.com/ornew/zeroparse/parsers/cel/internal/refgen

go 1.27.1

require (
	cel.dev/cel-go v0.32.0
	github.com/ornew/zeroparse/parsers/cel v0.0.0-00010101000000-000000000000
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/ornew/zeroparse/parsers/cel => ../..

replace github.com/ornew/zeroparse => ../../../..
