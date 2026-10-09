#!/bin/sh
# usage: pgcases.sh DIR [cmp.py args]
# Writes the reference results for the statements of DIR/cases/*.sql (psql scripts: statements separated by
# semicolons) to DIR/cases.jsonl.gz and compares the grammar of DIR with them (all statements).
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$1
shift
$R/.refsql/bin/python $R/parsers/postgresql/internal/refgen/gen.py -sql $D/cases -o $D/cases.jsonl.gz || exit 1
CORPUS=$D/cases.jsonl.gz sh $W/pgrun.sh $D all "$@"
