# 063. Configurable Optimization Pass Pipeline

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-25

## Summary

The passes over Core IR are called by two hand-written sequences, and the generator decides which run through
scattered booleans. This design makes a pass a declared unit and the sequence a pipeline that a small manager runs:

- each pass declares its name, the analyses it requires and invalidates, its mode applicability and its
  obligations;
- a flag per optimization turns a pass or one of its variants off;
- an optional verifier runs between passes;
- named flag sets (generation profiles `speed`, `memory` and `size`) select a coherent set of optimizations;
- the module can be printed after any pass.

The default pipeline reproduces today's generated output byte for byte. Users select a profile with `pego gen
-optimize`, and per-optimization flags and the IR dump are available as options for development and advanced use.

## Motivation

The passes run as follows today, and this is the inventory the proposal starts from.

- The sequence is written twice. `generator.typedCore` (`internal/engine/core_build.go`) runs `Terminals`,
  `RewriteTerms`, `Analyze`, `Guards`, `Scans`, `ClassRuns`, `Analyze`, `Verify`. `generator.startRecFlow`
  (`internal/engine/gen_flow_recognizer.go`) runs the same minus `Terminals` and `ClassRuns`. The order matters and is
  nowhere declared: `Guards` reads what the first `Analyze` computes, and a missing `Analyze` leaves choices unguarded
  without an error (`startRecFlow` carries a comment about 84,000 calls on the DuckDB grammar that the wrong order
  caused).
- Applicability is derived in several places: `lean` (no work controls and no Documents), `leanTyped`, `observability`
  and the private `disable*` fields of `GenOptions`. A pass such as `ClassRuns` is sound only in lean builds, because
  work controls and Documents charge for reading; that obligation is encoded by an `if`, not stated.
- Only some optimizations have a switch (`disableTerminalPrograms`, `disableGuardedClasses`,
  `disableLiteralTextDispatch`, `boxedComparisons`). `Guards`, `Scans` and `ClassRuns` have no switch, so no test
  checks that generation without them equals the engine.
- `Verify` runs once, after the last pass; a pass that breaks an invariant is reported at the end, with no pass named.
  `flow.Verify` runs only in tests.
- The only debugging aid is the test hook `decisions.coreHook`, which is called after each pass in the typed route.
  There is no way to print the module after a pass from a command.
- Generation profiles do not exist. The size and speed of generated parsers are decided by one fixed set of
  optimizations ([record 062](062-backend-independent-optimization-ir.md) lists the measured mechanisms); a user who
  wants a smaller parser has no lever except removing features.

Record 062 requires every optimization to be a pass with a stated precondition, a private same-source switch and a
negative-control test. This record gives that rule a mechanism.

## Goals

- One declaration per pass: name, requirements, invalidations, applicability, obligations and the flag that controls
  it. Both call sites run pipelines built from those declarations.
- The default pipeline produces the same Core IR, the same Flow IR and the same generated source as today for every
  grammar and generator mode.
- Every optimization pass has a flag, and a test that a build without it equals the engine.
- A pass that breaks a Core IR invariant is named, when verification between passes is on.
- The Core IR can be printed after any pass.
- A profile is a named flag set; `speed` is today's behavior.

## Non-Goals

- Passes over Flow IR. Flow IR has no optimization passes today (lowering makes the choices; see
  [Flow IR](../intermediate-representations.md#flow-ir)). The manager is written for Core IR and can host Flow passes
  when they exist.
- The textual pass that rewrites generated Go (`qualifyRuntime`). It stays a text pass: record 062 moves the
  other rewrites into the printers and leaves the cost of this one to an independent fix.
- The closure backend, bytecode and TypeScript, which do not use Core IR today.
- Changes to what a parser does. Flags and profiles change generated code, never results, errors or events.
- A textual pipeline language such as LLVM's `-passes=`.

## Design

### Pass Declarations

A pass is a value with the following fields. The names are those of the pipeline code, not of any public API.

| Field | Meaning |
|:--|:--|
| `Name` | Stable identifier, also the flag name and the print-after name |
| `Requires` | Analyses that must be valid when the pass starts |
| `Invalidates` | Analyses that the pass makes stale |
| `After` | Passes that must precede it when both are enabled (an ordering constraint with no data dependence) |
| `Applies` | A predicate over the generation mode (work controls, Documents, observability, table) |
| `Obligation` | One sentence stating what the pass preserves, as a comment and in the test name |
| `Run` | The function over `*core.Module` |

Analyses are the data that passes read and that other passes make stale. The manager knows two:
`call-graph` (`core.CallGraph`) and `attributes` (`core.Analyze`: `Nullable`, `First`, `Eff`). The attributes that the
builder sets (`Want`, `Shape`) are inputs, not analyses.

The five existing passes, declared:

| Pass | Requires | Invalidates | After | Applies when (today) | Obligation |
|:--|:--|:--|:--|:--|:--|
| `terminals` | | `attributes` | | Typed table, lean, no observability | Same input consumed and expectations recorded as the expression it replaces |
| `term-rewrites` | | | | Always | Same results and errors, in the same order |
| `guards` | `attributes` | | `terminals` | Always; guarded classes only when lean | A guard equals the alternative's first terminal |
| `scans` | | `attributes` | | Always | Same input consumed and expectations recorded |
| `class-runs` | `attributes` | `attributes` | `guards` | Typed table, lean | Same input consumed and expectations recorded; the first class of a guarded alternative is left alone |

`Analyze` is not a pass in this model but the way the manager makes `attributes` valid. It is deterministic over an
unchanged tree, so how often it runs never changes the output.

### The Manager

`Run(m, pipeline, options)` runs the enabled passes in order. Before each pass it makes the pass's required analyses
valid, recomputing the stale ones. After a pass it marks the invalidated ones stale. It fails with the pass name if a
pass lists a requirement the manager cannot compute, if two enabled passes violate an `After` order, or if verification
fails.

Applied to the typed table with every optimization enabled, the manager performs the same calls as `typedCore` today:
`attributes` is stale after `terminals` and is recomputed before `guards`; it is stale again after `scans` and
`class-runs` and is recomputed once before the final verification. The recognizer pipeline is the same list without
`terminals` and `class-runs`, and `Scan` origins are followed as `followRepeats` does.

The manager lives in `internal/ir/core` next to the passes and depends on nothing of the engine. The mode
predicate is a value the generator passes in (`Mode`), so `core` keeps no knowledge of work controls or Documents.

### Flags

Each optimization has a flag with the pass name. Variants of one pass have their own flags.

| Flag | Controls | Replaces today |
|:--|:--|:--|
| `terminals` | `terminals` | `disableTerminalPrograms` |
| `term-rewrites` | `term-rewrites` | |
| `text-dispatch` | `TextDispatch` of `term-rewrites` | `disableLiteralTextDispatch` |
| `field-compare` | `FieldCompare` of `term-rewrites` | `boxedComparisons` |
| `guards` | `guards` | none |
| `guarded-classes` | The `classes` argument of `guards`, and `flow.Options.GuardedClasses` | `disableGuardedClasses` |
| `scans` | `scans` | none |
| `class-runs` | `class-runs` | none |
| `class-tables` | `flow.Options.ClassTables` | the `leanTyped` condition |
| `inline-literals` | `flow.Options.InlineLiterals` (a count, 0 turns it off) | the constant `maxInlineLiteral` |

A flag can only turn an optimization off. Turning it on never overrides `Applies`: `class-runs` stays off in a build
with work controls whatever the flag says, because the pass is unsound there. The mode facts of `flow.Options` (`Work`,
`Recovers`, `Observe`) are not optimizations and have no flag.

`guards` off implies `guarded-classes` off and makes `class-runs` convert every class run, which `After` allows.

### Verification Between Passes

With verification on, the manager makes `attributes` valid and calls `core.Verify` after every pass and names the
pass that produced the first violation. Off (the default), it verifies once at the end, as today. Verification
between passes can run more analyses than the default pipeline; the output does not change.

### Profiles

A profile is a named set of flags. `speed` is the default and sets every flag on, which is today's behavior and
therefore changes no generated output.

| Flag | `speed` | `memory` | `size` |
|:--|:--|:--|:--|
| `terminals` | on | on | open |
| `term-rewrites`, `text-dispatch`, `field-compare` | on | on | on |
| `guards`, `guarded-classes` | on | on | open |
| `scans` | on | on | on |
| `class-runs` | on | on | open |
| `class-tables` | on | open | off |
| `inline-literals` | 4 | open | 0 |

The `memory` and `size` columns are hypotheses. A table is data and a straight-line expression is code, and the first
may be smaller or larger depending on the grammar; guards add code and remove calls. None of these trade-offs is
measured for this record. Each cell marked open is filled by measuring generated source size, binary size and resident
memory on the checked-in parsers, and the measured result goes in the profile's table and in
`docs/optimizations/`. The controls that today change run-time memory without being passes (`disableTypedObjectSlabs`,
`disableTypedListSlabs`, `disableTypedOutputBootstrap`) are candidates for the `memory` profile
but are not Core passes; whether a profile also sets them is an open question.

The word "profile" already names the rule profiler (`pego profile`, `WithProfile`), so the flag that selects a profile is `-optimize`.

### Print After Pass

The manager takes an optional `Dump func(pass string, m *core.Module)`, called with the pass name after each pass that
ran, and with `build` before the first. It replaces the test-only `decisions.coreHook`: the tests become users of the
same hook. A command-line front end prints with `core.Print(m, true)`, and the Flow functions of a rule with
`flow.String`, to a file or to standard error. The selection is a pass name or `all`, as in LLVM's `-print-after`.

### Migrating the Call Sites

1. Introduce the pass declarations, the manager and the two pipelines in `core`, together with a test that holds the
   old straight-line sequences as an oracle. For every grammar of the corpus and every mode of `coreGenModes`, run
   the oracle and the pipeline and compare `core.Print(m, true)`. No call site changes.
2. Switch `typedCore` and `startRecFlow` to the pipelines. Gate on three checks: `go generate ./parsers ./bench` leaves
   no diff, the corpus tests of the generator pass in default and `PEGO_TEST_ORDINARY=1` runs, and a byte comparison
   of the generated sources of the corpus before and after the switch (all modes) is empty.
3. Replace the `disable*` booleans with the flag set. Existing tests set flags instead of fields; the flag names come
   from the table above.
4. Remove the oracle with the old sequences once step 2 has landed.

Each step is its own commit and can be reverted alone.

### User-Facing Surface

Three surfaces were considered.

**Option A: Internal only.** Flags exist for tests and diagnostics through the private `GenOptions` fields. `pego gen`
and the Go options do not change. Profiles are not offered.

**Option B: Profiles.** `pego gen -optimize speed|memory|size` and a Go option `WithOptimize` select a profile.
Per-pass flags stay internal.

**Option C: Profiles and per-optimization flags.** Option B plus `pego gen -no-opt name[,name]` and
`WithoutOptimizations(names ...string)`, for reproducing a miscompilation or measuring one optimization, and
`-print-after name|all` (with a Go callback) for the IR dump.

Decision: **C, with the profile flag named `-optimize`.** `pego gen -optimize speed|memory|size` and the Go option
`WithOptimize` select a profile; `-no-opt name[,name]`, `WithoutOptimizations(names ...string)` and `-print-after
name|all` serve development and advanced use, such as isolating a miscompilation or measuring one optimization. A
profile is applied first and `-no-opt` removes optimizations from it, so `-optimize size -no-opt guards` is `size`
without `guards`. The name `-optimize` avoids the rule profiler's `pego profile` and `WithProfile`. The flags are
described in the command-line reference only; no guide explains individual optimizations, and the
[specification of generated parsers](../../../spec/runtime/generated-parsers.md) does not change, because it constrains the
parser's behavior, not the generated code. Optimization names follow the pass set, which record 062 expects to
change.

Any option leaves the generated API, results, errors and events unchanged. Documentation changes are the command-line
reference and the code-generation guide; the specification does not change.

## Alternatives Considered

**Keep the call sites as they are, and add the missing switches.** Cheapest, and fixes the missing negative controls.
It leaves the order undeclared and the duplication in place, so the next pass is added in two places and the
wrong-order failure remains silent. Rejected as the end state, but the missing switches are the first deliverable of
this design anyway.

**A fixed list without analyses.** A pipeline as a slice of functions, ordered by hand, with each pass calling
`Analyze` itself. Smaller than a manager. It spends analysis time where a pass might not need it and cannot express a
requirement, so it cannot report the silent mis-ordering that motivated this record. Deferred, not excluded: if
declared requirements prove not to be worth their code, this is the fallback.

**Full LLVM-style analysis management.** Cached analyses with preserved-set computation, per-function and per-module
scopes, and a textual pipeline language. The two analyses that exist (`call-graph`, `attributes`) are cheap to
recompute and deterministic, so a cache with preservation sets buys little; per-rule scopes would matter only if
passes became rule-local. Rejected for now.

**Profiles chosen by the generator from the grammar.** The generator could pick `size` or `speed` by grammar size.
It makes the output depend on a heuristic that users cannot predict or override. Rejected.

**Flags only on the command line, with no Go option.** Matches how `pego gen` options are read today, but the
Go API (`GenerateGo`) is what the build tooling of other projects calls. The options mirror each other today
(`-types` and `WithTypes`) and the new ones should too.

## Testing

- Pass declarations: a test that every pass in the registry has a flag, a documented obligation and a test of its
  disabled form (a table-driven check over the registry fails for a pass without them).
- The oracle comparison of step 1 and the byte comparison of step 2 above.
- Disabled passes: for each flag, generate the corpus with the flag off and run the existing generated-parser
  differential (`testGeneratedParsersCorpus` and `testGeneratedTypesCorpus`), which compares results and errors with
  the engine in both position units, as `TestTerminalProgramDisabledParity` does for `terminals`. A negative control per
  flag checks that turning it off changes the generated source for a grammar where the pass applies, so a flag that
  does nothing is caught.
- Combinations: each flag off, all optional flags off, and a seeded sample of pairs, with `Verify` after every pass.
  The full 2^n product is not run.
- Mode applicability: a test that `class-runs`, `terminals` and guarded classes do not run in builds with work
  controls, Documents or (for `terminals`) observability, whatever the flags say.
- Manager: a pass that requires an analysis that is stale gets it recomputed; a pass that violates `After` fails the
  run naming both passes; verification names the pass that broke an invariant (using a deliberately broken test pass).
- Print after pass: the dump hook is called once per pass in order, and the printed text of the default pipeline
  equals the text printed by the test hook today.
- Profiles: `speed` output equals the default output byte for byte; each profile's parsers pass the existing
  generated-parser corpus.

None of these tests exists yet; they land with the pipeline.

## Performance and Results

The default pipeline must not change generation time beyond noise: the manager adds a constant number of checks to a
sequence of passes that is linear in the grammar. Measure `GenerateGo` with `WithTypes` on the DuckDB grammar before
and after step 2 with the method of [record 062](062-backend-independent-optimization-ir.md#performance-and-results).
The `memory` and `size` presets are measured as described under [Profiles](#profiles) before they are named or
documented; until then they do not ship.

## Limitations and Open Questions

- **What `memory` and `size` contain.** Their flag sets are hypotheses until measured. Whether they also set the
  typed slab controls, and whether `size` also changes the runtime embedded in the parser, is open.
- **Granularity of `term-rewrites`.** Each rewrite (`TextEq`, `ConcatFused`, `ProjectedMap`) could have its own flag. Today
  no printer reads the rewritten terms, so a flag changes only the Core IR, and splitting it earns nothing until the
  Flow printer takes terms from Core.
- **Flow passes.** When Flow IR gains passes, the manager runs them, and the flag and profile tables grow. Whether
  Flow lowering's options (`class-tables`, `inline-literals`) become passes is decided with them.
- **Observability and work controls as passes.** They are modes, not optimizations, so they stay out of the flag set;
  generated parsers keep fixing them at generation time.
- **Missing negative controls.** `guards`, `scans` and `class-runs` have none today; whether a build without them equals
  the engine is expected but unverified, and is the first result this design needs.
