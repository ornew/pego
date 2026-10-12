# Nanosite Configuration

- Read `site/build.sh`, `site/config.json` and `site/nanosite.rev` first. The CLI and `sitegen.BuildWith` share the standard config and pipeline; lower-level custom drivers may define different contracts.
- `site/nanosite.rev` pins the generator and installed skill to the same commit. The selected command embeds its own skill; run `nanosite skill -out .agents/skills/nanosite` through `hack/update-nanosite-skill.sh`. Do not copy a newer skill independently.

## Standard CLI

- Paths: `-repo` is the input root; `-config`/`-out` are relative to the working directory. Configured source paths are repository-relative.
- Flags: `-check` validates without writes; `-drafts` includes drafts; `-base-path` overrides deployment prefix; `-serve` builds once, then previews.
- Identity: `title`, `description`, `lang`; optional `repository`, `branch` for source links.
- Deployment: `siteURL` is the HTTP(S) origin, `basePath` the separate prefix. An origin enables canonical URLs, sitemap and robots output.
- Publication: `include` selects Markdown, `exclude` wins over includes/assets, `drafts` defaults false. Globs: `*`, `?`, character classes, whole-segment `**`; no braces/negation.
- Routes: `prettyURLs` selects `topic/index.html` or `topic.html`; README/index become directory indexes. Collisions fail.
- Navigation: ordered `sections` have globally unique `title`, `match`, optional `collapsed`, `index`, `first`, and nested `children`. Child matches are checked before parent matches. `index` names the README source for ordering; `first` lists sources to prioritize among equal-weight pages.
- Paging: `pager` is `section` (default), `group` (top-level section and descendants), `site`, or `none`.
- `pagerOrder` is an ordered array of source-path glob patterns. It prioritizes matched pages within their top-level group, independent of navigation order; unmatched pages retain stable order after matches.
- Record indexes: `recordIndexes` entries define a virtual Markdown `source`, optional `directory` (defaults to the source directory), required `title` and `columns` (`title`/`field` pairs), optional `introduction`, `required` metadata fields, a template basename, `templateText`, `notice`, `optional`, `includeTemplate`, and `tableClass`. The generated source is immutable and receives normal front matter/defaults, routing, link and fragment handling. Records follow page publication filtering; numbered NNN records are listed in order and 000 records are skipped unless `includeTemplate` is true. The default exclude glob still applies, so adjust it to include a matching template. `optional` skips a missing records directory. `notice` adds a generated comment and `tableClass` styles the table. `templateText` is a Markdown footer appended unconditionally without `template`, or when that named template is published.
- Markdown: `markdown.rawHTML`, `alerts`, and `headingLinks` default false. `headingIDs` defaults to `unicode`; supported values are `unicode`, `github`, and `goldmark`. `alignAttributes` defaults true and accepts explicit false. `searchHeadings` adds heading-fragment results to search and defaults false.
- Assets: `assets` copies at repository-relative paths; `staticDir` maps a public directory to the output root. `styles`/`scripts` list output-relative assets loaded after defaults. `template` replaces the Go HTML layout.
- UI: `search`, `toc`. `redirects` maps old local routes to published destinations; chains/missing destinations fail.

## Standard front matter

- `title`, `description`, `navTitle`: metadata/short navigation label; title does not replace the H1.
- `section`, `weight`: group and lower-first ordering; configured section index/first sources sort first among tied pages, followed by README introductions and source paths.
- `layout`: `home` or `doc`; `permalink`/`aliases`: local routes ending in `/` or `.html`.
- `publish: false`: always omit; `draft: true`: omit unless enabled. File exclusions still win.
- `nav`, `search`, `toc`: navigation/paging, search and heading TOC controls; `tocDepth` selects the maximum heading level. Hiding navigation/search does not prevent publication.
- Precedence: engine defaults → matching config defaults → page metadata. Explicit false/zero override earlier values; unknown fields/types fail.
- Markdown links resolve against source paths, including custom permalinks. Validate targets/fragments; include local assets explicitly. Excluded-page links fail; external availability is not checked. Raw HTML is disabled by default.

## Go extension configuration

- `tocDepth` defaults to 4 and accepts 2–6. It controls TOC and search heading depth. `order` lists source paths to prioritize at equal weights, before README ordering.
- `directoryLinks` defaults false and uses trailing-slash index URLs when enabled. `repositoryLinks` defaults false and turns unresolved source links into repository file/directory links when `repository` is configured; these links take precedence over an incidental published route with the same filename.
- `bundledAssets` and `redirectRules` default true. The former copies built-in CSS/JS/theme files; the latter emits Netlify `_redirects`. `redirectTemplate` optionally supplies a Go template receiving `.Config`, `.Href`, and `.Lang`.
- `templates` loads named partial templates into the layout selected by `template`. `searchIndex` fields: `path` (default `assets/search-index.js`), `variable` (default `globalThis.NANOSITE_SEARCH`, or raw JSON when path ends `.json`), `fields` (mapping of `title`, `url`, `text`, `section`, `headings` to output keys), `text` (`markdown`/`html`), `headingTitle` (`context`/`heading`), `headingText` (`heading`/`page`), and `section` (`leaf`/`group`).
- The `sitegen` Go package exposes `ReadConfig`, `DefaultConfig`, and `BuildWith`. `BuildWith` accepts `Extensions` hooks: `Prepare`, `Decorate`, `View`, `TemplateFuncs`, `Validate`, plus marker and output policy fields. `Context` exposes selected pages/config/Markdown, generated assets, `AddHTMLPage`, and `Render`; generated HTML is used verbatim and supplied headings are needed for heading search. `Validate` runs on staged output in normal and check builds. `HTMLText` extracts plain text. See `docs/guide/extensions.md` for the API example.
- Keep standard publication, link validation, and output ownership in the builder. Use lower-level `render`/`publish` packages only when a custom pipeline is needed. Build the actual site and distinguish local output from deployment evidence.
