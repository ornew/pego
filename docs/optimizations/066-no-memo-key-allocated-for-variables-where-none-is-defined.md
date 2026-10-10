# 66. No memo key allocated for variables where none is defined

- The memo key of a call of a rule that reads variables holds their current values, and `envValues` allocated a
  slice for it on every memoized call. The set of variables a rule reads is transitive, so one variable used deep in
  a grammar (`parsers/cue` tracks the hashes of a raw string in `sv`) puts it on most rules, while in most calls no
  variable is defined at all. Where none is, the key is now a shared slice of nils (memo entries only compare keys).
- Effect (`parsers/cue` `ParseAST`, mean of 6 runs, Apple M3 Max): config 25.2 → 23.4 ms (−7%), 128,458 → 3,006
  allocations, 8.9 → 6.8 MB; corpus 18.3 → 17.2 ms (−6%), 99,588 → 2,045 allocations. The engine's outline workload
  is unchanged within noise (its rules that read variables run where one is defined).
