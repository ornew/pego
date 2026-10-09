"""usage: ag-a-pf.py FILE   prints the reference raw tree (without locations) of each statement of FILE (split at ';' at end of line)"""
import pglast, json, sys, re

def strip(x):
    if isinstance(x, list):
        return [strip(i) for i in x]
    if isinstance(x, dict):
        return {k: strip(v) for k, v in x.items() if 'location' not in k}
    return x

text = open(sys.argv[1]).read()
for s in re.split(r';\s*\n', text):
    s = s.strip()
    if not s:
        continue
    print(s)
    try:
        r = json.loads(pglast.parser.parse_sql_json(s))
        for st in r['stmts']:
            print(' ', json.dumps(strip(st['stmt']), separators=(',', ':')))
    except Exception as e:
        print('  ERROR', e)
