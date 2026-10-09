p = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/cmp.py'
t = open(p).read()
t = t.replace("""    if t == 'Seq' or t == 'Match' or t == 'Operator' or t == 'Error':""", """    if t == 'RangeFuncItem':
        cd = fields.get('Coldeflist')
        items = [conv_node(fields['Func'], True)]
        if cd and cd.get('children'):
            items.append({'List': {'items': [conv_node(c, True) for c in cd['children']]}})
        else:
            items.append({})
        return {'List': {'items': items}}
    if t == 'Seq' or t == 'Match' or t == 'Operator' or t == 'Error':""")
t = t.replace("""        out[rf] = conv_field(raw, rf, gf, v)
    # enum defaults""", """        if v == '':
            continue
        out[rf] = conv_field(raw, rf, gf, v)
    if raw == 'SQLValueFunction' and 'typmod' not in out:
        out['typmod'] = -1
    if raw == 'MergeSupportFunc':
        out['msftype'] = 25
    # enum defaults""")
t = t.replace("""        if v.get('type') == 'Iconst' and schema.get(tname, {}).get(rf, '') in ('int', 'int | None'):""", """        if v.get('type') == 'Iconst' and schema.get(tname, {}).get(rf, '') in ('int', 'int | None'):""")
open(p, 'w').write(t)
