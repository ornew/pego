# 44. Changes 36 and 37 in the VMs (instruction set 2)

- The VMs now compare `text(a) == text(b)` without boxing the strings and build no intermediate lists for a `concat`
  of `list`, `map` and `concat` calls, through six new expression instructions (`ETEXTCHK`, `ETEXTEQ`,
  `ELISTBEGIN`, `ELISTPUSH`, `EMAPPUSH`, `ELISTEND`; see [bytecode.md](../../spec/bytecode.md)). The instruction-set version
  of compiled files is now 2, and files of version 1 still load.
- Effect (min of 20 interleaved runs, Apple M3 Max, recursive VM, during a busy day): XML 19.2k → 1.2k allocations,
  JSON 22.0 → 20.5 MB and CSV 16.2 → 14.6 MB per parse; time within ±3% (CSV and XML 2% faster, JSON 3% slower).
