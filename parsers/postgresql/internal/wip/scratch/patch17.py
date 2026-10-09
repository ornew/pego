p = '/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/parsers/postgresql/decode.go'
t = open(p).read()
a = t.index("\t\tcase (ch == 'u' || ch == 'U') && i+2 < len(t) && t[i+1] == '&' && t[i+2] == '\\'':")
b = t.index("\t\tcase ch == '$':")
new = '''\t\tcase (ch == 'u' || ch == 'U') && i+2 < len(t) && t[i+1] == '&' && t[i+2] == '\\'':
\t\t\t// the parts of a U& string, then an optional UESCAPE 'c'
\t\t\tj := i + 2
\t\t\tesc := byte('\\\\')
\t\t\tvar sb strings.Builder
\t\t\tfor {
\t\t\t\tk, s := readQuoted(t, j+1)
\t\t\t\tsb.WriteString(s)
\t\t\t\tj = skipWhite(t, k)
\t\t\t\tif j < len(t) && t[j] == '\\'' {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tif j+7 <= len(t) && strings.EqualFold(t[j:j+7], "uescape") {
\t\t\t\t\tj = skipWhite(t, j+7)
\t\t\t\t\tesc = t[j+1]
\t\t\t\t\tj += 3
\t\t\t\t}
\t\t\t\tbreak
\t\t\t}
\t\t\tdec, err := unicodeEscapes(sb.String(), esc)
\t\t\tif err != nil {
\t\t\t\treturn "", err
\t\t\t}
\t\t\tout.WriteString(dec)
\t\t\ti = j
'''
t = t[:a] + new + t[b:]
# remove nextPartStarts and skipToQuote
a = t.index('// nextPartStarts tells')
b = t.index('// readQuoted reads a standard')
t = t[:a] + '''// skipWhite skips white space and -- comments.
func skipWhite(t string, j int) int {
	for j < len(t) {
		switch {
		case t[j] == ' ' || t[j] == '\\t' || t[j] == '\\n' || t[j] == '\\r' || t[j] == '\\f' || t[j] == '\\v':
			j++
		case strings.HasPrefix(t[j:], "--"):
			for j < len(t) && t[j] != '\\n' && t[j] != '\\r' {
				j++
			}
		default:
			return j
		}
	}
	return j
}

''' + t[b:]
# splitUescape is used by Ident only: simplify it
a = t.index('// splitUescape separates')
b = t.index('func indexFold')
t = t[:a] + '''// splitUescape separates the quoted body of a U&"..." identifier from a following UESCAPE 'c'. The escape
// character is '\\' by default. text starts at the opening quote.
func splitUescape(text string) (body string, esc byte) {
	esc = '\\\\'
	i := 1
	for i < len(text) {
		if text[i] == '"' {
			if i+1 < len(text) && text[i+1] == '"' {
				i += 2
				continue
			}
			break
		}
		i++
	}
	body = text[:i+1]
	rest := skipWhite(text, i+1)
	if rest+7 <= len(text) && strings.EqualFold(text[rest:rest+7], "uescape") {
		r := skipWhite(text, rest+7)
		if r+1 < len(text) {
			esc = text[r+1]
		}
	}
	return body, esc
}

''' + t[b:]
a = t.index('func indexFold')
b = t.index('// unicodeEscapes decodes')
t = t[:a] + t[b:]
open(p, 'w').write(t)
