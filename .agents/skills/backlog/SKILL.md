---
name: backlog
description: Maintain PEGO's selected Issue #1 backlog and verified completion records.
---

# Backlog

- Use PEGO Issue #1: https://github.com/ornew/pego/issues/1. Retrieve current
  issue content and management comments before proposing or making updates.
- Categories use two-digit IDs: B bugs, O optimizations, F features, Q quality
  and tools, I improvements, D docs, and M ready-made parsers. Preserve existing
  IDs; allocate new ones only after checking open items, archives and reservations.
  Features normally require a design record.
- Keep the issue body to its rules, incomplete items and management links.
  Maintain priority/dependency and progress comments consistently; progress is
  current status, decisions and blockers, not a conversation log.
- Preserve incomplete parent scope. Complete an item only after acceptance gates
  pass and integration is verified. Then archive its ID, linked commit and one
  outcome sentence, and synchronize open items and plans without duplicates.
- IDs are for Issue #1 only. Never put backlog IDs in reader docs, commits or
  design/optimization records; link those documents descriptively instead.
- Fetch before edits and read back affected surfaces. Keep local checks, remote
  CI, integration and deployment evidence distinct. Do not trigger or poll
  Actions while the standing workflow pause applies.
- The documentation sidekick owns Issue #1 after handoff; the primary owns
  implementation, tests, site code and Git integration. The primary reviews
  changes and verifies their saved state.
