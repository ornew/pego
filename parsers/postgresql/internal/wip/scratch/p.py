import pglast, json, sys
for s in sys.argv[1:]:
    print(s)
    try:
        r = json.loads(pglast.parser.parse_sql_json(s))
        for st in r['stmts']:
            print(' ', json.dumps(st['stmt'], separators=(',', ':')))
    except Exception as e:
        print('  ERROR', e)
