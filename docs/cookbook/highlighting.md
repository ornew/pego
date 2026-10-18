# Tokenize Source for Syntax Highlighting

**Problem.** You show code in a web page, a documentation tool or an editor widget, and want it highlighted. The text may be
broken (the user is typing), so the tokenizer must never fail, and every character must end up in some token.

Write a grammar of tokens, not of the language: a repetition of token rules, the last of which accepts any character.
The tree is a flat list, and the highlighter turns each token into a `<span>`.

```pego
// tokens.pego
type Comment = { Text string }
type Keyword = { Text string }
type Ident = { Text string }
type Number = { Text string }
type String = { Text string }
type Punct = { Text string }
type Space = { Text string }
type Unknown = { Text string }
type Token ~ Comment | Keyword | Ident | Number | String | Punct | Space | Unknown
type Tokens = { Items []Token }

// Every character belongs to a token, so tokenizing never fails, however broken the input is.
rule main Tokens = ts:token* $$ => Tokens{Items: $ts.Value}

rule token Token = comment / string / number / keyword / ident / punct / space / unknown

rule comment Comment = t:("--" [^\n]*) #atomic => Comment{Text: text($t)}
// A string that is not closed runs to the end of the line, as an editor would show it.
rule string String = t:("'" [^'\n]* "'"?) #atomic => String{Text: text($t)}
rule number Number = t:([0-9]+ ("." [0-9]+)?) #atomic => Number{Text: text($t)}
rule keyword Keyword = t:(("select" / "from" / "where" / "and" / "or" / "not" / "limit" / "order" / "by") !wordchar) #atomic => Keyword{Text: text($t)}
rule ident Ident = t:([a-z_] wordchar*) #atomic => Ident{Text: text($t)}
rule punct Punct = t:("<=" / ">=" / "<>" / [=<>+\-*/,;\(\)]) #atomic => Punct{Text: text($t)}
rule space Space = t:([ \t\r\n]+) #atomic => Space{Text: text($t)}
rule unknown Unknown = t:(.) #atomic => Unknown{Text: text($t)}
rule wordchar = [a-z0-9_]
```

```go
// main.go
package main

import (
	_ "embed"
	"fmt"
	"html"
	"log"
	"strings"

	"github.com/ornew/pego"
)

//go:embed tokens.pego
var grammar string

// class maps the type of a token to a CSS class.
var class = map[string]string{
	"Comment": "c", "Keyword": "k", "Number": "n", "String": "s", "Punct": "p", "Unknown": "err",
}

// Highlight returns src as HTML, with a <span> around each token that has a class.
func Highlight(p *pego.Parser, src string) (string, error) {
	tree, err := p.Parse(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, tok := range tree.Field("Items").(*pego.Node).Elems() {
		text := html.EscapeString(tok.Field("Text").(string))
		if c, ok := class[tok.Type()]; ok {
			fmt.Fprintf(&b, `<span class="%s">%s</span>`, c, text)
		} else {
			b.WriteString(text) // identifiers and white space
		}
	}
	return b.String(), nil
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	out, err := Highlight(p, "-- top customers\nselect name, age from users where age >= 30 and name = 'Ann';\nselect ~ 'oops <b>")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out)
}
```

```html
<span class="c">-- top customers</span>
<span class="k">select</span> name<span class="p">,</span> age <span class="k">from</span> users <span class="k">where</span> age <span class="p">&gt;=</span> <span class="n">30</span> <span class="k">and</span> name <span class="p">=</span> <span class="s">&#39;Ann&#39;</span><span class="p">;</span>
<span class="k">select</span> <span class="err">~</span> <span class="s">&#39;oops &lt;b&gt;</span>
```

## How It Works

- **Never fails.** `unknown` is `.`, which matches any character, so a character no other rule takes (`~` here) becomes
  an `Unknown` token and is shown as an error. Without it, one bad character would fail the whole parse.
- **Order is priority.** The choice tries `comment` and `string` first, so `--` and quotes win over anything inside them,
  `keyword` comes before `ident` so that `select` is a keyword, and `!wordchar` stops `selection` from being the keyword
  `select` followed by `ion`.
- **Broken input still highlights.** The string `'oops <b>` has no closing quote. `"'"?` makes the quote optional, so
  an unclosed string is a `String` up to the end of the line, which is also what most editors do, and the next line is
  highlighted normally.
- **Spans are text.** Every token is a text struct holding its text, and the tokens, joined, are exactly the input, so
  `strings.Join` of the texts gives the source back. `html.EscapeString` keeps `<` and `&` of the source from becoming
  markup.
- **Positions.** Each token has `Start()` and `End()`. For an editor, send those (they are code points by default; `pego.WithUnit`
  changes that) instead of HTML.

## Variations and Limits

- **A scope per token.** Replace the `class` map with the names your editor or theme uses, such as `keyword.sql`.
- **More precision.** Tokens that depend on what follows (a function name before `(`) are a lookahead away: an `ident`
  rule that ends with `&"("`, under its own type. Tokens that depend on the structure of the language need the real
  grammar, and the highlighter can then take the spans from its tree.
- **Case-insensitive keywords.** Write the characters as classes (`[sS][eE]...`) or normalize the text first.
- **Text that is edited.** Use a [`Document`](as-you-type.md), so that the tokens before and after an edit are reused.
