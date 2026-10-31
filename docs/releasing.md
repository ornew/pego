# Releasing

This page says what a ZeroParse release promises, which platforms and Go versions it supports, and how a release is
tagged. It is written for maintainers who cut a release and for users who decide whether to depend on one. The
vulnerability policy is in [SECURITY.md](../SECURITY.md).

ZeroParse has no tagged release yet.

## What a v0.x Release Promises

Go modules give a v0 version no compatibility guarantee. ZeroParse states its intent for each surface instead.

| Surface | At v0.x |
|:--|:--|
| The grammar language: `.zp` syntax and meaning ([specification](../spec/README.md)) | Stable in intent. A change that breaks a grammar is announced, and `zp fix` rewrites older syntax where it can |
| The API of generated Go parsers (`Parse`, `ParseAST`, the options and the error types) | Stable in intent. A generated parser carries the runtime of the generator that produced it, so regenerate it after upgrading ZeroParse |
| The module API, including the `zp` and `grammar` packages | May change in any release; changes are announced |
| The `runtime` package, as generated code uses it with `zp gen -runtime import` | Unstable: matches only a generator of the same ZeroParse version, so regenerate the parsers on every upgrade |
| Compiled grammars (`.zpc`) and the bytecode: container version 7, instruction-set version 29 ([bytecode specification](../spec/bytecode/README.md)) | Unstable: may change in any release. A loader accepts only its own versions and asks for the grammar to be compiled again |
| The JSON forms of grammars and nodes | Unstable: may change in any release |
| Ready-made parsers under `parsers/` | Not part of a release: they are used from a checkout of the repository, and `go get` does not fetch them from another module |

"Stable in intent" means that a change needs a reason and is announced; it is not the compatibility guarantee of a
v1 module.

## Version Numbers

Releases are tagged with semantic versions, `vMAJOR.MINOR.PATCH`, as Go modules require. The first release is
v0.1.0.

- Before v1, a **minor** version may change a surface that is stable in intent, with an announcement.
- A **patch** version fixes defects and changes no surface that is stable in intent.
- A release that turns out to be broken is retracted with a `retract` directive in the `go.mod` of the next release;
  a published version cannot be removed from the Go module proxy.

## Announcing Changes

Each release has release notes on its GitHub release page; the repository keeps no separate changelog file. The notes
list breaking changes first, each with what to do (including the `zp fix` rewrite where one exists), then new
features and fixes.

## Supported Platforms

| Platform | Status |
|:--|:--|
| macOS on Apple silicon (darwin/arm64) | Tested: the repository tests and the benchmarks run on it |
| Linux on x86-64 (linux/amd64) | Tested by the continuous-integration workflow when it runs. Workflows are paused at present, so a release runs the checks on linux/amd64 before it is tagged |
| WebAssembly (js/wasm) | Tested: the playground's WebAssembly build has a smoke test under Node.js, which runs when Node.js is installed |
| Windows | Not tested. The module builds and passes `go vet` for windows/amd64 |
| 32-bit platforms | Not supported. The module builds for linux/386, but known integer-overflow defects on 32-bit platforms are open; the work is deferred |

Generated TypeScript is tested under Node.js: the continuous-integration workflow uses Node.js 22.18.0, and the
benchmarks run it under Node.js 24.13.0. It is written to run in Deno, Bun and browsers, but no test runs it there.

## Go Versions

**ZeroParse requires Go 1.27.1 or later, and so does the Go code it generates.** That covers the `zp` command, the engine
API and generated Go with either runtime: the default `-runtime embed`, which copies the runtime into the generated
package, and `-runtime import`, which makes the consumer depend on the ZeroParse module. Generated Go is required to build
with Go 1.27.1 or later; no lower version is promised.

**Recording `zp` as a tool raises the module's `go` line to 1.27.1.** `go get -tool github.com/ornew/zeroparse/cmd/zp`
(`zeroparse.dev/zp/cmd/zp` once the repository has moved; see [Tagging a Release](#tagging-a-release)) adds
the ZeroParse module as a dependency, and because that module requires Go 1.27.1, the `go` command raises the consumer
module's `go` line to match (`go: upgraded go 1.23 => 1.27.1`). This is expected: the generated code needs the same
version.

## Tagging a Release

In the commands below, `vX.Y.Z` is the version being released, and the module path is the one in force at the time:
`zeroparse.dev/zp` once the repository has moved (step 2).

1. Check that the release items of the [project strategy](strategy.md#wedge-and-sequencing) are done.
2. For the first release only, move the repository to the `zeroparse` GitHub organization:
   - transfer the repository from `github.com/ornew/zeroparse` to `github.com/zeroparse/zp`, and point the `origin`
     remote of each checkout at the new address;
   - serve `go-import` and `go-source` meta tags from the site so that `zeroparse.dev/X` resolves to
     `github.com/zeroparse/X` for every repository of the organization;
   - change the module path to `zeroparse.dev/zp` in every `go.mod` of the repository and in every import
     path and document that names it (the root package is already named `zp`, so no qualified use changes);
   - regenerate the generated files with `go generate ./parsers ./bench`;
   - run the full suite, as in step 4;
   - update the site (`repository` in `site/config.json`) and the Workers Builds Git source (the repository that the
     Cloudflare Worker builds from, see [website maintenance](site.md));
   - enable private vulnerability reporting for the new repository, so that the route in
     [SECURITY.md](../SECURITY.md) works;
   - commit the change to `main`.

   After the first release, the ready-made parsers may move to their own repositories under the organization.

3. Start from a clean checkout of `main` at the commit to tag:

   ```bash
   git switch main && git pull --ff-only && git status --short   # prints nothing
   ```

4. Run the checks of the [development guide](development.md):

   ```bash
   go test -timeout=30m ./...
   go generate ./parsers ./bench && git status --short   # generated files are current: prints nothing
   parsers/test.sh
   (cd site && go test -count=1 ./...) && site/build.sh
   ```

5. Run the checks of step 4 on linux/amd64 as well, on a Linux machine or by a run of the continuous-integration
   workflow. While workflows are paused, this run is a precondition of the release.
6. Check the figures in the README and in [Why ZeroParse](why-zeroparse.md) against the current
   [benchmark report](benchmarks.md); rerun the benchmarks only if the report is older than the changes it describes.
   From the first release, `zp gen` generates parsers with budget controls by default (today `-budget-controls` turns
   them on), and published performance figures are measured with the limits on.
7. Update the resource policy of [SECURITY.md](../SECURITY.md) to the defaults that this release ships.
8. Write the release notes ([Announcing Changes](#announcing-changes)).
9. Tag the commit with an annotated tag and push the tag:

   ```bash
   git tag -a vX.Y.Z -m "ZeroParse vX.Y.Z"
   git push origin vX.Y.Z
   ```

   Signing the tag (`git tag -s` in place of `git tag -a`) is optional. When workflows are enabled, pushing the tag
   starts the continuous-integration workflow. The local checks of steps 4 and 5 are the release gate either way.

10. Ask the Go module proxy for the new version, so that `go get` and pkg.go.dev see it:

    ```bash
    GOPROXY=https://proxy.golang.org go list -m zeroparse.dev/zp@vX.Y.Z
    ```

11. Publish the release notes on the GitHub release of the tag.
