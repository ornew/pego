# PEGO Site Build

The pinned Nanosite CLI and `sitegen.BuildWith` share the standard publication
pipeline. `site/config.json` controls source publication, defaults, front matter,
record indexes, navigation, paging, and search. The project driver supplies the
PEGO landing page, API reference and Playground/theme integration through
Nanosite extension hooks; consult the installed Nanosite skill for config
semantics. The driver owns project-specific build flags and integration only.

```sh
sh site/build.sh
sh site/build.sh -serve localhost:8080
sh hack/update-nanosite-skill.sh                 # synchronize current pin
sh hack/update-nanosite-skill.sh FULL_COMMIT_SHA # update dependency and skill
```

`site/nanosite.rev` pins a published commit; `site/go.mod` must resolve to the
same module version. The wrapper checks this before running the driver. Requires
Go 1.27.1 or later. Tests run separately: `cd site && go test ./...`.

`site/config.json` excludes authoring templates while retaining published design
templates. Its standard include/exclude and metadata rules also apply to link
coverage and generated pages. The driver adds only its project hooks and flags.
See `docs/site.md` for build flags, navigation and deployment behavior; do not
hand-edit `site/dist`.
