# 004. Stabilizing Left Recursion in the VM

- **Status**: Superseded
- **Author**: @ornew
- **Date**: 2025-06-30

> **Note (2026-10)**: This document records an investigation of the former bytecode VM. Since the engine rewrite, left recursion is handled as in pegen: only the leader rule is evaluated with grow-the-seed (see [docs/development.md](../development.md#architecture)).

## 1. The original problem and its analysis

The initial VM implementation failed to parse grammars with simple direct left recursion such as `Expr -> Expr '+' Term | Term`. The test cases `simple_addition` and `chained_addition` reported failures where the parser should have succeeded.

Analysis of the debug logs showed that the left-recursion growth algorithm ("growing the seed") itself found the longest match (for example `x+x`) correctly. However, the VM's state transitions on returning from the left-recursive evaluation, after the growth loop had finished, were flawed, and as a result the parse as a whole failed.

## 2. Investigation and abandoned approaches

Several hypotheses were tested to solve the problem, but none of the fixes addressed the root cause.

### 2.1. Hypothesis 1: `choiceStack` was not reset

- **Idea**: The `choiceStack` (the backtrack stack for alternatives) was not reset on each iteration of the left-recursion growth loop, which might be corrupting the state. A fix reset `choiceStack` in `OpReturn` when returning to the top of the loop.
- **Problem**: The tests still failed. Resetting `choiceStack` pointed in the right direction but was not sufficient, which suggested that the underlying problem lay deeper in state management.

### 2.2. Hypothesis 2: A `SuperChoice` opcode

- **Idea**: The semantics of PEG's ordered choice (`|`) might conflict with the "longest match" that left recursion requires. Based on this hypothesis, a new family of opcodes for a longest-match choice specific to left-recursive rules (`OpOpenSuperChoice`, `OpRegisterChoice`, `OpCloseSuperChoice`) was proposed.
- **Problem**: Analysis of the `pegen` reference implementation showed that this approach was fundamentally wrong. `pegen` does not change the semantics of choice; it obtains the longest match indirectly by **re-evaluating the whole rule repeatedly until the result stops growing**. `SuperChoice` overcomplicated the problem and missed the point.

### 2.3. Hypothesis 3: Corruption of a global `failed` flag

- **Idea**: The VM's single `vm.failed` flag served two different purposes, which might be the source of the corruption:
  1. **Failure for choice**: a failure that triggers backtracking.
  2. **Failure for left recursion**: a failure set deliberately by the algorithm to prevent infinite recursion.

  The hypothesis was that the two interfered with each other and corrupted the state. A fix was considered in which `OpCall` would handle the failure on left-recursion detection with local backtracking instead of the global `failed` flag.
- **Problem**: The fix considered only one case (when a choice is present) and was not general. Given the other failure patterns, it was likely to introduce new bugs.

## 3. The common root cause: no separate execution context

All of the failed approaches pointed to the same root cause: **there was no independent execution context for managing the left-recursion growth loop**.

In `pegen`, the `while` loop in the `memoize_left_rec` decorator provides that independent context. The loop manages its own state (the best result and the last position) and calls the parser method inside it like a subroutine. Failures inside the method do not directly corrupt the loop's state.

In our VM, the logic corresponding to this loop was implemented inside `OpReturn`, which shared state (the `failed` flag, `choiceStack` and so on) with the VM's main loop. State conflicts and corruption were therefore unavoidable by construction.

## 4. Final proposal: encapsulating the context in an `OpEvalLeftRecursive` opcode

To solve the problem structurally, introduce a dedicated opcode, `OpEvalLeftRecursive`, that manages the entire left-recursion growth loop.

### 4.1. Design overview

The new opcode fully encapsulates the role of `pegen`'s `memoize_left_rec` decorator as a VM instruction.

- **The compiler's role:**
  When compiling a rule that is a left-recursion leader (for example `Expr`), the compiler generates code that evaluates the rule body through `OpEvalLeftRecursive` instead of evaluating it directly.

  **Before:**
  ```
  // Rule Expr:
  OpChoice(...)
  ...
  OpReturn
  ```

  **After:**
  ```
  // Rule Expr:
  OpEvalLeftRecursive(Address_of_Expr_Body)
  OpReturn

  // Rule Expr_Body:
  OpChoice(...)
  ...
  OpReturn // return from the subroutine
  ```

- **Behavior of `OpEvalLeftRecursive` in the VM:**
  When this opcode executes, the VM enters an independent subroutine evaluation loop that runs the following logic:

  1. **Initialization:** Set up a left-recursion frame (`lrStack`) and prime the memo at the current position with "failure".
  2. **Growth loop:**
     1. Save the VM state (`pos`, stack pointers and so on) before the iteration.
     2. Call `evalRuleBody` recursively, **as a subroutine**. The call ends at the `OpReturn` at the end of `Expr_Body`.
     3. **Evaluate the result:** After the subroutine returns, check `vm.failed` and `vm.pos`.
     4. **Check for growth:** If the subroutine succeeded and `vm.pos` advanced beyond the previous iteration, store the best result in `lrStack` and `memo` and continue the loop (back to step 1).
     5. **Stop growing:** If the subroutine failed or `pos` did not grow, exit the loop.
  3. **Finalization:** Push the best result found by the loop onto `valueStack`, update the VM state (`pos`, `failed`) to match the final result, and continue with the instruction after `OpEvalLeftRecursive`.

### 4.2. Expected benefits

- **Complete separation of concerns:** The complex logic of the left-recursion growth loop is fully encapsulated in a single instruction, `OpEvalLeftRecursive`.
- **State safety:** Because `evalRuleBody` is called as a subroutine, the VM's global state is no longer corrupted on each iteration of the loop, which greatly simplifies and stabilizes state management.
- **A clearer architecture:** This structure matches the spirit of the `pegen` reference implementation, which makes it reliable and easier to maintain.
