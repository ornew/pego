# 010. Bytecode VM

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-07

> **Note**: The normative definition of the instruction set and the file format is [bytecode.md](../bytecode.md). The instruction names and operands sketched in this record are the original plan and differ in detail from the final instruction set.

## Summary

Add a backend that compiles grammars into language-independent **bytecode** and executes it on a small **VM**.
The existing closure backend is kept, and the backend can be chosen according to the use case.

| Backend | Strengths | Intended use |
|:--|:--|:--|
| Closure (existing) | Speed within Go, simple implementation | Embedding in Go programs when speed matters |
| Bytecode (new) | Can be saved and distributed, easy to port to other languages, runs on a small runtime | Distributing grammars, runtimes in other languages, smaller generated code |

Both backends return the same results for the same grammar (the node tree including positions, syntax errors and recovered errors).
The closure backend also serves as the reference (oracle) for checking the correctness of the bytecode backend.

## Goals

- Document the instruction set and the file format as a specification independent of Go. A runtime in another language can execute the same bytecode by implementing the VM and the built-in functions according to the specification.
- Support every current language feature: memoization, left recursion, Pratt expressions, predicates and variables, actions, `#error`, `#recover`, `#stream`, and incremental parsing.
- Keep the runtime small. The compiler (parsing, static analysis, type checking, code generation) is not part of the runtime.
- Measure performance: for JSON parsing, compare `encoding/json`, the closure backend, the bytecode backend (and the generated Go parser).

## Non-goals

- Implementing runtimes in other languages (this design covers the specification and the Go implementation).
- JIT compilation of bytecode, or generating optimized code from bytecode (future work).

## Overview

```
grammar.Grammar ──Compile──▶ engine.Program ─┬─ backend: closure  ──▶ executed as closures
                                             └─ backend: bytecode ──▶ Module ──▶ executed on the VM
                                                                        │  ▲
                                                               Marshal ─┘  └─ Load (.pegoc)
```

