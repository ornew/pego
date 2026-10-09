#!/bin/sh
# usage: ag-a-region.sh [cmp args]   runs the whole corpus with the grammar of D, and compares only the statements of region A
# (it uses the out.jsonl that pgrun.sh wrote: run ag-a-run.sh first, or pass -run)
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$W/ag-a
if [ "$1" = "-run" ]; then
  shift
  cat $D/parts/*.pego > $D/postgresql.pego || exit 1
  python3 $R/parsers/postgresql/internal/refgen/keywords.py update $D/postgresql.pego || exit 1
  $W/pgdev -g $D/postgresql.pego -c $W/full.jsonl.gz -o $D/out.jsonl 2>&1 | grep -v "^compiled"
fi
python3 $D/ag-a-mkcmp.py || exit 1
exec $R/.refsql/bin/python $D/ag-a-cmp.py $W/full.jsonl.gz $D/out.jsonl -only $D/ag-a-region.txt "$@"
