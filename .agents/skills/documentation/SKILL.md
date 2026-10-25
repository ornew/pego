---
name: documentation
description: Maintain PEGO user documentation, contracts, design records and development guidance.
---

# Documentation

- User docs: `docs/tutorial/`, `docs/guide/`, and `docs/cookbook/` explain
  learning paths, features and runnable task recipes.
- Contracts: `spec/` defines normative grammar, parser and bytecode behavior.
  Keep tutorials and development history out of specifications.
- Decisions: `docs/design/records/` records problem, decision, alternatives and
  validation; distinguish proposed, accepted and implemented work.
- Development: `docs/development.md` and `docs/site.md` cover contribution and
  site workflows. `docs/performance.md` and `docs/optimizations/` hold method,
  applicability and evidence. `docs/benchmarks.md` is generated.
- Style: follow `docs/documentation-style.md` for page kinds, link directions,
  titles and headings, voice and terms, openings, and examples. The site tests
  enforce link directions and heading capitalization.
- Keep `AGENTS.md` concise. Follow existing indexes and templates, remove
  authoring prompts from finished pages, verify examples and links, and keep
  published docs independent of chats and Issue #1 IDs.
- The documentation sidekick owns documentation context and Issue #1 management
  after handoff. The primary owns site code, other implementation, tests and Git.
  The primary must review documentation changes and verify saved results.
