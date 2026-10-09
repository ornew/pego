#!/bin/sh
# usage: pgrun.sh DIR FILES [cmp.py args]
#   DIR    a working directory with parts/*.pego (the grammar is their concatenation, in name order) and an
#          optional ext/*.py (extensions of the tree converter)
#   FILES  comma separated scripts of the corpus, for example select.sql,join.sql, or "all"
#   CORPUS (environment) the corpus; default: the statements of the regression scripts of PostgreSQL 18
# Builds DIR/postgresql.pego (the keyword rules and the type Any are generated), parses the statements of
# the scripts with it through the engine, and compares acceptance and trees with the reference.
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$1
FILES=$2
shift
shift
C=${CORPUS:-$W/full.jsonl.gz}
cat $D/parts/*.pego > $D/postgresql.pego || exit 1
python3 $R/parsers/postgresql/internal/refgen/keywords.py update $D/postgresql.pego || exit 1
FARG="-files $FILES"
if [ "$FILES" = "all" ]; then FARG=""; fi
$W/pgdev -g $D/postgresql.pego -c $C -o $D/out.jsonl $FARG 2>&1 | grep -v "^compiled"
PGDIR=$D $R/.refsql/bin/python $W/cmp.py $C $D/out.jsonl "$@"
