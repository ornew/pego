import re, sys

src = open(sys.argv[1]).read()
parts = src.split('\n%%\n')
gram = parts[1]
i = 0
n = len(gram)
depth = 0
buf = []
while i < n:
    c = gram[i]
    if depth == 0:
        if gram.startswith('/*', i):
            i = gram.index('*/', i) + 2
            continue
        if c == "'":
            j = gram.index("'", i + 1)
            while gram[j - 1] == '\\' and gram[j - 2] != '\\':
                j = gram.index("'", j + 1)
            buf.append(gram[i:j + 1])
            i = j + 1
            continue
        if c == '{':
            depth = 1
            i += 1
            continue
        buf.append(c)
        i += 1
    else:
        if gram.startswith('/*', i):
            i = gram.index('*/', i) + 2
            continue
        if gram.startswith('//', i):
            i = gram.index('\n', i)
            continue
        if c == '"':
            j = i + 1
            while gram[j] != '"':
                if gram[j] == '\\':
                    j += 1
                j += 1
            i = j + 1
            continue
        if c == "'":
            j = i + 1
            while gram[j] != "'":
                if gram[j] == '\\':
                    j += 1
                j += 1
            i = j + 1
            continue
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
        i += 1
text = ''.join(buf)
text = re.sub(r'[ \t]+\n', '\n', text)
text = re.sub(r'\n{2,}', '\n', text)
print(text)
