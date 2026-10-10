# 17. ASCII bitmaps for character classes in the VMs

- `CLASS` and `SCAN` tested a character against a class by looping over its ranges (`Class.has`). The VM now
  precomputes, per class, a 128-bit bitmap of the characters below 128 (negation included) when the program is
  loaded, and tests ASCII characters with one bit lookup; other characters still use the ranges.
- The same idea in the closure engine measured no gain (see the experiments table); in the VMs, where the class is
  reached through the module's class table, it is a small but consistent gain.
- Effect (min of 8 interleaved runs, Apple M3 Max): recognition 1.3–4.6% faster on JSON, CSV, XML and minilang for both
  VMs (e.g. CSV bytecode 8.81 → 8.40 ms, XML 22.3 → 21.4 ms); full parses −1 to −4% (within noise on some).
