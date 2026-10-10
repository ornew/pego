# 38. Calling plain rules directly from closure call sites

- Rules that are never memoized in an ordinary parse and have no captures always end up in `invokePlain` (change 19),
  but every call went through `call`'s checks first. Such rules are now marked when the program is built
  (`rule.plain`), and the closure backend's call sites call `invokePlain` directly unless a `Document` parses (which
  memoizes them).
- Effect (min of 12 interleaved runs, Apple M3 Max, closure backend): full parses 0–3% faster, recognition 2–4% (JSON
  8.4 → 8.1 ms).
