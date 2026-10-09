#!/bin/sh
# usage: ag-a-run.sh [pgrun args after the files]   runs the corpus (FILES=all unless $FILES) for region A
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$W/ag-a
exec sh $W/pgrun.sh $D ${FILES:-all} "$@"
