#!/bin/sh
# usage: ag-a-cases.sh [cmp args]   runs the cases of region A
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$W/ag-a
exec sh $W/pgcases.sh $D "$@"
