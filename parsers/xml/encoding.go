package xml

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Transcode returns the text of a document given as bytes, in UTF-8 and without a byte order mark. It
// detects the encoding as Appendix F of XML 1.0 describes, from the byte order mark or the first bytes
// and the encoding declaration, and supports UTF-8, UTF-16 (big and little endian, with or without a
// byte order mark), ISO-8859-1 and US-ASCII. Other encodings are an error, as is a declaration that
// names another encoding than the one the bytes are in. It does not check that UTF-8 is valid; Decode
// does. Declaration detection has no fixed byte-prefix limit.
func Transcode(data []byte) (string, error) {
	var (
		big, little bool // UTF-16
		bom         bool
	)
	switch {
	case bytes.HasPrefix(data, []byte("\xEF\xBB\xBF")):
		data, bom = data[3:], true
	case bytes.HasPrefix(data, []byte("\x00\x00\xFE\xFF")), bytes.HasPrefix(data, []byte("\xFF\xFE\x00\x00")),
		bytes.HasPrefix(data, []byte("\x00\x00\x00\x3C")), bytes.HasPrefix(data, []byte("\x3C\x00\x00\x00")),
		bytes.HasPrefix(data, []byte("\x00\x00\x3C\x00")), bytes.HasPrefix(data, []byte("\x00\x3C\x00\x00")):
		return "", encodingError("unsupported encoding: UCS-4")
	case bytes.HasPrefix(data, []byte("\xFE\xFF")):
		data, big, bom = data[2:], true, true
	case bytes.HasPrefix(data, []byte("\xFF\xFE")):
		data, little, bom = data[2:], true, true
	case bytes.HasPrefix(data, []byte("\x00\x3C\x00\x3F")):
		big = true
	case bytes.HasPrefix(data, []byte("\x3C\x00\x3F\x00")):
		little = true
	case bytes.HasPrefix(data, []byte("\x4C\x6F\xA7\x94")):
		return "", encodingError("unsupported encoding: EBCDIC")
	}
	if big || little {
		if len(data)%2 != 0 {
			return "", encodingError("invalid UTF-16: odd number of bytes")
		}
		u := make([]uint16, len(data)/2)
		for i := range u {
			if big {
				u[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
			} else {
				u[i] = uint16(data[2*i+1])<<8 | uint16(data[2*i])
			}
		}
		var b strings.Builder
		b.Grow(len(u))
		for i := 0; i < len(u); i++ {
			r := rune(u[i])
			if utf16.IsSurrogate(r) {
				if i+1 >= len(u) {
					return "", encodingError("invalid UTF-16: unpaired surrogate")
				}
				r = utf16.DecodeRune(r, rune(u[i+1]))
				if r == utf8.RuneError {
					return "", encodingError("invalid UTF-16: unpaired surrogate")
				}
				i++
			}
			b.WriteRune(r)
		}
		s := b.String()
		switch enc := strings.ToUpper(declaredEncoding(s)); enc {
		case "", "UTF-16", "ISO-10646-UCS-2", "UCS-2", "CSUNICODE":
		case "UTF-16BE":
			if !big {
				return "", encodingError("the encoding declaration says UTF-16BE, but the document is in UTF-16LE")
			}
		case "UTF-16LE":
			if !little {
				return "", encodingError("the encoding declaration says UTF-16LE, but the document is in UTF-16BE")
			}
		default:
			return "", encodingError(fmt.Sprintf("the encoding declaration says %s, but the document is in UTF-16", enc))
		}
		return s, nil
	}
	s := string(data)
	switch enc := strings.ToUpper(declaredEncoding(s)); enc {
	case "", "UTF-8", "UTF8":
		return s, nil
	case "UTF-16", "UTF-16BE", "UTF-16LE", "ISO-10646-UCS-2", "UCS-2", "CSUNICODE":
		return "", encodingError(fmt.Sprintf("the encoding declaration says %s, but the document is not in UTF-16", enc))
	case "US-ASCII", "ASCII", "ANSI_X3.4-1968", "ISO646-US", "CSASCII":
		if bom {
			return "", encodingError("the encoding declaration says " + enc + ", but the document has a UTF-8 byte order mark")
		}
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return "", encodingError(fmt.Sprintf("byte %#x at %d is not US-ASCII", s[i], i))
			}
		}
		return s, nil
	case "ISO-8859-1", "ISO_8859-1", "LATIN1", "L1", "IBM819", "CP819", "CSISOLATIN1", "ISO-IR-100", "ISO_8859-1:1987":
		if bom {
			return "", encodingError("the encoding declaration says " + enc + ", but the document has a UTF-8 byte order mark")
		}
		var b strings.Builder
		b.Grow(len(data) + len(data)/8)
		for _, c := range data {
			b.WriteRune(rune(c))
		}
		return b.String(), nil
	default:
		return "", encodingError("unsupported encoding: " + enc)
	}
}

var encodingDecl = regexp.MustCompile(`^<\?xml[ \t\r\n]+version[ \t\r\n]*=[ \t\r\n]*("[^"]*"|'[^']*')[ \t\r\n]+encoding[ \t\r\n]*=[ \t\r\n]*("[^"]*"|'[^']*')`)

// declaredEncoding returns the encoding of the XML declaration at the start of s, or "".
func declaredEncoding(s string) string {
	if !strings.HasPrefix(s, "<?xml") {
		return ""
	}
	// A declaration can contain arbitrarily long whitespace before encoding.
	// Keep the search inside its terminator, rather than a fixed byte prefix.
	if end := strings.Index(s, "?>"); end >= 0 {
		s = s[:end]
	}
	m := encodingDecl.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[2][1 : len(m[2])-1]
}

func encodingError(msg string) error { return &WFError{Line: 1, Col: 1, Msg: msg} }
