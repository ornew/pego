# 002: Transparent Choice by Default

- **Status**: Implemented
- **Author**: Gemini
- **Date**: 2025-06-29

> **Note (2026-10)**: Implemented by the engine rewrite. A choice returns the value of the matched alternative and never creates a node. See the rules for values and CST construction in [spec/parser-expressions.md](../../spec/parser-expressions.md).

## Abstract

The current PEGO VM adheres to a strict "1 rule, 1 node" principle. This means a `ChoiceRule` (e.g., `def r = a / b`) always produces a `ChoiceNode`. While this design ensures a predictable CST structure and simplifies the VM's core logic, it complicates AST construction. Grammar authors must write verbose actions to manually "unwrap" these intermediate `ChoiceNode`s to create a clean AST.

This document proposes a change to the default behavior of `ChoiceRule` to make it transparent. Instead of generating a `ChoiceNode`, a successful match in a `ChoiceRule` will leave the result of the matched alternative directly on the value stack. This change eliminates the intermediate `ChoiceNode` from the CST/AST, leading to cleaner grammar actions and more intuitive tree structures.

## Motivation

The primary goal is to eliminate intermediate `ChoiceNode`s from the final AST by default, without sacrificing the predictability of the core parsing logic.

Consider the following PEGO grammar:
```pego
type A struct { Children []Node }
type B struct {}
type C struct {}
type D struct {}

def b: B = "b" -> new B{}
def c: C = "c" -> new C{}
def d: D = "d" -> new D{}

def r = b / c d

def root: A = "a" r "e" -> new A{Children: $2}
```

With the current implementation, `$2` in the `root` rule's action would be a `ChoiceNode`. The desired outcome is for `$2` to be a flat list of nodes: `[B{}]` if the `b` branch is taken, or `[C{}, D{}]` if the `c d` branch is taken. This proposal will make that the default behavior.

## Proposed Design

This design modifies the compilation of `ChoiceRule` to no longer produce a `ChoiceNode`. It leverages the existing VM capabilities to handle the variable number of nodes that different alternatives can produce.

### 1. Compiler Changes for `ChoiceRule`

The `compileRule` function in `compiler.go` will be modified for `ChoiceRule`. The new compilation strategy will be as follows:

1.  **No `ChoiceNode` Creation**: The compiler will no longer emit `OpNewNode, NodeTypeChoice`.
2.  **Direct Result**: Each alternative within the `ChoiceRule` will be compiled to leave its result(s) directly on the value stack.
3.  **Consistent Handling**: The parent rule (e.g., a `SeqRule`) will collect these results. To handle the variable number of nodes, `SeqRule` compilation already uses `OpListStart` and `OpListEnd` (or will be updated to), which collects all items pushed by its sub-expressions into a single list.

**Revised Bytecode Generation for `def r = b / c d`:**

```
// def r = b / c d
OpChoice, L1

// --- Alternative 1: b ---
OpCall, rule_b      // Leaves Node(B) on the stack
OpCommit, L_END     // Jumps to the end

L1:
// --- Alternative 2: c d ---
OpCall, rule_c      // Leaves Node(C) on the stack
OpCall, rule_d      // Leaves Node(D) on the stack
// No wrapping SeqNode is created here by default for `c d`
// The two nodes are left on the stack.

L_END:
// The stack now contains either [..., Node(B)] or [..., Node(C), Node(D)]
```

### 2. Compiler Changes for `SeqRule`

To accommodate a `ChoiceRule` that can now produce a variable number of nodes, the compilation of `SeqRule` must be robust. It will use a list-based collection mechanism.

**Bytecode for `def root = "a" r "e"`:**

```
OpListStart                 // Mark stack position before sequence
...
OpCall, rule_a              // Pushes 1 node
...
OpCall, rule_r              // Pushes 1 (for b) or 2 (for c d) nodes
...
OpCall, rule_e              // Pushes 1 node
...
OpListEnd                   // Collects all items since ListStart into a single list
OpNewNode, NodeTypeSeq
OpSetField, FieldChildren   // Set the collected list as children
...
```
`OpListEnd` correctly gathers all nodes placed on the stack by the sequence's components, regardless of how many there are, into a single `[]any` slice. This list is then set as the `_$Children` of the resulting `SeqNode`.

### 3. Promoting Grandchildren

A related problem is flattening nested sequences, for example, making `(a b) c` result in a single sequence of `[a, b, c]` instead of `[SeqNode(a,b), c]`. This can be solved by a specific sequence of existing opcodes, without needing a new `OpPromoteChildren`.

If a rule `def s = (a b)` is called from `def t = s c`, we can make `s` transparent. The action for `s` could be `-> $0`. In the compiler, this would translate to:

1.  A `SeqNode` for `(a b)` is created as usual.
2.  The action `-> $0` is compiled. `$0` refers to the children of the `SeqNode`.
3.  This can be compiled to:
    - `OpGetField, FieldChildren` // Get the `[]any` children from the `SeqNode`
    - This leaves the list of children on the stack, effectively replacing the `SeqNode` with its children.

This pattern can be applied by the compiler whenever a rule's action is simply to return its own children (`-> $0`), achieving grandchild promotion.

## Conclusion

By changing the default compilation strategy for `ChoiceRule` to be transparent, we eliminate intermediate `ChoiceNode`s from the AST. This simplifies grammar definitions and aligns the language more closely with the user's intent of building a clean AST. The challenge of variable numbers of children is elegantly handled by ensuring that consuming rules like `SeqRule` use a list-based collection strategy (`OpListStart`/`OpListEnd`). This approach requires no new opcodes and no new grammar syntax, relying instead on a smarter compilation of existing language features. It is a minimal, yet powerful, change that significantly improves the ergonomics of PEGO for AST construction.