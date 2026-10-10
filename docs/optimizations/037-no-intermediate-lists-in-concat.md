# 37. No intermediate lists in `concat`

- `concat(list($first), map($rest, (r) => $r.x))`, the usual way to build a list from a first element and the rest,
  built two lists only for `concat` to copy their elements: about 19k list nodes per JSON parse. When an argument of
  `concat` is itself a call of `list`, `map` or `concat`, the closure backend now pushes that call's elements
  directly onto the stack `concat` collects on, and the code generator emits the same pushes. This is done only when
  every argument is such a call (the common case): with any other argument, `concat` checks it only after evaluating
  all of them, and fusing would report a different error when two arguments fail. The VMs still build the
  intermediate lists (fusing them there would take new instructions).
- Effect (min of 12 interleaved runs, Apple M3 Max): JSON 14.4 → 14.1 ms (closure), 10.0 → 9.8 ms (generated), 21.4 →
  20.0 MB per parse; CSV 6.6 → 6.2 ms (closure), 4.8 → 4.5 ms (generated), 15.6 → 14.0 MB; minilang unchanged within
  noise.
