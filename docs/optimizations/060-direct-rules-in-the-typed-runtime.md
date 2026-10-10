# 60. Direct rules in the typed runtime

- The typed runtime ran every rule as the Node runtime does: a method per expression, captures in a frame with a
  trail that marks and resets undo, and the action through `useCtx` and `tctx.result`, reading the frame. A CPU
  profile of JSON `ParseAST` spread the time over those methods, `setCapture` and `reset`, `newFrame` and
  `freeFrame`, and the action plumbing (about 10%). A rule without a cut or `#recover` that is neither a Pratt rule
  nor a left-recursion leader is now compiled into the method that calls it (design 012, "Direct rules";
  `gen_direct.go`): failures jump to labels, captures are Go variables that backtracking points save and restore,
  and the action is a Go expression over them, without the checks of `tctx.result` when it makes a struct with the
  rule's range. Every rule of the JSON, XML and outline grammars is direct; in the calculators, all but the Pratt
  expression and the three left-recursive rules.
- The parity test of `ParseAST` gained grammars for the new code paths (`testdata/typed/direct_*.pego`: captures and
  an environment restored by choices, optionals and repetition elements, elements with captures of their own that
  become records, `$n`, built-ins, errors in actions, recovered errors undone and replayed from the memo, `#error`,
  anchors, bounded repetitions and alternatives that can never be tried), and now runs in both position units.
  Leaving out any one restoration (captures, environment, recovered errors), the clearing of element captures at
  each iteration, or sharing one character variable among choices makes it fail.
- Effect (min of 6 interleaved runs, Apple M3 Max): `ParseAST` JSON 6.44 → 3.86 ms (−40%), XML 6.96 → 4.34 ms (−38%),
  Arith_LeftRec 14.3 → 10.3 ms (−28%), Outline 3.32 → 2.46 ms (−26%), Arith_Pratt 6.40 → 6.03 ms (−6%); XML 4.3 →
  3.2 MB (no frames), the others unchanged in bytes. JSON `ParseAST` now takes the time of `Recognize` (3.9 ms) and
  about half that of `Parse` (7.2 ms). Generated `Parse` and `Recognize` are unchanged.
- The original implementation kept cut-bearing rules and left-recursion leaders on general dispatch. [Changes 78](078-local-cuts-in-typed-direct-rules.md), [79](079-inline-eligible-typed-left-recursion-bodies.md), [80](080-inline-eligible-typed-pratt-lines.md), [81](081-scoped-cuts-in-unfinished-typed-go-bodies.md) and [82](082-match-typed-direct-frame-layouts.md) add eligible ordinary rules with local cuts, cut-free left-recursion leader bodies under the runtime-owned growth wrapper, Pratt-line matchers, scoped cut-bearing LR/Pratt bodies and additional bodies whose frame layouts now match the general emitter. `#recover` and unmatched layouts remain general.
