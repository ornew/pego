"""Extracts the examples (programlisting) and the synopsis of the reference pages of region B1 to cases/docs.sql."""
import re, html, glob, os
REF = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/suites/postgres/doc/src/sgml/ref/'
names = ['create_function', 'create_procedure', 'create_cast', 'create_transform', 'create_aggregate', 'create_operator',
         'create_opclass', 'create_opfamily', 'create_type', 'create_tsconfig', 'create_tsdictionary', 'create_tsparser',
         'create_tstemplate', 'create_collation', 'create_conversion', 'create_language', 'create_extension',
         'create_access_method', 'alter_function', 'alter_procedure', 'alter_routine', 'alter_type', 'alter_operator',
         'alter_opclass', 'alter_opfamily', 'alter_tsconfig', 'alter_tsdictionary', 'alter_collation', 'alter_extension',
         'drop_cast', 'drop_transform', 'drop_opclass', 'drop_opfamily', 'drop_function', 'drop_aggregate', 'drop_operator']
out = []
for n in names:
    f = REF + n + '.sgml'
    if not os.path.exists(f):
        print('missing', f)
        continue
    t = open(f).read()
    for m in re.finditer(r'<programlisting>(.*?)</programlisting>', t, flags=re.S):
        body = re.sub(r'<[^>]+>', '', m.group(1))
        body = html.unescape(body)
        out.append('-- ' + n)
        out.append(body.strip('\n'))
    syn = re.search(r'<synopsis>(.*?)</synopsis>', t, flags=re.S)
open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cases', 'docs.sql'), 'w').write('\n'.join(out) + '\n')
