#!/bin/sh
# the generated size of the core alone (main/postgresql.pego is regenerated from its parts)
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
T=$W/ag-a/size-main.pego
cat $W/main/parts/*.pego > $T || exit 1
python3 $R/parsers/postgresql/internal/refgen/keywords.py update $T || exit 1
$W/pego gen -g $T -pkg postgresql -types -recognize -nodoc -o $W/ag-a/size-main.go
wc -l $W/ag-a/size-main.go
rm -f $T $W/ag-a/size-main.go
