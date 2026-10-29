# PEGO

PEGO is a Go parser framework using an extended PEG grammar language. Keep this
file concise; put contracts, design rationale, measurements and workflow detail
in the linked project documents.

At the start of every development session, use the `onboarding` skill in
`.agents/skills/onboarding/` to locate current sources and choose relevant checks.

## Documentation map

- `README.md`: project overview and documentation index.
- `spec/`: normative specifications: `language/` (grammar), `runtime/` (parser and runtime),
  `core-ir/` and `bytecode/`.
- `docs/tutorial/`, `docs/guide/`, `docs/cookbook/`: learning and usage.
- `docs/design/`: architecture and internal representations as implemented;
  `docs/design/records/`: decisions; `docs/optimizations/`: measured mechanisms.
- `docs/development.md`, `docs/site.md`, `docs/performance.md`: development,
  site maintenance and performance method/status.
- `site/`: Nanosite configuration and wrapper; `bench/`: benchmark sources.

Use the `documentation`, `performance`, `backlog` and `nanosite` skills when
relevant.
Keep reader-facing docs independent of chat history and backlog labels. Write
repository docs and comments in English. Update affected contracts and guides
with implementation changes; regenerate generated files.

## Development and checks

- Public APIs: `pego.go`, `grammar/`; parser/compiler: `internal/syntax/`,
  `internal/compile/`, `internal/engine/`; generated parsers: `parsers/`.
- Use Go 1.27.1. Run `go test -timeout=30m ./...` for the root module. After
  engine or generator changes, run `go generate ./parsers ./bench` and
  `parsers/test.sh`. For site changes, run `cd site && go test ./...` and the
  site build/check commands in `docs/site.md`.
- Keep generated benchmark reports/results unedited by hand and align
  `spec/bytecode/README.md` with VM changes.
- GitHub Actions are temporarily paused: do not trigger, rerun, cancel, poll or
  enable workflows. Keep 32-bit and ready-made parser work deferred.
  Required local validation still applies.
- Ask the owner about unspecified language semantics, with alternatives,
  tradeoffs and a recommendation; wait for answers before dependent work.
- Reuse the established Luna documentation/backlog sidekick and independent
  reviewer where appropriate. The primary owns implementation, tests, site
  code, Git operations and main integration; the documentation sidekick owns
  documentation context and Issue #1 management after handoff.

- For high-impact changes, request critical review from a reviewer whose context
  is independent of the worker; reuse existing reviewers. Share requirements,
  diffs and validation evidence, and resolve material findings before completion.

## Backlog, commits and branches

- Issue #1 is the living backlog: https://github.com/ornew/pego/issues/1.
  Categories are two-digit B, O, F, Q, I, D, plus M for ready-made parsers.
  Keep incomplete parent items; mark completion only after verified
  integration, in the recent-completions list with a commit link and outcome
  sentence. IDs belong in Issue #1, not reader-facing docs.
- Write commits, issues and PRs in the owner's voice: omit references to agents,
  AI assistance, the owner/user, requests, delegation or work history. Follow
  `docs/commit-messages.md`; include a body explaining the problem and rationale.
  Do not add agent signatures.
- Branch prefixes: `feat/`, `fix/`, `perf/`, `docs/`; do not use `codex/` or
  `claude/`. Standing permission allows pushes to main and work branches, but
  follow the current task's scope and authorization. Remove remote branches
  only after verifying integration.
