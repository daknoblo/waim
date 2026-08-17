# Contributing to waim

Thanks for taking the time to help out. Bug reports, ideas and pull requests are
all welcome.

> This is a personal side project developed with heavy AI assistance. Reviews
> may take a while — please do not read a slow reply as disinterest.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
Security problems have their own process, described in [SECURITY.md](SECURITY.md)
— please do not open a public issue for them.

## Getting set up

You need [Go](https://go.dev/dl/) (the version in [`go.mod`](../go.mod)), `git`
and `curl`. Docker is only needed if you want to build the image.

```bash
git clone https://github.com/daknoblo/waim.git
cd waim
make tools   # installs the templ CLI and the Tailwind standalone binary
```

`make tools` puts the templ CLI in your Go bin directory. If `templ` is not
found afterwards, that directory is not on your `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

The templ version is pinned on purpose — please install it via `make tools`
rather than `@latest`, otherwise the generated code will differ from what CI
expects.

## Running it

```bash
make run     # build and start on http://localhost:8080
```

Configuration lives in the web UI; data goes to `./appdata` in the working
directory (gitignored), including the generated `master.key`.

Without a Jellyfin server most pages stay empty, which makes UI work awkward.
Two shortcuts help:

```bash
make seed    # fill ./appdata with a synthetic scan run, so every card has data
make demo    # render the static demo site into ./dist
```

## Generated files are committed

`internal/web/*_templ.go` and `internal/web/assets/static/app.css` are checked
into the repository so the project builds without the templ and Tailwind
toolchain. **CI fails if they are stale.** After touching a `.templ` file or a
CSS class:

```bash
make generate   # regenerate *_templ.go
make css        # rebuild app.css
```

Commit the results together with your change.

## Before opening a pull request

```bash
make generate && make css   # only if you touched templates or styles
make vet
make test
golangci-lint run ./...     # optional locally; CI runs it either way
```

CI additionally runs the tests with the race detector and checks that the
documented image version matches the latest feature release.

A few things that are easy to miss:

- **User-facing strings are translated.** Every key belongs in both
  [`internal/i18n/locales/en.json`](../internal/i18n/locales/en.json) and
  [`internal/i18n/locales/de.json`](../internal/i18n/locales/de.json). English is
  the fallback, so a missing German key silently shows English text.
- **Secrets never get logged or exported.** API keys are encrypted at rest and
  redacted in log output; please keep it that way.
- **Comments explain why, not what.** The codebase deliberately keeps commentary
  sparse and reserves it for reasoning that is not obvious from the code.

## Commit messages

The project follows [Conventional Commits](https://www.conventionalcommits.org/).
The subject is lowercase and describes the effect of the change:

```
feat(stats): show upcoming releases for the library
fix(about): do not link patch versions to a release page
docs: mention the image retention job
ci: prune superseded sha- images from ghcr.io
refactor(stats): move the upcoming section below the gap overview
```

Breaking changes get a `!` and a `BREAKING CHANGE:` footer explaining what users
have to do:

```
feat!: generate the encryption key instead of requiring WAIM_MASTER_KEY
```

## Pull requests

- Open an issue first for larger changes, so the approach can be discussed
  before you invest time. Small fixes can go straight to a PR.
- Keep a PR focused on one thing. Unrelated cleanups are easier to review
  separately.
- Describe what changes for users, and how you verified it.
- Update the documentation in [`docs/`](../docs) when behaviour or configuration
  changes.

## Releases

Releases are cut by the maintainer with `make release` and are not part of a
regular pull request — please do not bump versions or pin documentation
versions in a PR.
