#!/bin/sh
# usage: sh ag-b1-t.sh RULE 'text' [LINES]   (G=other grammar file in ag-b1; default postgresql.pego)
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
$W/pego parse -g $W/ag-b1/${G:-postgresql.pego} -s "$1" -i "$2" -f sexpr 2>&1 | head -${3:-8}
