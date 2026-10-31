---
name: performance
description: Maintain ZeroParse's optimization method and reproducible performance evidence.
---

# Performance Evidence

- Follow `docs/performance.md` and use `docs/optimizations/000-template.md` for
  focused mechanism records. State operation, workload, backend, position unit,
  toolchain, configuration and hardware.
- Correctness comes before timing. Pin revisions and inputs; serialize runs and
  use paired or alternating baseline/candidate samples. Report noise and
  unmeasured paths; full snapshots describe combined performance, focused
  measurements support attribution.
- Keep focused raw logs outside Git unless deliberately versioned as fixtures.
  Preserve provenance and update the optimization index with applicability,
  evidence and measured tradeoffs. Separate plans, local results and production
  observations; avoid incomparable claims.
- Run optimization and timing work only under current owner authorization.
  Serialize benchmark runs and follow `docs/performance.md`; keep claims limited
  to measured paths and retain raw evidence outside Git unless deliberately
  versioned.
