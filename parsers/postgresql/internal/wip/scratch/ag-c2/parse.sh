#!/bin/sh
# usage: parse.sh RULE 'text'   (the engine on my grammar)
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
exec $W/pego parse -g $W/ag-c2/postgresql.pego -s "$1" -i "$2" -f sexpr
