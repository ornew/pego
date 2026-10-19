# PEGO Bytecode Specification

This document specifies the bytecode (the *module*) that a PEGO grammar compiles to, and the behavior of the virtual machine (VM) that executes it.
A runtime for another language must implement this document. The meaning of the language that the bytecode implements is defined by the other chapters of the [specification](README.md); for the results a parse must produce, see [Parsing](parsing.md).

The reference implementation is the Go package `internal/engine` (`bytecode.go`, `bcompile.go` and the VMs in `vm.go` and `ivm.go`). A bytecode VM must produce the same results as the closure backend.

## Terminology

| Term | Meaning |
|:--|:--|
| match code | The instruction sequence that matches input (rule bodies, Operators Rule parts) |
| expression code | The instruction sequence that evaluates Value Actions and Predicate Actions |
| entry stack | The stack of backtrack entries pushed by match instructions ([Failure](#failure)) |
| expectation | A string describing what was expected at a position, used in syntax errors |
| farthest failure | The farthest position at which a failure was recorded, together with the set of expectations recorded at that position |
| level | The Operators Rule binding level passed to a rule call (`min`); 0 for an ordinary call |
| value-free twin | A copy of a rule compiled so that it builds no value ([Rule table](#rule-table)) |
| transient rule | A rule that is not memoized in normal parses ([Rule calls](#rule-calls)) |

## Values

| Value | Description |
|:--|:--|
| Node | Type name, rule name (optional), range `[start, end)`, text, list of children, and fields (name → value). A node also carries a flag that marks it as holding text: a string node or an error node. |
| Integer, string, boolean | Used by Value Actions and Predicate Actions |
| nil | The absence of a value |
| Function | A lambda: the start of its expression code, its number of parameters, and the locals captured when it was created |

The reserved bytecode node types are the [kinds](types.md#node-type-names) `string`, `tuple`, `list` and `error`.
The Closure-only `#missing` pragma and `Missing` type have no bytecode encoding;
VM parsing, binary serialization and module construction reject those grammars.
A node is *fresh* only while the body of the rule that created it is executing ([Rule completion](#rule-completion)).

## Input and Positions

The input is a sequence of characters. The position unit is chosen for each parse: Unicode code points or UTF-8 bytes.
Reading a character returns the character (a code point) at the current position and its size in position units. Each invalid UTF-8 byte is read as one U+FFFD character whose size is one unit (one byte, or one code point).

## Module

A module consists of the following tables and code sequences.

| Table | Elements |
|:--|:--|
| String table | Strings. All other tables and instructions refer to strings by index. |
| Character class table | A list of ranges (pairs of lower and upper bounds), a negation flag, and an expectation string |
| Type table | Type name and the field names of a struct type |
| Field-list table | Lists of field names used by `ENEW` |
| Scope table | The capture names of a repetition element, in slot order |
| Rule table | See [Rule table](#rule-table) |
| Operators Rule table | See [Operators Rule table](#operators-rule-table) |
| Match code | A sequence of instructions |
| Expression code | A sequence of instructions |

An instruction consists of an opcode and up to three integer operands, `A`, `B` and `C`. A jump target is the index of an instruction in the same code sequence.

Operands are signed 32-bit integers. Repetition bounds that exceed this range
use `REPEATW` or `SCANW`, whose bound operands index `EINT` constants in the
expression code. These constants preserve the full implementation-int range;
they are read directly, without evaluating an action. Ordinary instructions
retain their compact representation.

The public grammar AST treats every negative repetition maximum as unbounded.
Compilation canonicalizes it to -1 before narrowing operands or creating wide
bound constants. The AST's original field is preserved; serialized bytecode
uses the existing -1 sentinel for both `REPEAT` and `SCAN` variants.

### Rule Table

| Item | Description |
|:--|:--|
| Name | Rule name |
| Body | Start of the body in the match code. The body pushes the value of its scope (or none, if the body is value-free) and ends with `RETURN`: the single value of the body, or a tuple node of its values when the scope is a tuple. |
| Scope | The capture names of the body, in slot order |
| Action | Start of the action in the expression code, or -1 if there is none |
| Body is a sequence | Whether the body's value is a tuple; used to interpret `$n` |
| Text type | Name of the struct type of a text struct rule (see Rule Completion), or -1 if the rule is not one |
| Memoize, left-recursion leader, position-dependent | Results of static analysis |
| Operators Rule | Index into the Operators Rule table, or -1 if there is none |
| Value-free twin | Whether this entry is a value-free twin (see below). The value of a value-free twin is always nil. |
| Transient | The rule is not memoized in normal parses ([Rule calls](#rule-calls)) |
| Variables | Names (string table indices, sorted) of the variables read by predicates in the rule or the rules it calls; memo entries are keyed by their values |
| Check | The type the rule's value is checked against when the rule produces it, as a string-table index of the type text (written like the type of `ECHECK`), or -1 if the value is not checked (see [Types and Errors](actions.md#types-and-errors)). A mismatch fails the parse with `cannot use <value type> as <type> in result of rule <name>`. |

**Value-free twins.** When a rule that returns a CST is called from a place where its value is discarded (`#atomic`, `-`, `!`, or a value-free body), the compiler compiles a second version of its body that builds no value. The twin is placed in the rule table after all original rules, and the `CALL` refers to it. The twin has the same name as the original rule; lookups of a start rule by name ignore twins. Because the twin matches the same input in the same way, results do not change. Rules that belong to a left-recursive cycle have no twin.

**Value-free bodies.** The body of a text struct rule, and the body of a rule whose action does not refer to `$n`, are compiled without building values. Capture targets still build their values.

### Operators Rule Table

| Item | Description |
|:--|:--|
| skip | Start of the skip code in the match code, or -1 if there is none. The code builds no value and ends with `END`. |
| operand | A list of lines |
| Prefix operators / infix and postfix operators | Lists of operators (index, kind, associativity, level, line) |

A *line* has the start of its code in the match code (the code pushes one value and ends with `END`), a scope, an action (start in the expression code, or -1), whether its body is a sequence, and the type its value is checked against when the line produces it (as the rule table's check, with the rule's name in the error), or -1. An operand line pushes the value of its scope, as a rule body does. An operator line pushes the operator's value: when its body is a sequence, that value is the tuple node of the operator's values, and the line's captures are attached to it.

Operator kinds are encoded as 0 = prefix, 1 = postfix, 2 = infix. Associativity is encoded as 0 = left, 1 = right, 2 = none.

## VM State

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

Value-free recognition uses `NEXT` mode 0 even for a source repetition marked
`#stream`. Mode 2 requires an element value on the stack; it belongs to a
value-building stream-capable program. Element capture frames remain available
to predicates in mode 0, but are not attached to an element value.

## Failure

When an instruction fails, the VM pops entries from the entry stack of the current rule body and handles each according to its kind. If there are no entries left, the rule body fails.

| Entry | Pushed by | When popped on failure |
|:--|:--|:--|
| Choice | `CHOICE` | Restore. If the entry is marked as cut, continue failing; otherwise jump to the recorded target. |
| Iteration | `ITER` | Restore. If the entry is marked as cut, continue failing; otherwise jump to the recorded target. |
| Positive lookahead | `LOOK` (neg = 0) | Restore `silent`, restore the state, and continue failing. |
| Negative lookahead | `LOOK` (neg = 1) | Restore `silent`, restore the state, and jump to the recorded target (the negative lookahead succeeds). |
| Label | `LABEL` | Restore the saved farthest failure. For mode B=0, report A as an explicit error message; for B=1, use A as the diagnostic label for ordinary expectations recorded at the wrapped expression's farthest position. Preserve explicit messages and continue failing. |
| Recovery | `RECOVER` | See [Recovery](#recovery). |
| Skip | Recovery handling | Restore, add the expectations recorded inside to the farthest failure, and continue failing. |

## Match Instructions

`b` denotes an operand that selects whether the instruction pushes a value (1) or not (0). Matching behaves the same either way.

| Code | Instruction | Operands | Behavior |
|--:|:--|:--|:--|
| 1 | `STR` | A string, B expectation, C b | If the characters of string A follow the current position, advance past them and, if b, push a string node. Otherwise record B as an expectation at the start position and fail. |
| 2 | `CLASS` | A class, B expectation, C b | If the current character is in the class (not in it, for a negated class), advance one character and, if b, push a string node. Otherwise record B at the current position and fail. |
| 3 | `ANY` | A b | If there is a character, advance one character (and, if b, push a string node). Otherwise record `any character` and fail. |
| 33 | `SCAN` | A class, B min, C max | A value-free repetition of one character. Advance over characters that are in class A (any character if A is -1) until a character does not match or C characters have been read (no limit if C is -1). If it stopped at a non-matching character (or at the end of input), record the class's expectation (`any character` if A is -1) at that position. Fail if fewer than B characters were read. |
| 34 | `GUARD` | A class, B depth, C target | First-character dispatch, emitted before the `CHOICE` of an alternative that must begin with a given terminal. If the next character is not in class A (or there is none) and the current call depth plus B does not exceed the nesting limit, record the class's expectation at the current position and go to C (the next alternative); otherwise continue. For a literal, A is a class of its first character whose expectation is the literal's. The alternative would have failed there recording just that expectation, so skipping it changes nothing but speed. |
| 4 | `TOP` | A b | If b, push an empty string node. |
| 5 | `FAIL` | | Fail. |
| 6 | `ASSERT` | A kind | 0: the position is 0. 1: at the end of input. 2: the position is 0 or the preceding character is `\n`. 3: at the end of input, or the current character is `\n` or `\r`. If the condition does not hold, record `beginning of input`, `end of input`, `beginning of line` or `end of line` respectively, and fail. |
| 7 | `JUMP` | A target | Jump to A. |
| 8 | `CHOICE` | A target | Save the state and push a choice entry with target A. |
| 9 | `COMMIT` | A target | Remove the top entry (a choice) and jump to A. |
| 10 | `CUT` | | Scan the entry stack from the top and mark the first choice or iteration entry as cut. If a lookahead entry or the base of the rule body is reached first, do nothing. Label and recovery entries are skipped over. |
| 11 | `PUSHPOS` | | Push the current position as a value. |
| 12 | `PUSHNIL` | | Push nil. |
| 13 | `SEQ` | A n | Pop n values and the position below them, and push a tuple node whose children are those n values, whose range runs from that position to the current position, and which is fresh. |
| 14 | `ATOMIC` | A b | Pop a position and, if b, push a string node of the text from that position to the current position. |
| 15 | `CAPTURE` | A slot, B pop | Write the top value into slot A of the current frame and record the write in the capture trail. If B is 1, also pop the value (a capture in a value-free context). |
| 16 | `REPEAT` | A min, B max, C scope | Push a repetition state (count 0, value-stack base at the current height). B = -1 means no maximum. C is a scope-table index, or -1 if elements have no scope. When the repetition builds a value, a `PUSHPOS` immediately before it pushes the start position. |
| 35 | `REPEATW` | A min constant, B max constant, C scope | Like `REPEAT`, with the bounds read from the `EINT` instructions at expression-code indices A and B. The scope, iteration, streaming and resumption semantics are unchanged. |
| 36 | `SCANW` | A class, B min constant, C max constant | Like `SCAN`, with the bounds read from the `EINT` instructions at expression-code indices B and C. |
| 17 | `ITER` | A exit | If the repetition has a finite maximum and its count has reached it, jump to A without executing the element. Otherwise save the state and push an iteration entry whose target is A (the exit of the loop). If the repetition has a scope, switch to a new frame. |
| 18 | `NEXT` | A loop, B mode, C slot | One iteration succeeded. Remove the iteration entry and restore the frame. If the repetition has a scope and the mode is 1 or 2, attach the captures to the element value (with mode 0 the scope exists only for predicates inside the element). Increment the count. Mode 0 keeps no value (a value-free repetition), 1 keeps the value, 2 passes the value to the stream consumer, and 3 pushes the value of slot C of the element's frame as the element value: a projected repetition, whose elements are matched without values (`map($x, (e) => e.$f)` reads only that capture; see `internal/engine/project.go`). Then, if the iteration consumed no input, or the count has reached the maximum, jump to the exit recorded by `ITER`; otherwise jump to A. |
| 19 | `ENDREPEAT` | A b | Pop the repetition state. Fail if the count is below the minimum. If b, pop the values above the value-stack base and the start position below them, and push a list node (fresh). |
| 20 | `LOOK` | A target, B neg | Save the state, push a lookahead entry and increment `silent`. A is used only by a negative lookahead (B = 1). |
| 21 | `ENDLOOK` | A neg | The inner expression succeeded. Positive: remove the entry, restore `silent`, restore only `pos` and the value-stack height, and continue. Negative: restore `silent`, restore the state, and fail. |
| 22 | `CALL` | A rule, B level, C keep | Call rule A at level B ([Rule calls](#rule-calls)). On success, push the value if keep is 1. |
| 23 | `OPERATORS_RULE` | A table | Parse Operators Rule table A at level `min` and push its value. `min` is the level of the `CALL` that invoked this body. |
| 24 | `PRED` | A expression | Evaluate expression code A. Fail if evaluation produces an error or `false`. |
| 25 | `ASSIGN` | A name, B expression | Evaluate expression code B. Fail on error; otherwise add variable A (a string index) to the environment. |
| 26 | `LABEL` | A message, B mode | Save the farthest failure, reset it to (current position, empty), and push a label entry. B=0 applies `@error`; B=1 applies `@diagnostic` and renames ordinary expected items from the wrapped failure. |
| 27 | `ENDLABEL` | | Remove the entry, add the expectations recorded inside to the saved farthest failure, and make that the farthest failure again. |
| 28 | `RECOVER` | A target | Save the state, save the farthest failure and reset it to (current position, empty), and push a recovery entry with target A (the skip code). |
| 29 | `ENDRECOVER` | A target | The inner expression succeeded. Remove the entry, merge the recorded expectations back as for `ENDLABEL`, and jump to A. |
| 30 | `ENDSKIP` | A b | The skip succeeded. Fail if it consumed no input. Otherwise add the error to the recovered errors and, if b, push an error node. |
| 31 | `RETURN` | | The rule body succeeded. |
| 32 | `END` | | An Operators Rule part succeeded. |

**Streaming.** `NEXT` with mode 2 is emitted only for the repetition marked `#stream` in the start rule. When the VM is parsing a stream and this repetition runs in the start rule's body (call depth 1), the element value is not kept: it is passed to the consumer (no longer fresh), the input up to the current position is committed (earlier input and memo entries are discarded), and the loop exits if the iteration consumed no input and otherwise jumps to A. On reentry, `ITER` enforces the finite maximum before executing another element; a zero maximum executes no elements. In any other parse, mode 2 behaves like mode 1.

### Recovery

When a recovery entry is popped on failure, the VM:

1. Takes the farthest failure recorded inside (a position and its expectations) and restores the saved farthest failure.
2. Restores the state.
3. Pushes a skip entry (holding a saved state for restoring and the failure from step 1) and jumps to the recorded target (the skip code).

`ENDSKIP` builds a syntax error from the failure held by the skip entry. The error node spans from the start of the recovery to the current position; its text is the text of that range and its `message` field is the string form of the syntax error.

## Rule Calls

`CALL r, min` calls rule r as follows. The call is memoized if the rule is a leader, or if it is marked for memoization and is not transient. In an incremental parse, a transient rule is memoized when it is marked for memoization, so that results from the previous parse can be reused.

A rule is marked transient when it calls no other rule, or when it is referenced only once in the grammar. Re-evaluating the former only scans the input once more; the latter is re-evaluated at the same position only when its single caller is. In both cases a memo entry would never be reused.

1. If the memo holds a result for (r, pos, min) and, when the rule reads variables, for the current values of those variables (the rule table's variable list; an undefined variable counts as `nil`), and the result was not computed inside a lookahead, or the VM is currently inside a lookahead, or the result is a provisional result of a growing left recursion, use it: record the expectations stored with the result (unless it is growing) and its recovered errors; on success, advance the position and return the value.
2. If the call is memoized, save the farthest failure and reset it to (start position, empty).
3. If the rule is a leader, evaluate it by [growing the seed](#left-recursion); otherwise [evaluate the body](#body-evaluation) once. On failure, remove the errors recovered during this call.
4. If the call is memoized, store in the memo the result, the end position, the recovered errors, whether the call ran inside a lookahead, and the expectations recorded inside; then merge those expectations into the saved farthest failure and restore it.
5. On failure, reset the position to the start position.

### Body Evaluation

To evaluate a body, the VM creates a new frame (sized for the rule's scope), saves the frame, the environment and the length of the capture trail, and runs the body's code.
While the body runs, it uses its own base of the entry stack and of the value stack.
When the body finishes, the VM restores the frame and the capture trail; on success, it performs [rule completion](#rule-completion); finally it restores the environment, so variables defined in the body are not visible to the caller.

### Left Recursion

A leader rule is evaluated as follows:

1. Store a failure for (r, pos, min) in the memo, marked as growing.
2. Reset the position to the start position, reset the recovered errors to their state before the call, and evaluate the body.
3. If the body fails, or does not advance beyond the previous success, stop. Otherwise store the result in the memo, marked as growing, and go back to step 2.
4. Finalize the last result (clear the growing mark) and return it.

### Rule Completion

The value of the rule is built from the body value v and the start position:

- **With an action:** evaluate the action with the `$n` items set to the children of v if the body is a sequence, and to [v] otherwise. If the result is a node newly created by the action, its range becomes the range of the rule. The result must be a node or nil.
- **With a text type:** create a struct of that type whose only field, `Text`, is the text of the rule's range, with the rule's range. A compiler writes such a rule, a text struct rule, for `rule r T = t:(e) #atomic => T{Text: text($t)}` where `T`'s field `Text` is a `string`: its body is `e #atomic` and it has no action or scope, since the struct is what the action would make.
- **Otherwise:** if the scope has captures, attach them. If v is fresh, give it the rule name if it has none, and clear its fresh mark.

**Attaching captures:** if no capture is non-nil, do nothing. If v is nil, is not fresh, or is itself one of the captures, wrap v in a tuple node whose only child is v, whose range runs from the start position to the current position, and which is fresh. Then add each non-nil capture as a field under its name.

## Operators Rule

The algorithm is the same as `operatorsRuleParse` in the closure backend; see the parsing rules in [Operators Rule](operators-rule.md#parsing-rules). Each part's code runs with its own entry-stack base and frame.

- **Skip:** increment `silent`, run the skip code, and on failure reset the position.
- **Longest match:** for each candidate operator, save the state, run the line's code in a new frame, and on success record the end position, the value, the frame, the environment, whether a cut was passed, and the recovered errors; then restore the state (writing the recorded captures back into the frame). A prefix or postfix candidate that succeeds without consuming input is treated as not matching. Select the candidate that advanced farthest; on a tie, select the one declared first. Also record whether some candidate failed after passing a cut, including an empty prefix/postfix match rejected as a candidate. A successful selected candidate takes precedence over this aggregate failure; each candidate's cut scope is isolated.
- **Head:** after skip, try the longest match among the prefix operators. If none matches and some candidate failed after passing a cut, the whole expression fails without trying operands. Otherwise, if one matches, apply it and parse the right operand at the operator's level. If the right operand fails, fail if a cut was passed; otherwise backtrack and try the operands. Operands are tried in declaration order; if an operand fails after passing a cut, the whole expression fails.
- **Tail:** after skip, try the longest match among the infix and postfix operators. If there is no candidate, stop (or fail, if a candidate failed after a cut). If the operator's level is less than or equal to `min`, or the operator is non-associative and a non-associative operator of the same level was just applied, backtrack and stop. A postfix operator is applied immediately. For an infix operator, parse the right operand at the operator's level (level - 1 for a right-associative operator); if that fails, fail if a cut was passed, and otherwise backtrack and stop.
- **Values:** if the operator has an action, evaluate it with local 0 = `$lhs`, 1 = `$rhs` and 2 = `$op` (a string node of the operator part's text). Otherwise build a tuple node whose children are `[op, rhs]`, `[lhs, op]` or `[lhs, op, rhs]` according to the operator kind, whose `operator` field is the operator's index, and which carries the rule name. The value of an operand is its action's result if it has an action; otherwise its captures are attached and, if it is fresh, it is given the rule name.

## Execution Models

A runtime implements one or both of two execution models, which differ in how rule calls and the Operators Rule are executed. Both must produce the same results.

- **Recursive model:** `CALL` and `OPERATORS_RULE` invoke the procedures in [Rule calls](#rule-calls) and [Operators Rule](#operators-rule) as host-language functions. The maximum nesting depth is bounded by the host stack.
- **Iterative model:** execution suspends at `CALL` and `OPERATORS_RULE` and resumes when the result is available (after a `CALL`, on success it pushes the value if keep is 1 and continues with the next instruction; on failure it proceeds as in [Failure](#failure)). Rule calls, left-recursion growth, the Operators Rule loop, longest match and skip are implemented as state machines whose state is kept in stack entries, and they perform the steps above in the same order. Host stack usage is constant, independent of how deeply the input nests.

In the reference implementation, both models share the instruction interpreter (`step`) and the stages of a rule call (`callBegin`, `growBegin`, `growStep`, `growEnd`, `invokeBegin`, `invokeEnd`, `callEnd`); they differ only in the order in which these are invoked (`vm.go`, `ivm.go`).
The reference implementation also limits the nesting depth of rule calls (100,000 by default, 10,000,000 for the iterative model; configurable with `pego.WithMaxDepth`) and reports a parse error when the limit is exceeded.
Actions and predicates may be evaluated with host function calls in either model, because their nesting depth is fixed by the grammar and does not depend on the input.

## Expression Code

Actions and predicates are evaluated by stack-based expression code. An evaluation error in an action is an error of the whole parse (`action in <rule name>: <message>`); in a predicate it is a failure.

| Code | Instruction | Operands | Behavior |
|--:|:--|:--|:--|
| 100 | `EINT` | A, B value | Push the integer A + B × 2³² (A and B signed, with 64-bit wrap-around): A holds its low 32 bits, and B is 0 for the integers of 32 bits. |
| 101 | `ESTR` | A string | Push a string. |
| 102 | `EBOOL` | A value | Push a boolean (0 or 1). |
| 103 | `ENIL` | | Push nil. |
| 104 | `ECAP` | A slot | Push the value of a capture slot. |
| 105 | `EITEM` | A n | Push `$n`. For n = 0, push a list value of all items. An out-of-range n is an error. |
| 106 | `ELOCAL` | A index | Push a local (a lambda parameter, or `$lhs`, `$rhs`, `$op`). |
| 107 | `EVAR` | A name | Push the value of a variable. An undefined variable is an error. |
| 108 | `EMEMBER` | A name, B text | Pop a value and push its field. `Start`, `End` and `Value` are built in for a node with a range: the range, and the text of a string node or the elements of a tuple or list node. A field not declared by the struct type is an error; a declared field that is absent yields nil. A name `$y` reads capture `y`: it is an error on a struct value, and an unmatched capture yields nil. With B 1 the value is an operand of the next comparison (`EBIN` with B 1): the `Text` field of a text struct may be pushed as a reference to the text, which only that comparison takes. |
| 109 | `ENEW` | A type, B field list | Pop as many values as the field list has names, and push a struct node. |
| 110 | `EBIN` | A operator, B text | Pop two values and push the result of the operation. The operator `[]` indexes the list below with the `int` on top. With B 1 the operator is a comparison whose operands may be text references pushed by `EMEMBER` with B 1. |
| 111 | `EUNARY` | A operator | Pop a value and push the result of the operation. |
| 112, 113 | `EAND` / `EOR` | A target | It is an error if the top value is not a boolean. `EAND` on false, and `EOR` on true, leave the value and jump to A. Otherwise pop the value and continue. |
| 114 | `EBOOLCHK` | A operator | It is an error if the top value is not a boolean. |
| 115 | `EFUNC` | A start, B parameter count | Push a function that captures the current locals. |
| 116 | `ECALL` | A built-in, B argument count | Pop the arguments, call the built-in function, and push the result. |
| 117 | `ERET` | | Return the top value as the value of the expression. |
| 118 | `ETEXTCHK` | | It is an error (the same as `text`'s) if `text` of the top value is not defined. The value stays. |
| 119 | `ETEXTEQ` | A neg | Pop two values and push whether their texts (as `text` gives them; both were checked with `ETEXTCHK`) are equal, or unequal if neg is 1. |
| 120 | `ELISTBEGIN` | | Start gathering the elements of a list. Gatherings nest. |
| 121 | `ELISTPUSH` | A n | Pop n values and add them, in order, to the list being gathered, with the checks of `list`. |
| 122 | `EMAPPUSH` | | Pop a function and a list (the list below) and add the results of calling the function on each element, with the checks of `map`. |
| 123 | `ELISTEND` | | Finish the innermost gathering and push its elements as a list value, as `concat` builds it. |
| 124 | | | Not used. Instruction sets before 14 used it to construct terminals, which the type system no longer has. |
| 125 | `ECHECK` | A type, B field, C struct | Check that the value on top of the stack, which stays there, fits the declared field type A, as a struct constructor checks a dynamically typed field value (see [Values and Constructors](actions.md#values-and-constructors)); a mismatch raises the evaluation error `cannot use <value type> as <A> in field <B> of <C>`. Each list element examined is one execution step. A is the type as written in the source after alias resolution (`*Ident`, `[]*(Ident \| Number)`, `int \| string`); B and C name the field and the struct type. The compiler emits it after a field value only when the type checker could not prove the value assignable, and never for fields of type `any`. |
| 126 | `ECAST` | A type | Check that the value on top of the stack, which stays there, fits type A, as `ECHECK` does, for a cast `x.(T)` (see [Casts](actions.md#casts)); a mismatch raises the evaluation error `cannot cast <value type> to <A>`. A is written like the type of `ECHECK`. The compiler omits it when every value fits the type, such as `any` or a type containing `any`. |

Names (`EVAR`, `EMEMBER`), operators (`EBIN`, `EUNARY`, `EBOOLCHK`, written as their source symbol such as `+` or `&&`) and type names (`ENEW`) are string-table indices.

Typed list constructors use `ECALL` with the `list` built-in; their element
types are checked at compilation and require no additional VM instruction or
per-element concrete-type guard.

Calling a function evaluates its code from its start, with the locals captured at creation followed by the arguments as its locals.

Instructions 118–123 are shortcuts with the same results as the general instructions. The reference compiler emits `ETEXTCHK` after each operand and `ETEXTEQ` for `text(a) == text(b)` and `text(a) != text(b)`, which compares the texts without making them values, and `ELISTBEGIN`, `ELISTPUSH`/`EMAPPUSH` per argument and `ELISTEND` for a `concat` call whose arguments are all `list`, `map` or such `concat` calls, which builds no intermediate list. In both cases the errors arise in the same order as with `ECALL`.

Built-in function indices: 0 `len`, 1 `text`, 2 `foldl`, 3 `foldr`, 4 `map`, 5 `list`, 6 `concat`, 7 `filter`. Their semantics are defined in [Actions](actions.md#operators-and-functions).

## Error Messages

A syntax error (see [Syntax errors](parsing.md#syntax-errors)) is formatted as `line:column: syntax error: expected A, B` (expectations sorted lexicographically). If `@error` messages apply, it is formatted as `line:column: message` (multiple messages joined with `; `). `@diagnostic` changes ordinary expected-item descriptions but does not replace explicit messages.
Expectation strings are those the compiler placed in the string table: literals in quotes, character classes in `[...]` form, `any character`, and so on.

## File Format

A compiled grammar uses container version 2 or 3. Version 2 stores one module and an optional grammar AST. Version 3 keeps that prefix and can add a length-delimited recognition bundle before the AST-present flag. The container version and each module's instruction-set version are separate version axes. Integers are variable-length: unsigned integers use LEB128 and signed integers use zigzag-encoded LEB128. A boolean is one byte (0 or 1).

| Part | Contents |
|:--|:--|
| Magic | `PEGOC\x00` (6 bytes) |
| Version | 1 byte (2 or 3). Version 2 has no recognition-bundle field. Version 3 adds that field after the main module. |
| Instruction-set version | Unsigned integer for this module: 15. Every module uses this version, whatever instructions it contains, and the optional recognition bundle declares it too. Earlier versions built the syntax trees that preceded the value model, held terminal types, or wrote field types with the earlier node type names; a runtime rejects them and asks for the grammar to be compiled again. |
| String table | Count, then each string (byte length and UTF-8 bytes), all unsigned. The module's string table is a prefix of this table. |
| Start rule | String index, unsigned (the empty string means no default start rule) |
| Package name | String index, unsigned |
| Module | See below |
| Recognition bundle (version 3 only) | Boolean, then when true an unsigned length in bytes and a recognition bundle. The bundle has its own instruction-set version, string table and module, followed by an ordered list mapping public start-rule names to rule indices. Its module and string table are independent of the main module. The loader validates that its ordered start names and rule indices match the main module. |
| AST present | Boolean. If true, the grammar AST and static-analysis results follow (including, per rule, whether it may have a value-free twin and which of its constructed fields and values are checked at run time). These serve the closure backend and grammar tools; a runtime may skip them. The AST codec does not serialize source positions, normalized comment ownership, or formatter trivia. |
| Checksum | CRC-32 (IEEE) of everything from the magic up to this point, 4 bytes little-endian |

The main module is laid out in the following order. A *list* is a count (unsigned) followed by its elements. Indices, positions and other integers inside the module are signed unless stated otherwise. A recognition bundle, when present, contains the same module layout plus the ordered start-name/index map described above.

1. The number of module strings, unsigned (this many strings from the start of the string table form the module's string table).
2. Source information, a boolean: whether the values a parse returns carry the rule names and capture names of the grammar. A loaded program keeps it, and the load options can also request it.
3. Character class table: a list of (negated, expectation string, number of ranges (unsigned), followed by the lower and upper bound of each range).
4. Type table: a list of (name, list of field names).
5. Field-list table, then scope table: each a list of lists of string indices.
6. Rule table: a list of (name, body, scope, action, body is a sequence, text type, memoize, leader, position-dependent, Operators Rule, value-free twin, transient, variables, check).
7. Operators Rule table: a list of (skip, list of operand lines, list of prefix operators, list of infix and postfix operators). A line is (start, scope, action, body is a sequence, check); an operator is (index, kind, associativity, level, line).
8. Match code, then expression code: each a list of instructions (opcode as 1 byte, then `A`, `B`, `C`).

### Load-Time Validation

On load, a runtime checks the following and rejects the file if any check fails:

- The checksum, the version and the instruction-set version are valid, and no data remains after the end.
- Every reference in the tables and instructions (strings, character classes, scopes, field lists, rules, Operators Rule tables, expression code, built-in functions) is in range. A version 3 recognition bundle must have a valid independent module and start map matching the main module.
- Every opcode is known, and every flag operand is within its range.
- The text type of a rule names a declared type with a field `Text`.
- Wide bounds reference `EINT` constants, fit the runtime's implementation `int`, and satisfy minimum ≥ 0 and maximum = -1 or maximum ≥ minimum.
- `ECHECK` references strings for the field and struct names, and a type text that parses as a field type naming only declared types, `Missing`, `error`, `node[string]`, `node`, `int`, `string`, `bool`, `nil` and `any`. The checks of rules and lines name type texts that parse the same way. The type text of `ECAST` parses as the type of `ECHECK` does. These type texts are resolved types, which contain no `&`, `_` or `_|_`.
- Every jump target is forward. Only `NEXT` jumps backward. The target of `EFUNC` is also forward.
- Every `RECOVER` body ends at an `ENDRECOVER` immediately before its skip target; that instruction jumps past an `ENDSKIP`. These recovery regions are nested and contain no stream-mode `NEXT` (mode 2) in either the protected body or skip. Element-local recovery ends before the enclosing streaming repetition's `NEXT`.

A runtime does not verify that instruction sequences use the value stack and the entry stack correctly. Runtime errors caused by invalid code are reported as parse errors, but termination is not guaranteed, so load files only from trusted sources.
