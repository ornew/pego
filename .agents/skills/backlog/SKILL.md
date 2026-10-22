---
name: backlog
description: Maintain PEGO's Issue #1 backlog, its priority and progress comments, and recent completions.
---

# Backlog

- Use PEGO Issue #1: https://github.com/ornew/pego/issues/1. Retrieve current
  issue content and management comments before proposing or making updates.
- Categories use two-digit IDs: B bugs, O optimizations, F features, Q quality
  and tools, I improvements, D docs, and M ready-made parsers. Preserve existing
  IDs and never reuse one. A new ID is the category's entry in the body's
  last-used-ID line plus one; update that line when allocating. Features
  normally require a design record.
- Keep the issue body to its rules, the last used IDs, management links, the
  ten most recent completions and incomplete items. The commit log is the full
  record of completed work; there are no archive comments.
  Maintain priority/dependency and progress comments consistently; progress is
  current status, decisions and blockers, not a conversation log.
- Preserve incomplete parent scope. Complete an item only after acceptance gates
  pass and integration is verified. Then remove it from the open list, add its
  ID, linked commit and one outcome sentence to the top of the recent
  completions (dropping the oldest beyond ten), and synchronize the priority
  and progress comments without duplicates.
- IDs are for Issue #1 only. Never put backlog IDs in reader docs, commits or
  design/optimization records; link those documents descriptively instead.
- Fetch before edits and read back affected surfaces. Keep local checks, remote
  CI, integration and deployment evidence distinct. Do not trigger or poll
  Actions while the standing workflow pause applies.
- The documentation sidekick owns Issue #1 after handoff; the primary owns
  implementation, tests, site code and Git integration. The primary reviews
  changes and verifies their saved state.
