# 33. Smaller VM entries

- Every choice and every repetition iteration pushes an entry onto the VM's entry stack. Entries were 112 bytes,
  mostly fields that only `#error` labels, `#recover` and skip entries use; in profiles, pushing them was the most
  expensive line of the VM loop. Those fields moved to a separate stack (`parser.labs`, indexed from the entry), and
  the stack heights in the saved state are `int32`: an entry is now 56 bytes.
- Effect (min of 6 interleaved runs, Apple M3 Max): both VMs 1–5% faster on full parses (JSON bytecode 18.8 →
  18.0 ms, XML 19.0 → 18.2 ms) and 2–6% on recognition, including the error-recovery workload.
