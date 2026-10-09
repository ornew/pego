#!/bin/sh
# builds D/postgresql.pego from the parts and lints it (prints the errors of the grammar)
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$W/ag-a
cat $D/parts/*.pego > $D/postgresql.pego || exit 1
python3 $R/parsers/postgresql/internal/refgen/keywords.py update $D/postgresql.pego || exit 1
if [ "$1" = "size" ]; then
  $W/pego gen -g $D/postgresql.pego -pkg postgresql -types -recognize -nodoc -o $D/parser.go
  wc -l $D/parser.go
  rm -f $D/parser.go
else
  $W/pego lint -g $D/postgresql.pego 2>&1 | head -${LINES_MAX:-60}
fi
