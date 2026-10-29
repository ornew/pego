// Package core is the part of the parse runtime that every backend of the engine shares: the
// parser state (Parser), its input, memo table, expectation record, capture frames, values,
// work limits, tracing and stream state, and the protocol by which rules are called and grown.
// It also declares the parse options, results, errors and trace events that the engine re-exports.
//
// # Backends
//
// A backend runs rule bodies; core runs everything around them. A backend provides:
//
//   - a body for every rule it runs (Rule.Body). A body matches at p.Pos and returns the value
//     and whether it matched; on failure the position is unspecified, as for a Matcher, and the
//     caller restores it. Rule.Act, Rule.Value and the other executable fields of Rule are set by
//     the backend that built the rule;
//   - optionally, state it keeps beside the parser for a parse, such as the stacks of a VM, as a
//     BackendState in Parser.Backend. Core calls it where it lets go of parse state
//     (BackendState.SplitChunks, BackendState.ClearInactive); a Document keeps it from one parse
//     to the next (BackendState.Keep, BackendState.Attach, BackendState.Release). A backend
//     installs its state when a parse first needs it and finds it again through Parser.Backend.
//
// A backend may use the exported state and methods of Parser: it calls rules through Parser.Call
// (or the steps CallBegin, GrowBegin, GrowStep, GrowEnd, InvokeBegin, InvokeEnd and CallEnd when
// it keeps its own call stack, as the iterative VM does), records expectations (Parser.Expect,
// Parser.Isolate, Parser.Unisolate), saves and restores the backtracking state (Parser.Mark,
// Parser.Reset), allocates values and frames from the parser's chunks, and charges its work to
// the parser's WorkControl. A backend does not change the memo table, the input or the
// expectation record by other means, so that every backend produces the same results, errors and
// trace events for the same grammar and input.
package core
