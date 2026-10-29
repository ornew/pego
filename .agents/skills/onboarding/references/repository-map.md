# Repository Map and Source Authority

Start with [README](../../../../README.md) for the product and documentation index,
then [development](../../../../docs/development.md) for the current architecture,
directory layout and implementation status. Search the relevant package and its tests
before choosing an edit location:

| Concern | First stop |
|:--|:--|
| Public Go API and grammar AST | `pego.go`, `grammar/` |
| Grammar source parsing | `internal/syntax/` |
| Compiler and static analysis | `internal/compile/` |
| Execution, bytecode VMs and code generation | `internal/engine/` |
| Generated Go parser source | Generator `internal/gen/gogen/generator.go` and runtime package `runtime/` (imported or copied into generated parsers); generated parsers under `parsers/` and `bench/gen/` are outputs |
| CLI, editor service and sample generation | `cmd/pego/`, `internal/lsp/`, `sample/` |
| Ready-made parsers, playground and site | `parsers/`, `cmd/playground/`, `site/` |

Use [specification index](../../../../spec/README.md) for supported language and
parsing contracts. Use [design records](../../../../docs/design/records/000-template.md) for
decisions and proposals, and [development status](../../../../docs/development.md#language-feature-status)
to distinguish them from shipped behavior. Use the [guide index](../../../../docs/guide/README.md)
for user-facing workflows. For optimization mechanisms and applicability, use the
[catalog](../../../../docs/optimizations/README.md) and
[performance method](../../../../docs/performance.md).

The live [Issue #1](https://github.com/ornew/pego/issues/1) is the backlog, not a
language contract. Fetch its present body and relevant comments before planning or
updating an item; branch, worktree and integration status also need live checks.
Use the [documentation](../../documentation/SKILL.md),
[backlog](../../backlog/SKILL.md), [performance](../../performance/SKILL.md) or
[nanosite](../../nanosite/SKILL.md) skill when that work is in scope.
