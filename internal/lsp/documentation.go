package lsp

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// documentation negotiates the formats explicitly advertised by the client.
// Clients without format support receive plaintext, as required by LSP.
func documentation(text string, formats []string) markupContent {
	for _, format := range formats {
		switch format {
		case "markdown":
			return markupContent{Kind: "markdown", Value: text}
		case "plaintext":
			return markupContent{Kind: "plaintext", Value: plainDocumentation(text)}
		}
	}
	return markupContent{Kind: "plaintext", Value: plainDocumentation(text)}
}

// plainDocumentation renders the markup emitted by PEGO's documentation builders.
// Code content and punctuation in code blocks remain unchanged.
func plainDocumentation(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	fenced := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			parts := strings.Split(line, "`")
			for i := range parts {
				if i%2 == 0 {
					parts[i] = plainMarkup(parts[i])
				}
			}
			line = strings.Join(parts, "")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var documentationLinks = regexp.MustCompile(`\[([^]]+)\]\(([^)]+)\)`)
var documentationBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var documentationUnderscoreBold = regexp.MustCompile(`__(.+?)__`)
var documentationEmphasis = regexp.MustCompile(`\*([^*]+)\*`)
var documentationUnderscoreEmphasis = regexp.MustCompile(`_([^_]+)_`)
var documentationHeading = regexp.MustCompile(`^#{1,6}\s+`)

func plainMarkup(text string) string {
	text = documentationLinks.ReplaceAllString(text, "$1 ($2)")
	text = documentationBold.ReplaceAllString(text, "$1")
	text = stripUnderscoreMarkup(text, documentationUnderscoreBold)
	text = documentationEmphasis.ReplaceAllString(text, "$1")
	text = stripUnderscoreMarkup(text, documentationUnderscoreEmphasis)
	return documentationHeading.ReplaceAllString(text, "")
}

// Underscore emphasis does not apply inside words. Keep source identifiers such
// as start_pos_name intact, including Unicode identifiers and double underscores.
func stripUnderscoreMarkup(text string, pattern *regexp.Regexp) string {
	var out strings.Builder
	last := 0
	for _, match := range pattern.FindAllStringSubmatchIndex(text, -1) {
		start, end, contentStart, contentEnd := match[0], match[1], match[2], match[3]
		before, after := rune(0), rune(0)
		if start > 0 {
			before, _ = utf8.DecodeLastRuneInString(text[:start])
		}
		if end < len(text) {
			after, _ = utf8.DecodeRuneInString(text[end:])
		}
		first, _ := utf8.DecodeRuneInString(text[contentStart:contentEnd])
		final, _ := utf8.DecodeLastRuneInString(text[contentStart:contentEnd])
		word := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) }
		if word(before) || word(after) || unicode.IsSpace(first) || unicode.IsSpace(final) {
			continue
		}
		out.WriteString(text[last:start])
		out.WriteString(text[contentStart:contentEnd])
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}
