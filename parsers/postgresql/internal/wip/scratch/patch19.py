W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(f).read()
    assert a in t, (f, a[:60])
    open(f, 'w').write(t.replace(a, b, 1))


sub(W + 'parts/10-lexical.pego', '''    !(("<=" / ">=" / "<>" / "=>") &plain_end) !("!=" !opc)''', '''    !(("<=" / ">=" / "<>" / "=>") &plain_end) !(("!=" / "%" / "^") !opc)''')
sub(W + 'parts/10-lexical.pego', '''    / x:@(hexinteger / octinteger / bininteger / decinteger) [len($x) <= 9] number_end !("." !".")''',
    '''    / &(int32_hex / int32_oct / int32_bin) (hexinteger / octinteger / bininteger) number_end
    / x:@decinteger [len($x) <= 9] number_end !("." !".")''')
sub(W + 'parts/10-lexical.pego', '''def Iconst: Iconst = t:iconst_text s -> $t''', '''// hexadecimal, octal and binary integers that fit in an int32 (the digits after the leading zeros are at
// most 7 hexadecimal digits, or 8 with a first digit of at most 7; 10 octal, or 11 starting with 1; 30
// binary, or 31 starting with 1)
def int32_hex = "0" (?xX) ( ("_"? "0")+ !("_"? (?0-9a-fA-F))
    / ("_"? "0")* "_"? (?1-9a-fA-F) ("_"? (?0-9a-fA-F)){0,6} !("_"? (?0-9a-fA-F))
    / ("_"? "0")* "_"? (?1-7) ("_"? (?0-9a-fA-F)){7} !("_"? (?0-9a-fA-F)) )
def int32_oct = "0" (?oO) ( ("_"? "0")+ !("_"? (?0-7))
    / ("_"? "0")* "_"? (?1-7) ("_"? (?0-7)){0,9} !("_"? (?0-7))
    / ("_"? "0")* "_"? "1" ("_"? (?0-7)){10} !("_"? (?0-7)) )
def int32_bin = "0" (?bB) ( ("_"? "0")+ !("_"? (?01))
    / ("_"? "0")* "_"? "1" ("_"? (?01)){0,30} !("_"? (?01)) )

def Iconst: Iconst = t:iconst_text s -> $t''')
sub(W + 'parts/10-lexical.pego', '''    / !iconst_text (hexinteger / octinteger / bininteger / decinteger) number_end !("." !".")''',
    '''    / !iconst_text (hexinteger / octinteger / bininteger / decinteger) number_end !("." !".")''')
