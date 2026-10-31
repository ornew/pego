# Security Policy

ZeroParse parses input that a program often does not control. This page says how to report a vulnerability, what counts as
one, what happens after a report, and what a parse bounds today and what the caller must bound.

ZeroParse is not safe by default for untrusted input yet. Only the nesting limit is on by default; budget limits are opt-in,
and nothing limits input length or memory. The [resource policy](#resource-policy) below says what to set.

## Reporting a Vulnerability

Report a vulnerability privately through GitHub's private vulnerability reporting: open the repository's **Security**
tab and choose **Report a vulnerability**. Do not open a public issue for it. Include:

- the ZeroParse commit or version, and the Go or Node.js version;
- the grammar, and the input or a program that builds it;
- the backend (closure, recursive VM, iterative VM, generated Go or generated TypeScript), the generation flags and the
  parse options, including the limits that were set;
- what happened, and what you expected from the documentation.

Reports are handled on a best-effort basis; no response time is promised.

**Disclosure.** A confirmed vulnerability is fixed and then published as a GitHub Security Advisory, with a CVE where
one applies. A disclosure date is agreed with the reporter; when no other date is agreed, the advisory is published 90
days after the report. Reporters are credited in the advisory unless they decline.

## Supported Versions

ZeroParse has no tagged release yet. Fixes land on the main branch; once releases exist, the latest v0.x release also gets
them.

A fix in the runtime reaches a generated parser only when the parser is generated again with a fixed ZeroParse. A parser
generated with the default `-runtime embed` contains its own copy of the runtime, so `govulncheck` and the Go
vulnerability database cannot see that copy; regenerate generated parsers after upgrading ZeroParse.

## What Counts as a Vulnerability

- **Resource use beyond an enabled limit.** A parse that keeps running after its spent budget exceeds the budget limits
  that were set, or that nests recursive rule calls past the nesting limit without the nesting error, on any backend or
  generated parser that supports the limit.
- **A crash or panic on any input.** A panic from ZeroParse's code that escapes a parse, a fatal Go runtime error such as a
  stack overflow at the default nesting limit, or a generated TypeScript parser that throws instead of returning an
  error. The documented exceptions are listed in the next section.
- **A wrong result that breaks a stated contract.** For example, a `Document` reparse that differs from a fresh parse
  of the same text, backends that disagree on a feature they share, or a recognizer that accepts input the full parse
  rejects. Report these privately when a program could be misled by them, such as a validator that accepts input the
  parser later rejects. When in doubt, report privately.
- **A crash or hang of the grammar compiler.** A panic, a fatal error or a compilation that does not finish on any
  grammar source, including one from an untrusted source and including compilation by `zp lsp`, which compiles the
  `.zp` files you open in an editor.

## What Does Not Count

These behaviors are documented, so they are limits of ZeroParse today, not vulnerabilities:

- Unbounded time or memory when the limits are off. Budget limits are opt-in, and no limit exists for input length or
  memory.
- Memory use within the budget limits that were set. Budget limits are not a memory limit: their counters are not bytes,
  and their units differ between backends. The memory a parse allocates is at most proportional to its spent budget
  plus the input ([Time and Memory](spec/runtime/parsing.md#time-and-memory)), so `Budget` bounds memory only
  indirectly.
- Slow parses of a grammar whose cost grows faster than the input, when no budget limit is set.
- The time and memory that parsing with a grammar uses, including a grammar from an untrusted source: a grammar can do
  exponential cost by design. Parse with an untrusted grammar under the same limits as untrusted input.
- A fatal stack overflow after raising the nesting limit beyond what the Go stack holds, on the closure backend or the
  recursive VM ([deep nesting](docs/guide/backends.md#deep-nesting-and-withmaxdepth)).
- A reader or a callback of yours that blocks: a parse cannot preempt it.
- A panic that starts in your own code, such as a stream callback or a reader, and passes through a parse.
- Loading a compiled grammar (`.zpc`) or bytecode from a source you do not trust: loading checks the structure but
  does not verify the module, and made-up bytecode is not guaranteed to terminate
  ([Loading Untrusted Data](docs/guide/compiled-grammars.md#loading-untrusted-data)).

## Resource Policy

### What a Parse Bounds Today

- **Nesting, on by default.** A parse fails with `nesting too deep: more than N nested recursive rule calls` once recursive rule calls (rules on a cycle of calls) nest deeper
  than 100,000, or 10,000,000 on the iterative VM (`zp.BytecodeIterative`), which keeps rule calls on its own stack.
  Generated Go and generated TypeScript have the same default of 100,000. `zp.WithMaxDepth`, `zp gen -max-depth`
  and the TypeScript `maxDepth` option change it. On Node.js's default stack, generated TypeScript reaches a few
  hundred to a few thousand nested recursive rule calls and then returns an error
  ([TypeScript](docs/guide/typescript.md#deep-nesting-and-the-javascript-stack)).
- **Budget, opt-in.** With `zp.WithLimits(zp.Limits{Budget: ...})` set,
  a parse stops once its spent budget exceeds the limit, whatever the grammar's class of growth, and `zp.WithContext` adds
  cancellation and deadlines. A zero limit is unlimited. Generated parsers offer these options only when generated
  with `-budget-controls` (TypeScript: `signal` and `budget`); this is planned to become the default
  at the first release ([Default Limits](#default-limits)). For a given artifact, input and options, the counts are
  the same on every run. `zp parse` has no budget limit flag
  ([Cancellation and Budget Limits](docs/guide/cancellation-and-limits.md)).
- **Stream memory, by working set.** A `ParseStream` parse releases the input, the memo entries and each element as it
  goes, so its memory is bounded by the working set (the largest element, the memo entries of the last few elements
  and your callback's own state), not by the input length. It keeps the list of recovered errors until the end unless
  `zp.WithDiagnosticLimit` bounds it ([Streaming](docs/guide/streaming.md#memory)).

### What the Caller Must Bound

- **Input length.** Nothing limits it. A whole parse of a 27 MiB CSV file peaked at about 1.0 GB.
- **Memory.** No memory limit exists, and budget limits bound memory only indirectly, through the budget spent building it.
- **The size of one stream element.** Nothing limits it, and one element that spans the input defeats streaming.
- **Open `Document`s.** A `Document` keeps its text and memo table between parses: at 100,000 lines (1.9 MiB of
  text), a `Document` with its tree holds about 151 MiB
  ([Incremental Parsing Performance](docs/guide/incremental-performance.md)). Bound how many a program keeps open and
  how large they grow.
- **Time.** Use a context deadline. Cancellation is cooperative: it takes effect at the next point where the parser
  checks it, and a blocked reader or callback is not interrupted.

### Which Inputs Are Expected to Be Slow

ZeroParse does not promise linear time. Memoization bounds how often a rule body runs at one position, but some grammars do
quadratic, cubic or exponential cost, and with such a grammar, input chosen by an attacker can reach the worst case.
The specification defines the classes of growth, with examples, and sufficient conditions for linear cost
([Time and Memory](spec/runtime/parsing.md#time-and-memory)). No tool classifies a grammar yet, so set budget limits
for any grammar that parses untrusted input.

### Parsing Untrusted Input

1. Set `Budget`. It is one counter for the input a parse inspects, its rule calls and loop iterations, and the
   values it builds, so it bounds a loop that only scans input and a value that grows without reading input alike.
   Generate parsers with `-budget-controls`.
2. Set a context deadline.
3. Limit the input length before parsing, and for a stream, the length the reader delivers.
4. Keep the default nesting limit or lower it. For input that nests deeply, use the iterative VM instead of raising
   the limit.
5. Bound recovered errors in a stream with `zp.WithDiagnosticLimit`, and bound the number of open `Document`s.
6. Load compiled grammars only from sources you trust.

### Default Limits

- **Today.** Budget limits are opt-in, and generated parsers carry them only when generated with `-budget-controls`.
- **Planned for the first release.** `zp gen` generates parsers with budget controls by default, as `-budget-controls`
  does today. Every parse gets one fixed budget proportional to the input length (c·n with a large constant), and
  an unlimited parse has to be requested explicitly. A lint warns about grammars outside the specification's
  sufficient conditions for linear cost.
- **Later.** Budgets refined per grammar by a complexity analysis, and optional caps on input length and on estimated
  memory.

Until the planned defaults ship, the limits above are the caller's to set.
