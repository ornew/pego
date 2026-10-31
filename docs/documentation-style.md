# Documentation Style

How the ZeroParse documentation is organized and written. It applies to the tutorial, the guides, the cookbook and the
specification; design pages, design records and optimization records follow their own rules. Pages that break a rule that a test
checks fail `cd site && go test ./...`; the rest is checked in review.

## Kinds of Pages

Every page has one job, and a reader should be able to tell which from the directory.

| Kind | Directory | Job | Template |
|:--|:--|:--|:--|
| Tutorial | `docs/tutorial/` | Teaches the language and the tools in order, one lesson at a time | [TEMPLATE.md](tutorial/TEMPLATE.md) |
| Guide | `docs/guide/` | Explains one feature in depth, for a reader who has a use for it | [TEMPLATE.md](guide/TEMPLATE.md) |
| Cookbook | `docs/cookbook/` | Solves one concrete task with complete, runnable files | [TEMPLATE.md](cookbook/TEMPLATE.md) |
| Specification | `spec/` | Defines the behavior that implementations must have | [TEMPLATE.md](../spec/TEMPLATE.md) |
| Design overview | `docs/design/` | Explains the stable base design: stages, representations, invariants and the reasons for them. It holds what the code does not show or shows only with difficulty, and leaves field lists, file inventories and per-function detail to the code and its package comments; a fact that changes often does not belong. A precise list is replaced by a pointer to the package | None; a page opens with what it explains |
| Design record | `docs/design/records/` | Records a decision, its alternatives and its validation | [000-template.md](design/records/000-template.md) |
| Optimization record | `docs/optimizations/` | Records a measured mechanism | [000-template.md](optimizations/000-template.md) |

A page that does two jobs is two pages. A guide that grows a section a reader would look for on its own, or that
mixes several independent options of a command, is split; a recipe that explains a feature points to the guide instead.

### Links

Links point from the page that depends on another to the page that it depends on, so the specification can be read
alone, a guide can be read with the specification, and so on. `TestDocumentLinkDirections` enforces this.

| A page of this kind | May link to |
|:--|:--|
| Specification | The specification, and standards (`rfc-editor.org`) |
| Guide | The specification, guides, design and optimization records, `docs/performance.md`, `docs/benchmarks.md` and code |
| Cookbook | The specification, guides, other recipes and code |
| Tutorial | Anything |

Guides and recipes do not link to project documents (the other files of `docs/` and the Markdown files of the
repository root) or to the tutorial. No guide links to the cookbook, and no recipe links to design records or
performance documents. A link to a section names its heading, so a renamed heading breaks the link, and the site check
reports it. Keep headings stable, and when a page is split or renamed, update the links in the same commit.

### Indexes

Each directory with an index (`README.md`) lists every page of the directory in the order of `site/config.json`,
grouped the same way. Add a page to both in the same commit. The label of an index entry is the page title, or
the title without a trailing qualifier.

## Titles and Headings

- Use Title Case for every heading of every level: capitalize the first and the last word and every other word,
  except articles (a, an, the), coordinating conjunctions (and, but, or, nor, for, so, yet) and prepositions of up to
  three letters (as, at, by, in, of, off, on, per, to, via, vs). Prepositions of four letters or more are capitalized
  (With, From, Into, Over, Through), and so is the adverb in "Up to Date". `TestHeadingTitleCase` enforces this for the
  tutorial, the guides, the cookbook, the specification, the project documents of `docs/` and the README.
- Words in code spans stay as written (`zp gen`, `foldl`). Every part of a hyphenated word is capitalized
  (Built-In, Indentation-Based).
- The title of a page names what the page is for. Tutorial titles name what the reader builds or learns, guide
  titles name the feature (a noun phrase), recipe titles start with a verb for the task, and specification titles name
  the construct.
- The page title and the label of its entry in the index agree.
- Do not number headings in guides, the cookbook or the specification. A number breaks every link to the sections
  after an insertion. A tutorial lesson that has steps names them in its text, not in its headings.
- A heading is a noun phrase or an imperative that the reader can find again; a question is allowed in a
  troubleshooting list only.

## Voice and Terms

- **Tutorial.** Second person, imperative, present tense: "Create `calc.zp`." "Run it." No "we" and no "let us".
- **Guide.** Second person for procedures. Declarative present tense for behavior. Do not hedge behavior that the
  specification defines; state it once and link to the specification. Keep advice (what to prefer) apart from the
  contract (what happens) in separate sentences or paragraphs.
