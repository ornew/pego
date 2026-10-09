#!/bin/sh
# usage: r.sh [pgrun options]: runs the C1 statements of the corpus and hides the rejections that stem from the
# stubs of other groups (the @@never@@ placeholders) and from the XML/JSON expressions
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
cd $W
CORPUS=ag-c1/c1corpus.jsonl.gz sh pgrun.sh ag-c1 all "$@" 2>&1 | grep -v 'never@@'
