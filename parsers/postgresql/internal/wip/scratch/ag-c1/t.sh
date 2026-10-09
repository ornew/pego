#!/bin/sh
# usage: t.sh [pgcases options]: runs the test cases of t/cases with the test stubs (working versions of the
# CREATE TABLE AS rules of group A, and surrogates of ALTER DATABASE ... SET and ALTER FUNCTION ... SET) instead
# of the stubs that never match
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
cd $W/ag-c1
rm -rf t/parts
mkdir -p t/parts
for f in parts/*.pego; do
  case $f in
    parts/99-stubs.pego) ;;
    *) cp $f t/parts/ ;;
  esac
done
cp t/stubs/99-teststubs.pego t/parts/
python3 t/patch.py
cd $W
sh pgcases.sh ag-c1/t "$@"
