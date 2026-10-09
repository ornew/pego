#!/bin/sh
# Rebuilds D/chk: a copy of D with the core changes proposed by group D applied (see ag-d-corefixes.py), runs its cases.
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$W/ag-d
rm -rf $D/chk
mkdir -p $D/chk/parts $D/chk/ext $D/chk/cases
cp $D/parts/*.pego $D/chk/parts/
cp $D/ext/*.py $D/chk/ext/
cp $D/cases/*.sql $D/chk/cases/
python3 $D/ag-d-corefixes.py $D/chk || exit 1
cd $W
sh pgcases.sh $D/chk "$@"
