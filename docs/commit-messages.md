# Commit Messages

## Guidance

### Language and general form

A commit message MUST be written in English.

A commit message MUST be readable, concise, and unambiguous, and MUST use the
format Git expects: a single-line summary, a blank line, and then the body.

```
Summarize changes in around 50 characters or less

More detailed explanatory text, wrapped to about 72 characters. The blank
line separating the summary from the body is required whenever a body is
present: tools such as `log`, `shortlog`, and `rebase` rely on it.

Explain the problem this commit solves, and why it is solved this way.
How it is solved is in the diff.

 - Bullet points are acceptable.

Resolves: #123
See also: #456, #789
```

### Summary line

- The summary SHOULD be about 50 characters and MUST NOT exceed 72.
- The summary MUST be written in the imperative mood, such that it completes the
  sentence "If applied, this commit will _____".
  - Good: `Refactor subsystem X for readability`
  - Bad: `Refactored subsystem X for readability`
- The first word MUST be capitalized, and the summary MUST NOT end with a
  period.
- The summary MAY contain inline code spans, and MUST NOT contain any other
  Markdown.
- The summary MUST say specifically what the commit does. Abstract phrases such
  as "fix a problem", "fix a bug", or "improve behaviour" MUST NOT be used.
  `Initialize the variable explicitly` is a better summary than `Fix a bug`,
  because it names the change.

### Body

- The body MAY span multiple lines and MUST be wrapped at 72 characters.
- Paragraphs are separated by blank lines.
- The body MUST explain **what** changed and **why**. It SHOULD NOT explain
  **how**: the diff says that.
- The body SHOULD be structured so that the reader gets the context, the change,
  and the consequence, in that order. A longer commit SHOULD use paragraphs for
  the background, the change itself, and any improvement it enables.
- Issue references SHOULD be placed at the end, as `Resolves:` or `See also:`
  trailers.

### What a commit contains

A commit SHOULD be one logical change. A change that invalidates a document MUST
update that document in the same commit; documentation is part of the logical
change, not a follow-up to it.

## Rationale

Commit messages are the only documentation that is guaranteed to still describe
the change it belongs to, because it can never drift from it. The formatting
rules exist so that the log is readable with the tools that read it — 50 and 72
characters are what `git log` and `git shortlog` were built around.