- **Cookbook.** The problem in a sentence or two, then the solution. Steps are imperative.
- **Specification.** Third person, declarative present tense. MUST, SHOULD and MAY only as the
  [specification README](../spec/README.md) declares them. No second person, no advice, no performance remarks, no
  history of the feature.
- One name for one concept, taken from the [glossary](../spec/glossary.md): Rule Expression, Rule Declaration Statement,
  Operators Rule, Value Action, Predicate Action, start rule, position unit, code point, node. Defined terms are
  capitalized as the specification capitalizes them. A new term lands in the glossary before it is used in a page.
  "Operators Rule" is the only name of that construct.
- American English. Write "Go API", "TypeScript", "WebAssembly" and "`.zpc`". Say "input" for the text that a
  parser reads and "grammar source" for a `.zp` file.
- Write for the reader of the published page. Do not mention chats, requests, review, the order in which work was
  done, backlog identifiers or agents. A page does not describe how it differs from an earlier version; a migration is
  a section that tells the reader what to change, with the old and the new spelling.

## How a Page Opens and Ends

- A page opens with one to three sentences saying what it is and who needs it. It does not start with "This document"
  or "This guide". A guide shows basic usage before options.
- A section opens with its point, then shows the example, then states the rule that the example demonstrates. A long
  page lists its sections near the top only when a reader would use the list to jump.
- A guide ends with Related Topics, a tutorial lesson with Next Steps. A cookbook recipe follows its template: a
  "**Problem.**" paragraph, the solution with its files, Expected Output, How It Works, and Variations and Limits.
  `TestCookbookRecipes` enforces the paragraph and the order of the sections.
- The first line of a code block that holds a file the reader should save is a comment with the file name
  (`// main.go`, `// rules.zp`). The extraction rule of the cookbook relies on it.

## Examples

Every example is run, and its output is pasted from that run. An example that a reader copies must work as printed.

- Tag every fence. Grammars are `zp`, Go is `go`, TypeScript is `ts`, JSON is `json`, shell commands are `bash`
  (not `sh` or `console`), and the output of a program or a command is `text`. An untagged fence is a mistake.
  `TestCodeFenceLanguages` enforces this.
- Draw a diagram as a `mermaid` fence when it has more than a handful of nodes, branches or labeled edges; the site
  renders it in the reader's color theme and keeps the fence text where scripts do not run. Keep a small sketch
  (a short pipeline or a tree of a few lines) as a `text` fence, which reads the same everywhere. A diagram shows
  structure; a label is a word or two, and the prose carries the explanation. Only pages that contain a `mermaid`
  fence load the renderer.

  ```mermaid
  flowchart LR
    A[Grammar text] --> B[Syntax tree]
    B --> C{Valid?}
    C -- yes --> D[Program]
    C -- no --> E[Diagnostics]
  ```

- Put a command and its output in separate blocks, the command first, with no `$` prompt, so a reader can copy the
  command. `TestNoCommandPrompts` enforces this.
- Write a complete `package main` program when the program is the point, and statement fragments otherwise, never half a
  function. Handle errors: `log.Fatal(err)` in programs, `return err` in fragments. Do not use `panic` and do not
  ignore errors.
- Grammars are formatted with `zp fmt`. A rule body that spans lines is parenthesized, because a line break
  before the action ends the rule. Types come first, then rules in the order a reader needs them. `main` is the start
  rule unless the page is about start rules.
- State the unit of every position that is shown, or write it as `line:column`. Write ranges as `[start, end)`.
- Machine-dependent numbers (times, sizes of memory) say on what they were measured, and a page says that they differ
  between machines.

## Before Committing a Page

1. Run the examples of the page, and compare the output that the page shows with the output that you get.
2. Run `cd site && go test -count=1 ./...` and `go test ./spec/...`. They check link directions, headings and the
   anchors of links. The build check of [website maintenance](site.md) checks the links of the rendered site.
3. Update the index of the directory, `site/config.json` and the pages that link to a page that you moved or split.
4. Regenerate generated pages (`docs/benchmarks.md`, `spec/language/syntax.md`) instead of editing them.
5. Read the page once as the reader it is for, without the context of the change.
