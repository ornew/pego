# 003. SCC-Based Memoization and Left-Recursion Support

- **Version**: 2.0
- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2025-06-30

> **Note (2026-10)**: The engine has since been rewritten from a bytecode VM into a compiler that turns parser expressions into closures. The SCC-based analysis and the grow-the-seed approach were carried over. For the current implementation, see [docs/development.md](../development.md#architecture).

## 1. Summary

This document describes the design used to add efficient packrat parsing (memoization) to the PEGO parser, together with an algorithm that correctly handles direct and indirect left recursion.

In the VM-based implementation, the compiler analyzes the dependencies between grammar rules as a graph and detects its strongly connected components (SCCs); the VM then applies a memoization strategy chosen from that analysis. To resolve left recursion, the "grow-the-seed" algorithm was introduced. This makes it possible to support left-recursive grammars, which could not be handled before, while achieving linear-time parsing.

## 2. Background and problems

- **Performance**: Packrat parsing memoizes the result of each rule keyed by input position, which guarantees that a rule is never parsed twice at the same position. Parsing time therefore approaches linear time (O(n)) in the length of the input. The previous implementation had no such general memoization mechanism.
- **Left recursion**: A grammar with left recursion, such as `E -> E '+' T | T`, sends a naive top-down recursive-descent parser into an infinite loop. The same holds for PEG, and indirect left recursion (for example `A -> B`, `B -> A`) causes the same problem. Solving it requires detecting the recursion and handling it specially.

## 3. Goals

- Memoize the results of all rules to achieve packrat parsing.
- Parse grammars that contain direct and indirect left recursion correctly.
- Preserve the longest-match principle and guarantee unambiguous results.
- Clearly separate static analysis at compile time from dynamic processing in the VM at run time.

## 4. Design

The implementation is split into two phases: an analysis phase at compile time and a processing phase in the VM at run time.

### Phase 1: Grammar analysis at compile time (`compiler.go`, `compiler_analysis.go`)

After receiving the `Grammar` object, and before generating VM bytecode, the compiler performs the following static analyses.

#### 4.1. Building the rule dependency graph and detecting SCCs

The compiler builds a directed graph whose nodes are all the rules in the grammar, and applies Tarjan's algorithm to find all of its strongly connected components (SCCs). This identifies the groups of mutually recursive rules.

#### 4.2. Nullability analysis

`analyzeNullability` determines whether each rule can match the empty string. This is essential for deciding whether a recursion is a *left* recursion. The analysis iterates until the results stabilize, so it handles circular definitions correctly.

#### 4.3. Detecting left recursion and choosing the memoization type

Using the SCC and nullability information, the compiler decides which memoization strategy each rule uses. A map holding this information was added to the `Program` object.

```go
// in compiler.go
type MemoizationType int

const (
    NoMemo MemoizationType = iota
    NormalMemo
    LeftRecursiveMemo
)

type Program struct {
    // ...
    MemoTypes map[int]MemoizationType // key: RuleID
}
```

Each SCC is classified as follows:

- **Non-recursive rules**: a rule whose SCC has size 1 and no self-loop is set to `NormalMemo`.
- **Recursive rules** (SCC size > 1, or a self-loop):
  - Determine whether the SCC is left-recursive, that is, whether there is a path to a recursive call that passes only through nullable rules.
  - **Not left-recursive**: set every rule in the SCC to `NormalMemo`.
  - **Left-recursive**:
    - Choose the rule with the smallest RuleID in the SCC as the *leader*.
    - Set only the leader to `LeftRecursiveMemo`.
    - Set the other members of the SCC to `NoMemo`. This guarantees that evaluation of the recursion always starts at the leader, which makes the VM's processing more efficient.

### Phase 2: Run-time processing in the VM (`vm.go`)

The VM changes the behavior of the `OpCall` instruction according to the `MemoTypes` map in the `Program` object produced by the compiler.

#### 4.4. The memo table

The VM struct holds the memo table as a nested map.

```go
// in vm.go
type memoEntry struct {
    success bool
    endPos  int
    node    *Node
}

type VM struct {
    // ... existing fields ...
    memo    map[int]map[int]*memoEntry // ruleID -> pos -> memoEntry
    lrStack map[int]int                // ruleID -> startPos (tracks rules under left-recursive evaluation)
}
```

`lrStack` records which rule is being evaluated at which position while the grow-the-seed algorithm runs.

#### 4.5. Changes to `OpCall`

When the VM executes `OpCall` (operand: `ruleID`), its logic branches on `program.MemoTypes[ruleID]`:

1. First, look up `vm.memo[ruleID][vm.pos]`; on a hit, return the stored result immediately.
2. On a miss, branch on the memoization type:

- **`NormalMemo`**:
  1. Write a provisional "failure" entry to the table to prevent infinite loops.
  2. Perform an ordinary rule call (push a frame on `callStack` and jump to the rule's IP).
  3. On returning from the rule with `OpReturn`, record the final result (success or failure) in the memo table.

- **`LeftRecursiveMemo`**:
  - Call `handleLeftRecursiveMemo`, which runs the grow-the-seed algorithm.

- **`NoMemo`**:
  - Call the rule without memoization. This applies to the non-leader members of a left-recursive SCC and restricts the entry point of the evaluation to the leader.

#### 4.6. Left-recursive memoization (`handleLeftRecursiveMemo`)

`handleLeftRecursiveMemo` implements grow-the-seed as follows:

1. **Recursion detection**: Check `lrStack`. If the same rule is already being evaluated at the same position, the call is recursive and returns "failure" immediately. This is the base case of the recursion.

2. **Seeding**:
   1. Record the current `(ruleID, pos)` in `lrStack`.
   2. Write a provisional "failure" entry (the seed) to the memo table.
   3. Evaluate the rule with the `evalRule` helper. The recursive calls fail because of step 1, so only the non-recursive alternatives are evaluated, which yields the first seed result.

3. **Growing**:
   1. If the seed parse fails, the result is a permanent failure.
   2. If the seed parse succeeds, start a loop with that result as the best result (`bestEntry`).
   3. In the loop, store the current `bestEntry` in the memo table and call `evalRule` again. This time, the recursive calls can use the memoized `bestEntry`, so the rule may match a longer input.
   4. If the new result does not match further than `bestEntry`, growth has stopped and the loop ends.
   5. If it matches further, update `bestEntry` and continue the loop.

4. **Finalization**:
   1. Remove `(ruleID, pos)` from `lrStack`.
   2. Apply the final `bestEntry` to the VM state and store it in the memo table.

`evalRule` is the key helper that evaluates a rule speculatively, saving and restoring the main VM state (IP, pos and the stack pointers).

## 5. Implementation milestones

The design was implemented in the following milestones:

- **M1: Utilities**: graph structures, SCC detection (Tarjan) and nullability analysis in `compiler_analysis.go`, with unit tests (`compiler_analysis_test.go`).
- **M2: Compiler**: integrate the static analysis into `Compile` in `compiler.go` and store the `MemoTypes` map in the `Program` struct.
- **M3: VM data structures**: add the `memo` table and `lrStack` to the `VM` struct in `vm.go`.
- **M4: VM logic**: branch the handling of `OpCall` on the memoization type, and implement `handleLeftRecursiveMemo` and `evalRule`.
- **M5: Tests**: add test cases for direct and indirect left recursion to `vm_left_recursion_handling_test.go` and confirm that they all pass.

## 6. Risks and considerations

- **Captures**: The interaction between memoization and captured AST nodes is handled by including `*Node` in `memoEntry`. This guarantees that nodes produced during the growing phase of left recursion are updated correctly.
- **Performance**: The memo table uses `map[int]map[int]*memoEntry`, which performs well with Go's map implementation. The impact on garbage collection was judged acceptable for current use cases.
