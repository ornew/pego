package lsp

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// documentation negotiates the formats explicitly advertised by the client.
// Clients without format support receive plaintext, as required by LSP.
func documentationControlled(text string, formats []string, control *renderControl) markupContent {
	control.poll()
	for _, format := range formats {
		control.poll()
		switch format {
		case "markdown":
			return markupContent{Kind: "markdown", Value: text}
		case "plaintext":
			return markupContent{Kind: "plaintext", Value: plainDocumentationControlled(text, control)}
		}
	}
	return markupContent{Kind: "plaintext", Value: plainDocumentationControlled(text, control)}
}

// plainDocumentation renders the markup emitted by PEGO's documentation builders.
// Code content and punctuation in code blocks remain unchanged.
func plainDocumentationControlled(text string, control *renderControl) string {
	control.poll()
	lines := control.split(text, "\n")
	var out []string
	fenced := false
	for _, line := range lines {
		control.poll()
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			parts := control.split(line, "`")
			for i := range parts {
				control.poll()
				if i%2 == 0 {
					parts[i] = plainMarkupControlled(parts[i], control)
				}
			}
			line = control.join(parts, "")
		}
		out = append(out, line)
	}
	return control.join(out, "\n")
}

var documentationLinks = regexp.MustCompile(`\[([^]]+)\]\(([^)]+)\)`)
var documentationBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var documentationUnderscoreBold = regexp.MustCompile(`__(.+?)__`)
var documentationEmphasis = regexp.MustCompile(`\*([^*]+)\*`)
var documentationUnderscoreEmphasis = regexp.MustCompile(`_([^_]+)_`)
var documentationHeading = regexp.MustCompile(`^#{1,6}\s+`)

func plainMarkupControlled(text string, control *renderControl) string {
	control.poll()
	text = control.replaceDocumentation(documentationLinks, text, "$1 ($2)")
	text = control.replaceDocumentation(documentationBold, text, "$1")
	text = stripUnderscoreMarkupControlled(text, documentationUnderscoreBold, control)
	text = control.replaceDocumentation(documentationEmphasis, text, "$1")
	text = stripUnderscoreMarkupControlled(text, documentationUnderscoreEmphasis, control)
	return control.replaceDocumentation(documentationHeading, text, "")
}

// Underscore emphasis does not apply inside words. Keep source identifiers such
// as start_pos_name intact, including Unicode identifiers and double underscores.
func stripUnderscoreMarkupControlled(text string, pattern *regexp.Regexp, control *renderControl) string {
	control.poll()
	var out strings.Builder
	last := 0
	for _, match := range control.documentationMatches(pattern, text) {
		control.poll()
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
		control.writeString(&out, text[last:start])
		control.writeString(&out, text[contentStart:contentEnd])
		last = end
	}
	control.writeString(&out, text[last:])
	return out.String()
}
