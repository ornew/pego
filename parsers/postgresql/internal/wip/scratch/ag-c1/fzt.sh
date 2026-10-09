#!/bin/sh
# usage: fzt.sh SEED COUNT [pgcases options]: like fz.sh with the test stubs of t/ (run t.sh first)
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
SEED=$1
COUNT=$2
shift
shift
cd $W/ag-c1
rm -rf fz/parts
mkdir -p fz
cp -r t/parts fz/parts
ln -sfn ../ext fz/ext
FZT=1 /Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/.refsql/bin/python fuzz.py $SEED $COUNT
cd $W
sh pgcases.sh ag-c1/fz "$@"
