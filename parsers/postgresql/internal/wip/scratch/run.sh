#!/bin/sh
# usage: run.sh FILES [cmp args]   e.g. run.sh select.sql,join.sql -v 5
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
cd $R || exit 1
W=$R/.w
sh $W/build.sh > $W/build.out 2>&1
head -5 $W/build.out | grep -v "^built" | grep -v "postgresql.pego$"
go build -o $W/pgdev ./pgdev || exit 1
FILES=$1
shift
$W/pgdev -g parsers/postgresql/postgresql.pego -c $W/full.jsonl.gz -o $W/out.jsonl -files "$FILES" 2>&1 | grep -v "^compiled"
$R/.refsql/bin/python $W/cmp.py $W/full.jsonl.gz $W/out.jsonl "$@"
