#!/bin/sh
# usage: sh ag-b1-region.sh [cmp.py args]   (run pgrun.sh first: it writes out.jsonl)
# Compares the statements of region B1 of the corpus (ONLY is a regex over the statement text).
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
D=$W/ag-b1
ONLY='(?is)^\s*(create\s+(or\s+replace\s+)?(function|procedure|aggregate|trusted|procedural|language|extension|access\s+method|default\s+conversion|conversion|collation|type|operator|text\s+search|cast|transform)|alter\s+(function|procedure|routine|type|operator|text\s+search\s+(configuration|dictionary)|collation|extension)|drop\s+(cast|transform|operator\s+(class|family)))'
export ONLY
PGDIR=$D $R/.refsql/bin/python $D/ag-b1-cmp.py $W/full.jsonl.gz $D/out.jsonl "$@"
