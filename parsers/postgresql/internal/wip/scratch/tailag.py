import json, sys, os
D = '/Users/s27814/.claude/projects/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/subagents/'
for aid in sys.argv[1:]:
    last = None
    with open(D + 'agent-' + aid + '.jsonl') as f:
        for line in f:
            try:
                o = json.loads(line)
            except Exception:
                continue
            m = o.get('message', {})
            if m.get('role') == 'assistant':
                c = m.get('content')
                if isinstance(c, list):
                    for b in c:
                        if b.get('type') == 'text' and b['text'].strip():
                            last = (b['text'], m.get('stop_reason'))
    print('=====', aid)
    print((last[0][-1500:] if last else None), last[1] if last else '')
