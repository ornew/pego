# Security Policy

PEGO parses input that a program often does not control. This page says how to report a vulnerability, what counts as
one, what happens after a report, and what a parse bounds today and what the caller must bound.

PEGO is not safe by default for untrusted input yet. Only the nesting limit is on by default; work limits are opt-in,
and nothing limits input length or memory. The [resource policy](#resource-policy) below says what to set.

## Reporting a Vulnerability

Report a vulnerability privately through GitHub's private vulnerability reporting: open the repository's **Security**
tab and choose **Report a vulnerability**. Do not open a public issue for it. Include:

- the PEGO commit or version, and the Go or Node.js version;
- the grammar, and the input or a program that builds it;
- the backend (closure, recursive VM, iterative VM, generated Go or generated TypeScript), the generation flags and the
  parse options, including the limits that were set;
- what happened, and what you expected from the documentation.

Reports are handled on a best-effort basis; no response time is promised.

**Disclosure.** A confirmed vulnerability is fixed and then published as a GitHub Security Advisory, with a CVE where
one applies. A disclosure date is agreed with the reporter; when no other date is agreed, the advisory is published 90
days after the report. Reporters are credited in the advisory unless they decline.

## Supported Versions

PEGO has no tagged release yet. Fixes land on the main branch; once releases exist, the latest v0.x release also gets
them.

A fix in the runtime reaches a generated parser only when the parser is generated again with a fixed PEGO. A parser
generated with the default `-runtime embed` contains its own copy of the runtime, so `govulncheck` and the Go
vulnerability database cannot see that copy; regenerate generated parsers after upgrading PEGO.

## What Counts as a Vulnerability

- **Resource use beyond an enabled limit.** A parse that keeps running after its counted work exceeds the work limits
  that were set, or that nests rule calls past the nesting limit without the nesting error, on any backend or
  generated parser that supports the limit.
- **A crash or panic on any input.** A panic from PEGO's code that escapes a parse, a fatal Go runtime error such as a
  stack overflow at the default nesting limit, or a generated TypeScript parser that throws instead of returning an
  error. The documented exceptions are listed in the next section.
- **A wrong result that breaks a stated contract.** For example, a `Document` reparse that differs from a fresh parse
  of the same text, backends that disagree on a feature they share, or a recognizer that accepts input the full parse
  rejects. Report these privately when a program could be misled by them, such as a validator that accepts input the
  parser later rejects. When in doubt, report privately.
- **A crash or hang of the grammar compiler.** A panic, a fatal error or a compilation that does not finish on any
  grammar source, including one from an untrusted source and including compilation by `pego lsp`, which compiles the
  `.pego` files you open in an editor.

## What Does Not Count

These behaviors are documented, so they are limits of PEGO today, not vulnerabilities:

- Unbounded time or memory when the limits are off. Work limits are opt-in, and no limit exists for input length or
  memory.
- Memory use within the work limits that were set. Work limits are not a memory limit: their counters are not bytes,
  and their units differ between backends. The memory a parse allocates is at most proportional to its counted work
  plus the input ([Time and Memory](spec/runtime/parsing.md#time-and-memory)), so `MaxSteps` bounds memory only
  indirectly.
- Slow parses of a grammar whose work grows faster than the input, when no work limit is set.
- The time and memory that parsing with a grammar uses, including a grammar from an untrusted source: a grammar can do
  exponential work by design. Parse with an untrusted grammar under the same limits as untrusted input.
- A fatal stack overflow after raising the nesting limit beyond what the Go stack holds, on the closure backend or the
  recursive VM ([deep nesting](docs/guide/backends.md#deep-nesting-and-withmaxdepth)).
- A reader or a callback of yours that blocks: a parse cannot preempt it.
- A panic that starts in your own code, such as a stream callback or a reader, and passes through a parse.
- Loading a compiled grammar (`.pegoc`) or bytecode from a source you do not trust: loading checks the structure but
  does not verify the module, and made-up bytecode is not guaranteed to terminate
  ([Loading Untrusted Data](docs/guide/compiled-grammars.md#loading-untrusted-data)).

## Resource Policy

### What a Parse Bounds Today

- **Nesting, on by default.** A parse fails with `nesting too deep: more than N rule calls` once rule calls nest deeper
  than 100,000, or 10,000,000 on the iterative VM (`pego.BytecodeIterative`), which keeps rule calls on its own stack.
  Generated Go and generated TypeScript have the same default of 100,000. `pego.WithMaxDepth`, `pego gen -max-depth`
  and the TypeScript `maxDepth` option change it. On Node.js's default stack, generated TypeScript reaches a few
  hundred to a few thousand nested rule calls and then returns an error
  ([TypeScript](docs/guide/typescript.md#deep-nesting-and-the-javascript-stack)).
- **Work, opt-in.** With both limits set, `pego.WithWorkLimits(pego.WorkLimits{MaxInputWork: ..., MaxSteps: ...})`
  stops a parse once its counted work exceeds them, whatever the grammar's class of growth, and `pego.WithContext` adds
  cancellation and deadlines. A zero limit is unlimited. Generated parsers offer these options only when generated
  with `-work-controls` (TypeScript: `signal`, `maxInputWork` and `maxSteps`); this is planned to become the default
  at the first release ([Default Limits](#default-limits)). For a given artifact, input and options, the counts are
  the same on every run. `pego parse` has no work-limit flag
  ([Cancellation and Work Limits](docs/guide/cancellation-and-limits.md)).
- **Stream memory, by working set.** A `ParseStream` parse releases the input, the memo entries and each element as it
  goes, so its memory is bounded by the working set (the largest element, the memo entries of the last few elements
  and your callback's own state), not by the input length. It keeps the list of recovered errors until the end unless
  `pego.WithDiagnosticLimit` bounds it ([Streaming](docs/guide/streaming.md#memory)).

### What the Caller Must Bound

- **Input length.** Nothing limits it. A whole parse of a 27 MiB CSV file peaked at about 1.0 GB.
- **Memory.** No memory limit exists, and work limits bound memory only indirectly, through the work that builds it.
- **The size of one stream element.** Nothing limits it, and one element that spans the input defeats streaming.
- **Open `Document`s.** A `Document` keeps its text and memo table between parses: at 100,000 lines (1.9 MiB of
  text), a `Document` with its tree holds about 151 MiB
  ([Incremental Parsing Performance](docs/guide/incremental-performance.md)). Bound how many a program keeps open and
  how large they grow.
- **Time.** Use a context deadline. Cancellation is cooperative: it takes effect at the next point where the parser
  checks it, and a blocked reader or callback is not interrupted.

### Which Inputs Are Expected to Be Slow

PEGO does not promise linear time. Memoization bounds how often a rule body runs at one position, but some grammars do
quadratic, cubic or exponential work, and with such a grammar, input chosen by an attacker can reach the worst case.
The specification defines the classes of growth, with examples, and sufficient conditions for linear work
([Time and Memory](spec/runtime/parsing.md#time-and-memory)). No tool classifies a grammar yet, so set work limits
for any grammar that parses untrusted input.

### Parsing Untrusted Input

1. Set **both** work limits. `MaxSteps` alone does not bound a loop that only scans input; `MaxInputWork` does. A value
   that grows without reading input is bounded by `MaxSteps`, not by `MaxInputWork`. Generate parsers with
   `-work-controls`.
2. Set a context deadline.
3. Limit the input length before parsing, and for a stream, the length the reader delivers.
4. Keep the default nesting limit or lower it. For input that nests deeply, use the iterative VM instead of raising
   the limit.
5. Bound recovered errors in a stream with `pego.WithDiagnosticLimit`, and bound the number of open `Document`s.
6. Load compiled grammars only from sources you trust.

### Default Limits

- **Today.** Work limits are opt-in, and generated parsers carry them only when generated with `-work-controls`.
- **Planned for the first release.** `pego gen` generates parsers with work controls by default, as `-work-controls`
  does today. Every parse gets one fixed work budget proportional to the input length (c·n with a large constant), and
  an unlimited parse has to be requested explicitly. A lint warns about grammars outside the specification's
  sufficient conditions for linear work.
- **Later.** Budgets refined per grammar by a complexity analysis, and optional caps on input length and on estimated
  memory.

Until the planned defaults ship, the limits above are the caller's to set.
