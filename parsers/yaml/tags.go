package yaml

import (
	"net/netip"
	"strings"
)

// tagCharacters checks the encoded URI character set of YAML productions 39/40.
// The percent escapes are checked before shorthand decoding, so escaped Unicode
// and reserved characters retain their existing decoded application values.
func tagCharacters(s string, suffix bool) bool {
	if !uriCharacters(s, ":/?#@[]") {
		return false
	}
	return !suffix || !strings.ContainsAny(s, "!,[]{}")
}

// validTagURI checks RFC 3986's generic URI grammar, including its scheme.
// It does not impose HTTP-specific host/path rules or dereference the URI.
func validTagURI(s string) bool {
	i := strings.IndexByte(s, ':')
	if i <= 0 || !asciiLetter(s[0]) {
		return false
	}
	for _, c := range []byte(s[1:i]) {
		if !asciiLetter(c) && !(c >= '0' && c <= '9') && !strings.ContainsRune("+.-", rune(c)) {
			return false
		}
	}
	s = s[i+1:]
	if i := strings.IndexByte(s, '#'); i >= 0 {
		if !uriCharacters(s[i+1:], ":@/?") {
			return false
		}
		s = s[:i]
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		if !uriCharacters(s[i+1:], ":@/?") {
			return false
		}
		s = s[:i]
	}
	if strings.HasPrefix(s, "//") {
		authority, path, found := strings.Cut(s[2:], "/")
		return validTagAuthority(authority) && (!found || uriCharacters(path, ":@/"))
	}
	return uriCharacters(s, ":@/")
}

func validTagAuthority(s string) bool {
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		if !uriCharacters(s[:i], ":") {
			return false
		}
		s = s[i+1:]
	}
	var port string
	if strings.HasPrefix(s, "[") {
		i := strings.IndexByte(s, ']')
		if i < 0 || !validTagIPLiteral(s[1:i]) {
			return false
		}
		rest := s[i+1:]
		if rest != "" {
			if rest[0] != ':' {
				return false
			}
			port = rest[1:]
		}
	} else {
		host := s
		if i := strings.IndexByte(s, ':'); i >= 0 {
			host, port = s[:i], s[i+1:]
		}
		if !uriCharacters(host, "") {
			return false
		}
	}
	for i := range port {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	return true
}

func validTagIPLiteral(s string) bool {
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		version, address, ok := strings.Cut(s[1:], ".")
		if !ok || version == "" || address == "" || strings.Contains(address, "%") {
			return false
		}
		for i := range version {
			if !hexDigit(version[i]) {
				return false
			}
		}
		return uriCharacters(address, ":")
	}
	address, err := netip.ParseAddr(s)
	return err == nil && address.Is6() && address.Zone() == ""
}

// uriCharacters accepts unreserved, sub-delims, percent escapes and the supplied
// component-specific delimiters. It deliberately checks encoded bytes, not a
// decoded tag string: %20, %25 and UTF-8 escapes are legal URI data.
func uriCharacters(s, extra string) bool {
	allowed := "-._~!$&'()*+,;=" + extra
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' {
			if i+2 >= len(s) || !hexDigit(s[i+1]) || !hexDigit(s[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if !asciiLetter(c) && !(c >= '0' && c <= '9') &&
			!strings.ContainsRune(allowed, rune(c)) {
			return false
		}
	}
	return true
}

func asciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func hexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
