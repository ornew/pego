package gentest

const Records = `rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n"`
