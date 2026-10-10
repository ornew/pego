# 51. First-character dispatch in the VMs (instruction set 3)

- Changes 49 and 50 in the VMs: a new instruction, `GUARD c, d, l`, is emitted before the `CHOICE` of an
  alternative that must begin with a given terminal (the analysis, `firstTerminal`, is shared by every backend). The
  instruction-set version of compiled files is now 3; files of versions 1 and 2 still load (they simply have no
  guards).
- Effect (min of 10 interleaved runs, Apple M3 Max): JSON 18.5 → 16.1 ms (recursive VM, −13%) and 22.4 → 19.0 ms
  (iterative VM, −15%); Minilang −8% on both; Arith_Pratt −4% and −7%; XML −1% and −7%.
