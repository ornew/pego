# PEGO Bytecode Specification

This document specifies the bytecode (the *module*) that a PEGO grammar compiles to, and the behavior of the virtual machine (VM) that executes it.
A runtime for another language must implement this document. The meaning of the language that the bytecode implements is defined by the other chapters of the [specification](README.md); for the results a parse must produce, see [Parsing](parsing.md). For the design background, see [design record 010](../docs/design/010-bytecode-vm.md).

The reference implementation is the Go package `internal/engine` (`bytecode.go`, `bcompile.go` and the VMs in `vm.go` and `ivm.go`). A bytecode VM must produce the same results as the closure backend.

## Terminology

| Term | Meaning |
|:--|:--|
| match code | The instruction sequence that matches input (rule bodies, Pratt parts) |
| expression code | The instruction sequence that evaluates actions and predicates |
| entry stack | The stack of backtrack entries pushed by match instructions ([Failure](#failure)) |
| expectation | A string describing what was expected at a position, used in syntax errors |
| farthest failure | The farthest position at which a failure was recorded, together with the set of expectations recorded at that position |
| level | The Pratt binding level passed to a rule call (`min`); 0 for an ordinary call |
| value-free twin | A copy of a rule compiled so that it builds no value ([Rule table](#rule-table)) |
| transient rule | A rule that is not memoized in normal parses ([Rule calls](#rule-calls)) |

## Values

| Value | Description |
|:--|:--|
| Node | Type name, rule name (optional), range `[start, end)`, terminal text, list of children, and fields (name → value). A node also carries a flag that marks it as a terminal. |
| Integer, string, boolean | Used by actions and predicates |
| nil | The absence of a value |
| Function | A lambda: the start of its expression code, its number of parameters, and the locals captured when it was created |

The reserved node types are `Match` (terminal), `Seq`, `List`, `Operator` and `Error`.
A node is *fresh* only while the body of the rule that created it is executing ([Rule completion](#rule-completion)).

## Input and positions

The input is a sequence of characters. The position unit is chosen for each parse: Unicode code points or UTF-8 bytes.
Reading a character returns the character (a code point) at the current position and its size in position units. Each invalid UTF-8 byte is read as one U+FFFD character whose size is one unit (one byte, or one code point).

## Module

A module consists of the following tables and code sequences.

| Table | Elements |
|:--|:--|
| String table | Strings. All other tables and instructions refer to strings by index. |
| Character class table | A list of ranges (pairs of lower and upper bounds), a negation flag, and an expectation string |
| Type table | Type name, whether the type is a terminal type, and the field names of a struct type |
| Field-list table | Lists of field names used by `ENEW` |
| Scope table | The capture names of a repetition element, in slot order |
| Rule table | See [Rule table](#rule-table) |
| Pratt table | See [Pratt table](#pratt-table) |
| Match code | A sequence of instructions |
| Expression code | A sequence of instructions |

An instruction consists of an opcode and up to three integer operands, `A`, `B` and `C`. A jump target is the index of an instruction in the same code sequence.

### Rule table

| Item | Description |
|:--|:--|
| Name | Rule name |
| Body | Start of the body in the match code. The body pushes one value (or none, if the body is value-free) and ends with `RETURN`. |
| Scope | The capture names of the body, in slot order |
| Action | Start of the action in the expression code, or -1 if there is none |
| Body is a sequence | Used to interpret `$n` |
| Terminal type | Name of the terminal type, or -1 if there is none |
| Memoize, left-recursion leader, position-dependent | Results of static analysis |
| Pratt | Index into the Pratt table, or -1 if there is none |
| Value-free twin | Whether this entry is a value-free twin (see below). The value of a value-free twin is always nil. |
| Transient | The rule is not memoized in normal parses ([Rule calls](#rule-calls)) |
| Variables | Names (string table indices, sorted) of the variables read by predicates in the rule or the rules it calls; memo entries are keyed by their values |

**Value-free twins.** When a rule that returns a CST is called from a place where its value is discarded (`@`, `-`, `!`, or a value-free body), the compiler compiles a second version of its body that builds no value. The twin is placed in the rule table after all original rules, and the `CALL` refers to it. The twin has the same name as the original rule; lookups of a start rule by name ignore twins. Because the twin matches the same input in the same way, results do not change. Rules that belong to a left-recursive cycle have no twin.

**Value-free bodies.** The body of a terminal-type rule, and the body of a rule whose action does not refer to `$n`, are compiled without building values. Capture targets still build their values.

### Pratt table

| Item | Description |
|:--|:--|
| skip | Start of the skip code in the match code, or -1 if there is none. The code builds no value and ends with `END`. |
| operand | A list of lines |
| Prefix operators / infix and postfix operators | Lists of operators (index, kind, associativity, level, line) |

A *line* has the start of its code in the match code (the code pushes one value and ends with `END`), a scope, an action (start in the expression code, or -1), and whether its body is a sequence.

Operator kinds are encoded as 0 = prefix, 1 = postfix, 2 = infix. Associativity is encoded as 0 = left, 1 = right, 2 = none.

## VM state

| State | Description |
|:--|:--|
| `pos` | Current position |
| Value stack | Values built during matching |
| Entry stack | Points to return to on failure (see [Failure](#failure)). Each rule body has its own base. |
| Frame | The capture slots of the current scope |
| Capture trail | Writes to captures (frame, slot, previous value) |
| Environment | Variables, as a persistent list |
| Recovered errors | Syntax errors recovered by `#recover` |
| `silent` | While positive, expectations are not recorded |
| Farthest failure | A position and the set of expectations recorded there |
| Repetition state | For each active repetition: count, value-stack base, maximum, and scope |
| Memo | (rule, position, level) → result |

**Save and restore.** Saving the state records `pos`, the height of the value stack, the height of the entry stack, the number of repetition states, the length of the capture trail, the environment, the number of recovered errors, and the frame. Restoring returns each of these to the recorded value.
Unwinding the capture trail writes each recorded previous value back into its slot.

## Failure

When an instruction fails, the VM pops entries from the entry stack of the current rule body and handles each according to its kind. If there are no entries left, the rule body fails.

| Entry | Pushed by | When popped on failure |
|:--|:--|:--|
| Choice | `CHOICE` | Restore. If the entry is marked as cut, continue failing; otherwise jump to the recorded target. |
| Iteration | `ITER` | Restore. If the entry is marked as cut, continue failing; otherwise jump to the recorded target. |
| Positive lookahead | `LOOK` (neg = 0) | Restore `silent`, restore the state, and continue failing. |
| Negative lookahead | `LOOK` (neg = 1) | Restore `silent`, restore the state, and jump to the recorded target (the negative lookahead succeeds). |
| Label | `LABEL` | Restore the saved farthest failure, record the message as an expectation at the farthest position reached inside the label, and continue failing. |
| Recovery | `RECOVER` | See [Recovery](#recovery). |
| Skip | Recovery handling | Restore, add the expectations recorded inside to the farthest failure, and continue failing. |

## Match instructions

`b` denotes an operand that selects whether the instruction pushes a value (1) or not (0). Matching behaves the same either way.

| Code | Instruction | Operands | Behavior |
|--:|:--|:--|:--|
| 1 | `STR` | A string, B expectation, C b | If the characters of string A follow the current position, advance past them and, if b, push a `Match`. Otherwise record B as an expectation at the start position and fail. |
| 2 | `CLASS` | A class, B expectation, C b | If the current character is in the class (not in it, for a negated class), advance one character and, if b, push a `Match`. Otherwise record B at the current position and fail. |
| 3 | `ANY` | A b | If there is a character, advance one character (and, if b, push a `Match`). Otherwise record `any character` and fail. |
| 33 | `SCAN` | A class, B min, C max | A value-free repetition of one character. Advance over characters that are in class A (any character if A is -1) until a character does not match or C characters have been read (no limit if C is -1). If it stopped at a non-matching character (or at the end of input), record the class's expectation (`any character` if A is -1) at that position. Fail if fewer than B characters were read. |
| 34 | `GUARD` | A class, B depth, C target | First-character dispatch, emitted before the `CHOICE` of an alternative that must begin with a given terminal. If the next character is not in class A (or there is none) and the current call depth plus B does not exceed the nesting limit, record the class's expectation at the current position and go to C (the next alternative); otherwise continue. For a literal, A is a class of its first character whose expectation is the literal's. The alternative would have failed there recording just that expectation, so skipping it changes nothing but speed. |
| 4 | `TOP` | A b | If b, push an empty `Match`. |
| 5 | `FAIL` | | Fail. |
| 6 | `ASSERT` | A kind | 0: the position is 0. 1: at the end of input. 2: the position is 0 or the preceding character is `\n`. 3: at the end of input, or the current character is `\n` or `\r`. If the condition does not hold, record `beginning of input`, `end of input`, `beginning of line` or `end of line` respectively, and fail. |
| 7 | `JUMP` | A target | Jump to A. |
| 8 | `CHOICE` | A target | Save the state and push a choice entry with target A. |
| 9 | `COMMIT` | A target | Remove the top entry (a choice) and jump to A. |
| 10 | `CUT` | | Scan the entry stack from the top and mark the first choice or iteration entry as cut. If a lookahead entry or the base of the rule body is reached first, do nothing. Label and recovery entries are skipped over. |
| 11 | `PUSHPOS` | | Push the current position as a value. |
| 12 | `PUSHNIL` | | Push nil. |
| 13 | `SEQ` | A n | Pop n values and the position below them, and push a `Seq` whose children are those n values, whose range runs from that position to the current position, and which is fresh. |
| 14 | `ATOMIC` | A b | Pop a position and, if b, push a `Match` of the text from that position to the current position. |
| 15 | `CAPTURE` | A slot, B pop | Write the top value into slot A of the current frame and record the write in the capture trail. If B is 1, also pop the value (a capture in a value-free context). |
| 16 | `REPEAT` | A min, B max, C scope | Push a repetition state (count 0, value-stack base at the current height). B = -1 means no maximum. C is a scope-table index, or -1 if elements have no scope. When the repetition builds a value, a `PUSHPOS` immediately before it pushes the start position. |
| 17 | `ITER` | A exit | If the repetition has a finite maximum and its count has reached it, jump to A without executing the element. Otherwise save the state and push an iteration entry whose target is A (the exit of the loop). If the repetition has a scope, switch to a new frame. |
| 18 | `NEXT` | A loop, B mode, C slot | One iteration succeeded. Remove the iteration entry and restore the frame. If the repetition has a scope and the mode is 1 or 2, attach the captures to the element value (with mode 0 the scope exists only for predicates inside the element). Increment the count. Mode 0 keeps no value (a value-free repetition), 1 keeps the value, 2 passes the value to the stream consumer, and 3 (instruction set 3) pushes the value of slot C of the element's frame as the element value: a projected repetition, whose elements are matched without values (`map($x, (e) => $e.f)` reads only that field; see `internal/engine/project.go`). Then, if the iteration consumed no input and the count is at least the minimum, or the count has reached the maximum, jump to the exit recorded by `ITER`; otherwise jump to A. |
| 19 | `ENDREPEAT` | A b | Pop the repetition state. Fail if the count is below the minimum. If b, pop the values above the value-stack base and the start position below them, and push a `List` (fresh). |
| 20 | `LOOK` | A target, B neg | Save the state, push a lookahead entry and increment `silent`. A is used only by a negative lookahead (B = 1). |
| 21 | `ENDLOOK` | A neg | The inner expression succeeded. Positive: remove the entry, restore `silent`, restore only `pos` and the value-stack height, and continue. Negative: restore `silent`, restore the state, and fail. |
| 22 | `CALL` | A rule, B level, C keep | Call rule A at level B ([Rule calls](#rule-calls)). On success, push the value if keep is 1. |
| 23 | `PRATT` | A table | Parse the [Pratt expression](#pratt-expressions) A at level `min` and push its value. `min` is the level of the `CALL` that invoked this body. |
| 24 | `PRED` | A expression | Evaluate expression code A. Fail if evaluation produces an error or `false`. |
| 25 | `ASSIGN` | A name, B expression | Evaluate expression code B. Fail on error; otherwise add variable A (a string index) to the environment. |
| 26 | `LABEL` | A message | Save the farthest failure, reset it to (current position, empty), and push a label entry. |
| 27 | `ENDLABEL` | | Remove the entry, add the expectations recorded inside to the saved farthest failure, and make that the farthest failure again. |
| 28 | `RECOVER` | A target | Save the state, save the farthest failure and reset it to (current position, empty), and push a recovery entry with target A (the skip code). |
| 29 | `ENDRECOVER` | A target | The inner expression succeeded. Remove the entry, merge the recorded expectations back as for `ENDLABEL`, and jump to A. |
| 30 | `ENDSKIP` | A b | The skip succeeded. Fail if it consumed no input. Otherwise add the error to the recovered errors and, if b, push an `Error` node. |
| 31 | `RETURN` | | The rule body succeeded. |
| 32 | `END` | | A Pratt part succeeded. |

**Streaming.** `NEXT` with mode 2 is emitted only for the repetition marked `#stream` in the start rule. When the VM is parsing a stream and this repetition runs in the start rule's body (call depth 1), the element value is not kept: it is passed to the consumer (no longer fresh), the input up to the current position is committed (earlier input and memo entries are discarded; see [design record 007](../docs/design/007-streaming-and-incremental-parsing.md)), and the loop exits if the iteration consumed no input and otherwise jumps to A. On reentry, `ITER` enforces the finite maximum before executing another element; a zero maximum executes no elements. In any other parse, mode 2 behaves like mode 1.

### Recovery

When a recovery entry is popped on failure, the VM:

1. Takes the farthest failure recorded inside (a position and its expectations) and restores the saved farthest failure.
2. Restores the state.
3. Pushes a skip entry (holding a saved state for restoring and the failure from step 1) and jumps to the recorded target (the skip code).

`ENDSKIP` builds a syntax error from the failure held by the skip entry. The `Error` node spans from the start of the recovery to the current position; its text is the text of that range and its `message` field is the string form of the syntax error.

## Rule calls

`CALL r, min` calls rule r as follows. The call is memoized if the rule is a leader, or if it is marked for memoization and is not transient. In an incremental parse, a transient rule is memoized when it is marked for memoization, so that results from the previous parse can be reused.

A rule is marked transient when it calls no other rule, or when it is referenced only once in the grammar. Re-evaluating the former only scans the input once more; the latter is re-evaluated at the same position only when its single caller is. In both cases a memo entry would never be reused.

1. If the memo holds a result for (r, pos, min) and, when the rule reads variables, for the current values of those variables (the rule table's variable list; an undefined variable counts as `nil`), and the result was not computed inside a lookahead, or the VM is currently inside a lookahead, or the result is a provisional result of a growing left recursion, use it: record the expectations stored with the result (unless it is growing) and its recovered errors; on success, advance the position and return the value.
2. If the call is memoized, save the farthest failure and reset it to (start position, empty).
3. If the rule is a leader, evaluate it by [growing the seed](#left-recursion); otherwise [evaluate the body](#body-evaluation) once. On failure, remove the errors recovered during this call.
4. If the call is memoized, store in the memo the result, the end position, the recovered errors, whether the call ran inside a lookahead, and the expectations recorded inside; then merge those expectations into the saved farthest failure and restore it.
5. On failure, reset the position to the start position.

### Body evaluation

To evaluate a body, the VM creates a new frame (sized for the rule's scope), saves the frame, the environment and the length of the capture trail, and runs the body's code.
While the body runs, it uses its own base of the entry stack and of the value stack.
When the body finishes, the VM restores the frame and the capture trail; on success, it performs [rule completion](#rule-completion); finally it restores the environment, so variables defined in the body are not visible to the caller.

### Left recursion

A leader rule is evaluated as follows:

1. Store a failure for (r, pos, min) in the memo, marked as growing.
2. Reset the position to the start position, reset the recovered errors to their state before the call, and evaluate the body.
3. If the body fails, or does not advance beyond the previous success, stop. Otherwise store the result in the memo, marked as growing, and go back to step 2.
4. Finalize the last result (clear the growing mark) and return it.

### Rule completion

The value of the rule is built from the body value v and the start position:

- **With an action:** evaluate the action with the `$n` items set to the children of v if the body is a sequence, and to [v] otherwise. If the result is a node newly created by the action, its range becomes the range of the rule. The result must be a node or nil.
- **With a terminal type:** create a terminal of that type whose text is the text of the rule's range, and give it the rule name.
- **Otherwise:** if the scope has captures, attach them. If v is fresh, give it the rule name if it has none, and clear its fresh mark.

**Attaching captures:** if no capture is non-nil, do nothing. If v is nil, is not fresh, or is itself one of the captures, wrap v in a `Seq` whose only child is v, whose range runs from the start position to the current position, and which is fresh. Then add each non-nil capture as a field under its name.

## Pratt expressions

The algorithm is the same as `prattParse` in the closure backend; see the parsing rules in [Pratt Expressions](pratt.md#parsing-rules). Each part's code runs with its own entry-stack base and frame.

- **Skip:** increment `silent`, run the skip code, and on failure reset the position.
- **Longest match:** for each candidate operator, save the state, run the line's code in a new frame, and on success record the end position, the value, the frame, the environment, whether a cut was passed, and the recovered errors; then restore the state (writing the recorded captures back into the frame). A prefix or postfix candidate that succeeds without consuming input is treated as not matching. Select the candidate that advanced farthest; on a tie, select the one declared first. Also record whether some candidate failed after passing a cut.
- **Head:** after skip, try the longest match among the prefix operators. If one matches, apply it and parse the right operand at the operator's level. If the right operand fails, fail if a cut was passed; otherwise backtrack and try the operands. Operands are tried in declaration order; if an operand fails after passing a cut, the whole expression fails.
- **Tail:** after skip, try the longest match among the infix and postfix operators. If there is no candidate, stop (or fail, if a candidate failed after a cut). If the operator's level is less than or equal to `min`, or the operator is non-associative and a non-associative operator of the same level was just applied, backtrack and stop. A postfix operator is applied immediately. For an infix operator, parse the right operand at the operator's level (level - 1 for a right-associative operator); if that fails, fail if a cut was passed, and otherwise backtrack and stop.
- **Values:** if the operator has an action, evaluate it with local 0 = `$lhs`, 1 = `$rhs` and 2 = `$op` (a `Match` of the operator part's text). Otherwise build an `Operator` node whose children are `[op, rhs]`, `[lhs, op]` or `[lhs, op, rhs]` according to the operator kind, whose `operator` field is the operator's index, and which carries the rule name. The value of an operand is its action's result if it has an action; otherwise its captures are attached and, if it is fresh, it is given the rule name.

## Execution models

A runtime implements one or both of two execution models, which differ in how rule calls and Pratt expressions are executed. Both must produce the same results.

- **Recursive model:** `CALL` and `PRATT` invoke the procedures in [Rule calls](#rule-calls) and [Pratt expressions](#pratt-expressions) as host-language functions. The maximum nesting depth is bounded by the host stack.
- **Iterative model:** execution suspends at `CALL` and `PRATT` and resumes when the result is available (after a `CALL`, on success it pushes the value if keep is 1 and continues with the next instruction; on failure it proceeds as in [Failure](#failure)). Rule calls, left-recursion growth, the Pratt loop, longest match and skip are implemented as state machines whose state is kept in stack entries, and they perform the steps above in the same order. Host stack usage is constant, independent of how deeply the input nests.

In the reference implementation, both models share the instruction interpreter (`step`) and the stages of a rule call (`callBegin`, `growBegin`, `growStep`, `growEnd`, `invokeBegin`, `invokeEnd`, `callEnd`); they differ only in the order in which these are invoked (`vm.go`, `ivm.go`).
The reference implementation also limits the nesting depth of rule calls (100,000 by default, 10,000,000 for the iterative model; configurable with `pego.WithMaxDepth`) and reports a parse error when the limit is exceeded.
Actions and predicates may be evaluated with host function calls in either model, because their nesting depth is fixed by the grammar and does not depend on the input.

## Expression code

Actions and predicates are evaluated by stack-based expression code. An evaluation error in an action is an error of the whole parse (`action in <rule name>: <message>`); in a predicate it is a failure.

| Code | Instruction | Operands | Behavior |
|--:|:--|:--|:--|
| 100 | `EINT` | A, B value | Push the integer A + B × 2³² (A and B signed, with 64-bit wrap-around): A holds its low 32 bits, and B is 0 for the integers of 32 bits. |
| 101 | `ESTR` | A string | Push a string. |
| 102 | `EBOOL` | A value | Push a boolean (0 or 1). |
| 103 | `ENIL` | | Push nil. |
| 104 | `ECAP` | A slot | Push the value of a capture slot. |
| 105 | `EITEM` | A n | Push `$n`. For n = 0, push a `List` of all items. An out-of-range n is an error. |
| 106 | `ELOCAL` | A index | Push a local (a lambda parameter, or `$lhs`, `$rhs`, `$op`). |
| 107 | `EVAR` | A name | Push the value of a variable. An undefined variable is an error. |
| 108 | `EMEMBER` | A name | Pop a value and push its field. `startPos`, `endPos` and `children` are built in. A field not declared by the struct type is an error; a declared field that is absent yields nil. |
| 109 | `ENEW` | A type, B field list | Pop as many values as the field list has names, and push a struct node. |
| 110 | `EBIN` | A operator | Pop two values and push the result of the operation. |
| 111 | `EUNARY` | A operator | Pop a value and push the result of the operation. |
| 112, 113 | `EAND` / `EOR` | A target | It is an error if the top value is not a boolean. `EAND` on false, and `EOR` on true, leave the value and jump to A. Otherwise pop the value and continue. |
| 114 | `EBOOLCHK` | A operator | It is an error if the top value is not a boolean. |
| 115 | `EFUNC` | A start, B parameter count | Push a function that captures the current locals. |
| 116 | `ECALL` | A built-in, B argument count | Pop the arguments, call the built-in function, and push the result. |
| 117 | `ERET` | | Return the top value as the value of the expression. |
| 118 | `ETEXTCHK` | | It is an error (the same as `text`'s) if `text` of the top value is not defined. The value stays. *(Instruction set 2.)* |
| 119 | `ETEXTEQ` | A neg | Pop two values and push whether their texts (as `text` gives them; both were checked with `ETEXTCHK`) are equal, or unequal if neg is 1. *(Instruction set 2.)* |
| 120 | `ELISTBEGIN` | | Start gathering the elements of a list. Gatherings nest. *(Instruction set 2.)* |
| 121 | `ELISTPUSH` | A n | Pop n values and add them, in order, to the list being gathered, with the checks of `list`. *(Instruction set 2.)* |
| 122 | `EMAPPUSH` | | Pop a function and a list (the list below) and add the results of calling the function on each element, with the checks of `map`. *(Instruction set 2.)* |
| 123 | `ELISTEND` | | Finish the innermost gathering and push its elements as a `List`, as `concat` builds it. *(Instruction set 2.)* |

Names (`EVAR`, `EMEMBER`), operators (`EBIN`, `EUNARY`, `EBOOLCHK`, written as their source symbol such as `+` or `&&`) and type names (`ENEW`) are string-table indices.

Calling a function evaluates its code from its start, with the locals captured at creation followed by the arguments as its locals.

Instructions 118–123 are shortcuts with the same results as the general instructions. The reference compiler emits `ETEXTCHK` after each operand and `ETEXTEQ` for `text(a) == text(b)` and `text(a) != text(b)`, which compares the texts without making them values, and `ELISTBEGIN`, `ELISTPUSH`/`EMAPPUSH` per argument and `ELISTEND` for a `concat` call whose arguments are all `list`, `map` or such `concat` calls, which builds no intermediate list. In both cases the errors arise in the same order as with `ECALL`.

Built-in function indices: 0 `len`, 1 `text`, 2 `foldl`, 3 `foldr`, 4 `map`, 5 `list`, 6 `concat`. Their semantics are defined in [Actions](actions.md#built-in-functions).

## Error messages

A syntax error (see [Syntax errors](parsing.md#syntax-errors)) is formatted as `line:column: syntax error: expected A, B` (expectations sorted lexicographically). If `#error` messages apply, it is formatted as `line:column: message` (multiple messages joined with `; `).
Expectation strings are those the compiler placed in the string table: literals in quotes, character classes in `(?...)` form, `any character`, and so on.

## File format

A module is stored in the `.pegoc` version 2 format. Integers are variable-length: unsigned integers use LEB128 and signed integers use zigzag-encoded LEB128. A boolean is one byte (0 or 1).

| Part | Contents |
|:--|:--|
| Magic | `PEGOC\x00` (6 bytes) |
| Version | 1 byte (2) |
| Instruction-set version | Unsigned integer (3). A runtime loads files of every instruction-set version up to its own: version 2 only adds instructions (118–123), and version 3 `GUARD` (34) and `NEXT` mode 3. |
| String table | Count, then each string (byte length and UTF-8 bytes), all unsigned. The module's string table is a prefix of this table. |
| Start rule | String index, unsigned (the empty string means no default start rule) |
| Package name | String index, unsigned |
| Module | See below |
| AST present | Boolean. If true, the grammar AST and the static-analysis results follow (including, per rule, whether it may have a value-free twin). These serve the closure backend and the formatter of the reference implementation; a runtime may skip them. |
| Checksum | CRC-32 (IEEE) of everything from the magic up to this point, 4 bytes little-endian |

The module is laid out in the following order. A *list* is a count (unsigned) followed by its elements. Indices, positions and other integers inside the module are signed unless stated otherwise.

1. The number of module strings, unsigned (this many strings from the start of the string table form the module's string table).
2. Character class table: a list of (negated, expectation string, number of ranges (unsigned), followed by the lower and upper bound of each range).
3. Type table: a list of (name, terminal, list of field names).
4. Field-list table, then scope table: each a list of lists of string indices.
5. Rule table: a list of (name, body, scope, action, body is a sequence, terminal type, memoize, leader, position-dependent, Pratt, value-free twin, transient, variables).
6. Pratt table: a list of (skip, list of operand lines, list of prefix operators, list of infix and postfix operators). A line is (start, scope, action, body is a sequence); an operator is (index, kind, associativity, level, line).
7. Match code, then expression code: each a list of instructions (opcode as 1 byte, then `A`, `B`, `C`).

### Load-time validation

On load, a runtime checks the following and rejects the file if any check fails:

- The checksum, the version and the instruction-set version are valid, and no data remains after the end.
- Every reference in the tables and instructions (strings, character classes, scopes, field lists, rules, Pratt tables, expression code, built-in functions) is in range.
- Every opcode is known, and every flag operand is within its range.
- Every jump target is forward. Only `NEXT` jumps backward. The target of `EFUNC` is also forward.

A runtime does not verify that instruction sequences use the value stack and the entry stack correctly. Runtime errors caused by invalid code are reported as parse errors, but termination is not guaranteed, so load files only from trusted sources.
