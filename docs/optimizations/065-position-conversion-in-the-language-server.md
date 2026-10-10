# 65. Position conversion in the language server

- `pego lsp` converted between LSP positions (UTF-16 code units) and PEGO positions (code points) by walking each line
  from its start for every token, diagnostic and range: quadratic on long lines (a 64 KB line took 0.7 s to analyze and
  1.25 s for semantic tokens; an 800 KB line did not open within 10 s). The text index now keeps a checkpoint every 64
  bytes with the cumulative counts of UTF-16 units and code points, so a conversion walks at most 64 bytes.
- Effect: a 1 MB single-line grammar (260,000 tokens) is analyzed and tokenized in 0.35 s instead of over 10 s;
  `BenchmarkAnalyze` (go.pego and python.pego) unchanged at about 11 ms.
