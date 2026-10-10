# 43. Direct calls of plain rules in the VMs

- As in the closure backend (change 38) and generated parsers (change 42), the recursive VM calls rules that are
  never memoized in an ordinary parse and have no captures through `invokePlain` directly, and the iterative VM
  starts them with `plainCallFrame` without asking whether to memoize.
- Effect (min of 10 interleaved runs, Apple M3 Max): both VMs 0.5–2.5% faster on full parses and 1.4–4.6% on
  recognition (JSON bytecode 12.2 → 11.7 ms).
