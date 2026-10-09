"""Generates the lookahead expressions that tell whether an integer literal fits in an int32."""

# A digit with an optional underscore in front of it ("_" separates digits, never starts or ends a number).
SD = '("_"? (?0-9))'


def digit_class(lo, hi, base=10):
    digs = "0123456789abcdef"
    if lo == hi:
        return '"%s"' % digs[lo]
    return "(?%s-%s)" % (digs[lo], digs[hi])


def maxint_alts(max_digits, base, anyc):
    """alternatives for numbers of exactly len(max_digits) digits that are <= max_digits"""
    alts = []
    n = len(max_digits)
    for i in range(n):
        d = max_digits[i]
        # prefix equal to max_digits[:i], digit i less than max, rest any
        if d > 0:
            prefix = "".join('"_"? "%s" ' % "0123456789abcdef"[x] for x in max_digits[:i])
            lo_hi = "(?%s-%s)" % ("0", "0123456789abcdef"[d - 1]) if d - 1 > 0 else '"0"'
            rest = anyc * (n - i - 1)
            alts.append((prefix + '"_"? ' + lo_hi + (" " + " ".join([anyc] * (n - i - 1)) if n - i - 1 else "")).replace('"_"? "', '"', 0))
    # exactly equal
    alts.append(" ".join('"_"? "%s"' % "0123456789abcdef"[x] for x in max_digits))
    return alts


if __name__ == "__main__":
    pass
