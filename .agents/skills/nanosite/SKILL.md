---
name: nanosite
description: Update Markdown publication, front matter, navigation, assets and theme configuration for sites built with Nanosite or its Go extension APIs.
---

# Nanosite

- Read `site/config.json`, `site/build.sh`, `site/nanosite.rev` and the site guide. Identify whether it uses the standard CLI or a project extension driver.
- Read [references/config.md](references/config.md) for configuration fields, front matter, publication and extension capabilities. Apply only the settings supported by the active driver.
- For Go extension behavior, follow the [site maintenance guide](../../../docs/site.md) and use `sitegen.BuildWith` to keep project hooks inside the shared publication pipeline.
- Keep Markdown the source of truth; do not edit generated output. Preserve published routes, fragments, theme assets, search and Playground integrations.
- Add finished pages to the relevant index/navigation. Exclude authoring templates and drafts deliberately; hidden navigation/search is not private publication.
- Build the actual site and check links after changes. For theme edits, inspect desktop/mobile, color modes and affected interactions. Distinguish local output from deployed behavior.
