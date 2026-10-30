# 053. Variable Declarations

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-13

## Summary

A parser variable is declared by a top-level `var` declaration, next to the type and rule declarations. The
declaration gives the variable a static type, `int`, `string` or `bool`, and an initial value that every parse
starts with:

```pego
var depth int = 0
var mode string
var seen = false
```

Actions rebind a declared variable with `x := expr`, and read it by its name. The type checker gives every
read the declared type, so `x + 1` and `V{N: x}` are checked when the grammar is compiled. Reading or binding
a name that is not declared is a compile error.

## Motivation

Context-sensitive grammars keep indentation, modes and counters in variables. Before declarations, a variable
came into existence at its first binding: its type was unknown to the type checker, its value was undefined
until a rule bound it, and the start of a parse depended on which rule happened to bind it first. A grammar
with several entry points had to repeat the setup in each of them, and a misspelled name was an error only at
run time, if the input reached it.

A declaration states the type and the starting value once, for every entry point, and makes every use of the
name checkable at compile time.

## Goals

- Give every variable a static type that the type checker applies to reads and bindings.
- Start every parse, through every entry point and every backend, from the same initial values.
- Report undeclared names, duplicate declarations and mistyped bindings when the grammar is compiled.
- Keep the semantics of `x := expr` unchanged: a binding is undone on backtracking and when the rule returns.
- Keep memoization and Document reuse keyed by the values of the variables a rule depends on.

## Non-Goals

Declarations add no mutable global state, no startup callback and no second initialization phase. They do
not add variable types other than `int`, `string` and `bool`, nil variables, or initializers that run code.

## Design

### Public Behavior and Contracts

A declaration is `var name [type] [= value]`. At least one of the type and the value is required.

- The type is `int`, `string` or `bool`. Any other type, including `nil`-able and node types, is a compile error.
- The value is an int, string or bool literal; an int literal may be negated. Without a value, the variable
  starts as the zero value of its type: `0`, `""` or `false`. Without a type, the type is the type of the value.
  `=` defines the initial value; it is not a binding. `nil` is rejected.
- A value of another type than the declared type is a compile error. An initializer is a literal, so it cannot read
  another variable and the order of declarations does not matter.
- A name is declared once. A second declaration of the same name is a compile error.
- A declaration is a top-level statement and may appear anywhere among the other declarations.

Within rules, `name := expr` binds a declared variable and `name` reads it. The expression must be assignable to the
declared type. A value whose type the checker does not know is rejected as well, and the error tells the author to
convert it explicitly with a cast, `x := $d.(int)`: a binding is checked only at compile time, so no run-time check
and no run-time type error exists for it. A cast that fails is an ordinary evaluation error, which makes the binding's
predicate fail. A binding that the rule makes is visible to the rest of the rule and to the rules it calls, and it is
undone when an alternative fails and when the rule returns, as before. A read sees the nearest binding, or the initial
value when nothing has bound the variable.

### Implementation and Invariants

The compiler collects the declarations into a table of names, types and initial values, and every program carries the
table. Each parse begins by giving the parser the environment built from the table, once, before the start rule is
entered. The environment is a persistent vector indexed by the variables' slots (their declaration order), so parses share the
initial one and a bind copies only the path to its slot.
Because every declared variable is bound from the start, a rule that depends on a variable has the same memo key
whether or not an earlier rule bound it.

The saved formats carry the table: the bytecode module lists the declarations, so a program loaded without its syntax
tree seeds the same environment. Generated Go and TypeScript parsers embed the declarations and seed them in the
one place every entry point passes through.

### Compatibility and Backend Support

All backends seed the initial environment: the closure evaluator, the recursive and iterative bytecode machines, generated
Go (plain and typed) and generated TypeScript. The bytecode instruction set version and the compiled file format change
to carry the table. A grammar without declarations has an empty table and behaves as before; a grammar that used
undeclared variables must declare them. The hand-written parser and the grammar of the grammar syntax accept the same
declarations, and the formatter, the JSON form and the language server know them.

## Alternatives Considered

- **An `init` block of bindings.** It sets values but not types, so a read still has an unknown type, and it
  introduces a second kind of statement that must be validated to hold bindings only.
- **Declaring the variable at its first binding.** The type of a read would depend on control flow, and a read before
  any binding would stay a run-time error.
- **Initializing in the start rule, or by parser options.** Each entry point would repeat the setup, or grammar
  semantics would move into host code with a different contract for every API.
- **Package-level mutable variables.** Results would depend on parser reuse and call order, and concurrent parses
  would be unsafe.

## Testing

The tests cover every compile error with its message; the initial values on the closure evaluator, both bytecode
machines, generated Go (plain and typed) and TypeScript; typed reads and bindings; start rules other than the main
one; Documents, saved programs with and without their syntax tree, and the memoization of rules that read variables;
the agreement of the hand-written parser with the grammar of the grammar syntax; and that a grammar without
declarations is unchanged.

## Limitations and Open Questions

An initializer is a literal. Initializers that compute a value from other declarations are not supported.
