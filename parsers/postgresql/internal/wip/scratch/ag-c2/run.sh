#!/bin/sh
# usage: run.sh FILES [args]   (pgrun on my directory)
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
exec sh $W/pgrun.sh $W/ag-c2 "$@"