- `engine.Compile` performs syntax, type and static analysis as it does now, and then builds an executable form for each backend.
- The unit of bytecode is called a **module**. A module is a collection of instruction sequences and tables, and it is the only thing the runtime reads.
- In the public API, the backend and execution model are chosen with an option (for example `pego.Compile(g, start, pego.WithBackend(pego.BytecodeIterative))`). Based on the performance comparison ([decision 4](#4-default-backend)), the default is the closure backend when the AST is available and bytecode otherwise.

## Execution models

### Two execution models

The same instruction set can be executed by either of two execution models, and the user chooses between them ([decision 1](#1-execution-model)).

| Execution model | Rule calls | Strengths | Weaknesses |
|:--|:--|:--|:--|
| **Recursive** | Host-language function calls | Small, portable runtime. Structurally close to the closure backend, so the two are easy to keep consistent. | Nesting depth is limited by the host stack. |
| **Iterative** | The VM's own call stack | Deeply nested input does not consume the host stack. Parsing can be suspended and resumed. | Left recursion, Pratt, recovery and so on are handled as stack entries, so the runtime is larger. |

The instruction set and the module do not depend on the execution model. The recursive model is implemented first and the iterative model is added later.

### Recursive model

The VM runs **one execution loop per rule body**. A rule call (`CALL`) calls the VM's execution function recursively (a host-language function call).
Backtracking within a rule body uses a **backtrack stack** per body.

Reasons for this shape:

- Memoization, left-recursion seed growing, Pratt longest match (trying candidates and backing up), `#recover`, and lambda calls from built-in functions in actions all have the form "run a part and receive its result". Using host recursion lets them be written with almost the same structure as in the closure backend, which keeps the two consistent.
- It can be ported to any language with recursion, and the runtime stays small.

As with the closure backend, the maximum nesting depth is determined by the host stack.

### Iterative model

Everything, including calls, runs in a single execution loop with the VM's own stack (as in LPeg).
`CALL` pushes a call entry and jumps to the body, and `RETURN` pops the entry and returns. When a call entry is popped while unwinding on failure, the VM stores the failure in the memo or finishes the growth of a left recursion.
The processing that the recursive model implements as runtime functions (the left-recursion growth loop, the Pratt loop, `#recover`, and lambda calls from built-in functions) is implemented as state machines whose state is held in stack entries.
Because the stack does not depend on the host, parsing can stop while waiting for input to arrive and resume later (which also allows stream parsing in which the caller pushes input).

### Alternatives considered

| Approach | Decision |
|:--|:--|
| Interpreting the expression tree directly | Easy to port, but no different from interpreting the current AST, and it lacks the speed advantage of a flat instruction sequence. |

### State

| State | Contents |
|:--|:--|
| Input | A sequence of Unicode code points or of UTF-8 bytes ([position units](#position-units)) |
| `pos` | Current position |
| Value stack | Values built by matching and actions |
| Backtrack stack | Choice points and similar within a rule body (below) |
| Capture frames | Capture slots for a rule body and for each repetition element |
| Write history (trail) | Writes to captures, undone on backtracking |
| Variable environment | A persistent list (as now) |
| Memo | (rule, position, level) → result, end position, examined range, recovered errors, expectations |
| Error records | Farthest failure position, expectations, `silent`, recovered errors |

### Backtrack stack entries

On failure, the VM pops an entry from the backtrack stack and handles it according to its kind.

| Kind | Pushed by | When popped on failure |
|:--|:--|:--|
| Choice | `CHOICE` | Restore the state and go to the next alternative. If cut, restore the state and continue failing. |
| Repetition | `REPEAT` | Restore the state and finish the repetition (if cut, continue failing). |
| Lookahead | `AND`, `NOT` | Handle as a lookahead failure. |
| `#error` | `LABEL` | Replace the expectations with the message and continue failing. |
| `#recover` | `RECOVER` | Record the error and go to the skip instructions. |

When no entry is left to pop, the rule body fails.
A cut (`CUT`) marks the topmost choice or repetition entry of the current rule body as cut.

## Position units

Allow the unit of positions (node `Start` and `End`, `startPos` and `endPos`, the position and column of syntax errors, the character count of `len`, and the ranges of `Document.Edit`) to be chosen for each parse ([decision 2](#2-position-unit)).

| Unit | Meaning of a position | Use |
|:--|:--|:--|
| **Code points** (default) | Number of code points from the start | Same as now. For users who handle positions as character counts. |
| **UTF-8 bytes** | Number of bytes from the start | For languages and editors that handle input as bytes (such as UTF-8 positions in LSP). Decoding can be skipped. |

- The meaning of matching does not depend on the unit. Character classes and `.` match one code point (in byte units, they advance by one UTF-8 character).
- An invalid UTF-8 byte sequence is read in byte units as U+FFFD, one byte at a time (as Go's `utf8.DecodeRune` does).
- The position unit can be chosen in **every backend, including the closure backend**, so that backends can be compared with each other in the same unit.
- Revise the description of positions in the specification so that the unit is selectable.

## Instruction set

An instruction consists of a one-byte opcode and variable-length integer operands. Jump targets are offsets within the instruction sequence.
Strings, character classes, rules and so on are referenced by their index in the module's tables.
Many match instructions have a flag that indicates whether to build a value (no value is built inside `@`, `-` or a lookahead).

### Matching

| Instruction | Operands | Behavior |
|:--|:--|:--|
| `STR` | string, expectation, flag | Match a string (value: `Match`) |
| `CLASS` | class, expectation, flag | One character of a character class (including negated classes) |
| `ANY` | flag | Any single character |
| `TOP` | flag | Empty match |
| `FAIL` | | Fail (`_\|_`) |
| `ASSERT` | kind | `^^`, `$$`, `^`, `$` |

### Control

| Instruction | Operands | Behavior |
|:--|:--|:--|
| `JUMP` | target | Unconditional jump |
| `CHOICE` | target | Push a choice entry. On failure, go to the target. |
| `COMMIT` | target | Remove the choice entry and go to the target. |
| `CUT` | | Cut |
| `REPEAT` | target, min, max, element scope | Start a repetition |
| `NEXT` | target | Commit one element (end on an iteration that consumes no input; check the maximum count) |
| `END_REPEAT` | | Check the count and build a `List` |
| `OPTIONAL` / `END_OPTIONAL` | target | `?` (`nil` on failure) |
| `AND` / `NOT` / `END_LOOK` | target | Lookahead |

### Values

| Instruction | Behavior |
|:--|:--|
| `MARK` | Record the value-stack height and the position (start of `Seq` and `@`) |
| `SEQ` | Build a `Seq` from the values since the mark |
| `ATOMIC` | Build a `Match` from the marked position to the current position |
| `DROP` | Discard a value |
| `CAPTURE` | Slot: write the top value to a capture |

### Rules and attributes

| Instruction | Operands | Behavior |
|:--|:--|:--|
| `CALL` | rule, level | Call a rule (the runtime performs memoization and left recursion according to the rule table) |
| `RETURN` | | End of a rule body (the runtime applies the action, the CST rules and the conversion to a terminal type) |
| `PRATT` | Pratt table | Run the Pratt loop (below) |
| `PRED` / `ASSIGN` | expression code (, variable name) | Predicates |
| `LABEL` / `END_LABEL` | message | `#error` |
| `RECOVER` / `END_RECOVER` | skip code | `#recover` |
| `STREAM` | | Commit an element of `#stream` and pass it on |

### Pratt expressions

A Pratt expression is represented by a **table**, not by an instruction sequence. For `skip`, `operand` and each operator, the table holds the start of that part's code, its capture scope, its action code, and its kind, associativity and level.
Longest-match selection and level checks are performed by the runtime's Pratt loop (the same algorithm as `prattParse` in the closure backend), which runs each part's code on the VM.
The algorithm is not broken down into instructions because performing the "try and back up" of longest match in the runtime keeps implementations in other languages simpler and less error-prone.

## Actions and predicates

Action and predicate expressions are compiled into separate **expression code** (stack-based, without backtracking). The runtime does not interpret expression trees.

| Instruction | Behavior |
|:--|:--|
| `PUSH_INT`, `PUSH_STR`, `PUSH_BOOL`, `PUSH_NIL` | Constants |
| `LOAD_CAPTURE` slot, `LOAD_ITEM` n, `LOAD_LOCAL` n, `LOAD_VAR` name | Captures, `$n`, lambda parameters and `$lhs` and similar, variables |
| `MEMBER` name, `NEW` type field-count | Field access, struct construction |
| `BINARY` operator, `UNARY` operator | Operations |
| `JUMP_IF_FALSE`, `JUMP_IF_TRUE`, `JUMP` | Short-circuit evaluation of `&&` and `\|\|` |
| `FUNC` code parameter-count | Lambdas (used only as arguments of built-in functions) |
| `BUILTIN` index argument-count | Built-in functions (`len`, `text`, `foldl`, `foldr`, `map`, `list`, `concat`) |
| `RETURN` | Return the value of the expression |

`foldl` and the other built-ins are implemented as runtime functions and call the code of the lambda they receive on the expression VM.
An evaluation error in an action is an error of the whole parse, and in a predicate it is a failure (as now).

## Module and file format

Raise the `.pegoc` version to 2 and make its content a module. The AST is included by default and can be omitted ([decision 3](#3-ast-in-pegoc)).

| Part | Contents |
|:--|:--|
| Header | Magic, version, instruction-set version |
| String table | All strings |
| Character class table | Lists of ranges, negation |
| Type table | Struct field names (for run-time checks), terminal types |
| Rule table | Name, start of body code, capture scope, action code, terminal type, memoization, left-recursion leader, position dependency, and the shape of the body for `$n` |
| Pratt table | As above |
| Instruction sequences | Match code and expression code |
| AST (optional) | For loading into the closure backend and for `pego convert` (formerly `pego fmt`). Can be omitted. |
| Checksum | CRC-32 |

The runtime reads only the parts other than the AST. If the AST is omitted, the file contains only what the runtime needs.

Version 1 files (AST and analysis results) remain loadable by the closure backend.

## Streams and incremental parsing

- Reading input (`fill`), discarding committed input (`commit`), and recording the examined range (`hw`, `lw`) are shared as input operations used by the match instructions. The specification defines, as "input operations", the properties a runtime must satisfy.
- As in the closure backend, each memo entry records the range it examined. `Document` becomes independent of the backend.

## Impact on code generation

In the future, `pego gen` will also be able to embed "bytecode plus a minimal VM". Because the generated code then follows the runtime specification, generators for other languages can be built just by porting the VM.
Within the scope of this design, the current `pego gen` (which generates Go code) is unchanged.

## Verification

1. **Equivalence tests**: run the engine tests' `check` helper with every backend: closure and bytecode (recursive and iterative), each with and without memoization (six combinations). Test both code-point and byte position units.
2. **Corpus comparison**: for every grammar and input in `examples/` and in the engine tests, check that the results of both backends (JSON including positions, and errors) match (reusing the code-generation test infrastructure).
3. **File format**: round trips of saving and loading, no panics on corrupted data, fuzz tests.
4. **Instruction validation**: on load, check that jump targets, table indices and stack usage are within range (no panics on invalid modules).

## Performance comparison

Compare the backends on several benchmarks and record the results in `docs/benchmarks.md`.
Different grammar features may favor different backends, so the benchmarks are chosen to cover a balanced range of matching types (lexical repetition, Pratt, left recursion, predicates, error recovery) and usage modes (batch, incremental, streaming, loading).

### Backends compared

| Backend | Description |
|:--|:--|
| Go standard library | Only where a comparable standard parser exists (table below) |
| Closure | The current engine |
| Bytecode (recursive and iterative) | The same grammar executed as bytecode |
| Generated Go parser | Output of `pego gen` |

Both code-point and byte position units are measured.

### Benchmarks

| Name | Grammar | Input | Main focus | Standard-library comparison |
|:--|:--|:--|:--|:--|
| JSON | `examples/json` | Typical JSON from a few KB to a few MB | Lexical repetition, AST of union types | `encoding/json` (decoding into `any`) |
| CSV | New example (RFC 4180) | Thousands of rows, with quotes | Simple repetition, many terminals | `encoding/csv` |
| XML | New example (elements, attributes, text, comments) | Hundreds of KB | Nesting, predicates (matching start-tag and end-tag names) | `encoding/xml` (reading tokens) |
| Arithmetic | `examples/calculator` (both Pratt and left recursion) | Long expressions, deep parentheses | Pratt loop, left-recursion growth | `go/parser.ParseExpr` |
| Program | `examples/minilang` | Programs of thousands of lines | Mixed PEG and Pratt, keyword exclusion | None |
| Outline | `examples/outline` | Deep hierarchy | Predicates and variables (non-memoized rules) | None |
| Error recovery | `examples/minilang` | Programs with many errors | `#recover`, expectation recording | None |
| Incremental | `examples/minilang` | Repeated one-character edits to a large program | Memo reuse | None (compared with a fresh parse) |
| Stream | A sequence of records | Tens of MB of input | Reading and discarding, memory bound | None |
| Preparation | Each example's grammar | None | Compiling from source, loading `.pegoc` | None |

Comparisons with the standard library state the differences in output in the table (every PEGO backend builds a tree with positions).
Inputs are generated in the tests from a fixed random seed so that results are reproducible.

## Implementation order

1. Selectable position units (closure backend and specification)
2. Instruction set and module definitions, the compiler (`engine.Program` → module), and a disassembler (for debugging)
3. The recursive VM: matching, control, values, and rule calls (memoization, left recursion)
4. The VM for action and predicate expressions
5. Pratt expressions, attributes, streams and incremental parsing
6. Backend selection in the public API and the CLI, `.pegoc` version 2
7. The iterative VM
8. Extend the equivalence tests to all tests
9. Benchmarks (including the new example grammars), recording the results, and choosing the default backend

At each step, the equivalence tests for the features implemented so far must pass before committing.

## Decisions

### 1. Execution model

- Options: rule calls through host recursion / iteration with an own stack, including calls / recursion first, iteration later.
- **Decision**: implement both and let the user choose, because the appropriate model depends on the use case (ease of porting, deeply nested input, suspend and resume).

### 2. Position unit

- Options: code points / UTF-8 bytes / selectable.
- **Decision**: selectable (code points by default). The unit is selectable in every backend, and results are compared between backends in the same unit.

### 3. AST in .pegoc

- Options: included by default and omittable / always included / never included.
- **Decision**: included by default and omittable.

### 4. Default backend

- Options: decide after the comparison / keep the closure backend / switch to bytecode.
- The comparison uses several benchmarks, including JSON ([performance comparison](#performance-comparison)).
- **Decision**: decide based on the results of the performance comparison. Until then, the closure backend is the default.
- **Outcome** ([benchmarks.md](../benchmarks.md)): within Go, the closure backend was 4–38% faster than bytecode on every workload, so the default is the closure backend when the AST is available and bytecode (the recursive model) otherwise.
