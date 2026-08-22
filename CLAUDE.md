# clisurface — Claude Code instructions

A Go library, not a CLI. No `main` package, nothing to install; it ships as git
tags and is consumed by anything that needs a CLI's command tree as data.

Read `.planning/design.md` before writing any code here. The boundary between
this library and its consumers is the decision this repo exists to hold.

## Constraints that must not regress

- **A bare subcommand is never run.** Discovery may run `--help` at any depth
  and a framework's completion callback, and nothing else. The shape most worth
  finding is a noun that performs a read when invoked with no verb, and
  detecting it by running it would fire real reads against production APIs and
  databases. This is the invariant the whole library is trusted on.

- **No consumer's own knowledge reaches this package.** It never reads a repo
  registry, never resolves a repo to a binary, and holds no opinion about
  whether a verb was well chosen. Those are questions about *one portfolio's*
  CLIs; this answers questions about *a* CLI. A consumer passes in a binary
  name and gets a tree back. The test is whether the package would still make
  sense to a reader who has never seen the tools that consume it.

- **The core takes no third-party dependencies; anything that needs one gets its
  own package.** What binds is containment rather than a blanket ban, so a
  consumer importing the core inherits nothing.

  Nothing has needed the split yet. A `clisurface/pty` package was planned, for
  reading a tool's help the way a person sees it, and the measurement killed it:
  `COLUMNS`, `FORCE_COLOR` and a declared `TERM` reproduce everything a
  pseudo-terminal produces. `DisplayRunner` is stdlib-only as a result. Measure
  before reaching for a dependency here.

- **Every reader gets a hostile fixture.** A parser change can silently *reduce*
  coverage, and the failure looks like a tool that legitimately has no
  subcommands. That is how `gh` returned zero commands for months. A reader
  without a fixture pinning its shape is a reader nothing will notice breaking.

- **The `go.mod` floor matches the house declaration, not the lowest version
  the code needs.** Generated CI reads the floor from `go.mod`, pins
  `GOTOOLCHAIN=local`, then installs golangci-lint, which carries its own
  minimum — so a floor below that fails Lint on a repo whose code is fine. A
  library would otherwise floor lower than a tool; here they match, and that is
  the reason.

## Rules that live elsewhere

Layout, gofumpt, golangci-lint v2, doc comments, the module rules, and what to
test at which layer are house-wide standards rather than this repo's, and are
not restated here.

## Sanctioned exceptions

- **No goreleaser.** There is no binary to build, upload or install. A consumer
  resolves this by module path and tag, so `release.yml` cuts the tag and there
  is nothing else to ship.

## Never write the breaking-change trailer in a commit message

Those two words — either number, colon or not, subject or body — cut a major
release here, and a major on a library is an outage rather than a version. The
analyzer matches them unanchored against the raw message and ORs the result with
the configured rules, so no config can stop it and it majors even a `fix:`
commit. **The ban covers a commit that merely discusses the trailer**; name it
some other way and never quote it.

The module path carries no `/vN` suffix, so once a major exists every consumer
resolving `@latest` silently keeps the highest v1 instead. Deliberate majors use
`chore(release-major)`. The full reasoning and the reset procedure live in the
house release standard.
