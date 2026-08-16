# Development

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

## Prerequisites

- Go 1.25+
- [templ](https://templ.guide/) CLI (code generation)
- The Tailwind CSS standalone CLI (CSS generation) — no Node.js required

Install the templ CLI:

```bash
make tools          # go install github.com/a-h/templ/cmd/templ@v0.3.1020
```

Download the Tailwind standalone binary into `./bin` (example for macOS arm64;
pick the asset for your platform from the Tailwind releases page):

```bash
curl -fsSL https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.17/tailwindcss-macos-arm64 -o bin/tailwindcss
chmod +x bin/tailwindcss
```

## Project layout

See [architecture.md](architecture.md) for the package overview. UI templates
live in `internal/web/*.templ`; their generated `*_templ.go` files and the
compiled `internal/web/assets/static/app.css` **are committed** so the project
builds without the templ/Tailwind toolchain (e.g. in the Docker build).

## Common tasks

```bash
make generate   # regenerate Go code from .templ files
make css        # rebuild the embedded Tailwind CSS
make build      # build the static binary into ./bin/waim
make test       # run tests
make vet        # go vet
make run        # build and run (needs WAIM_MASTER_KEY)
make seed       # fill ./appdata with a synthetic scan run
make demo       # render the static GitHub Pages demo into ./dist
make docker     # build the Docker image locally
```

The demo site is the real UI rendered with sample data (`cmd/demo`): htmx
attributes are stripped and routes are rewritten to file names, so it works from
any static host. `.github/workflows/pages.yml` publishes it to GitHub Pages on
every push to `main`.

Run locally:

```bash
WAIM_MASTER_KEY=dev-secret WAIM_ADDR=:8080 make run
```

Locally, waim stores its data in `./appdata` (gitignored) in the working
directory. Then open <http://localhost:8080>.

Without a Jellyfin server the pages stay empty, which makes UI work on the
statistics page awkward. `make seed` writes a database with a synthetic scan
run — titles, gaps, ratings and announced releases — so every card has data:

```bash
make seed                      # into ./appdata
make seed SEED_OUT=/tmp/waim   # somewhere else, e.g. to mount into a container
make seed SEED_FORCE=1         # overwrite an existing database
```

It refuses to overwrite an existing database unless `SEED_FORCE=1`, and its
random seed is fixed so screenshots stay comparable between runs.

## Editing the UI

1. Edit the relevant `internal/web/*.templ` file.
2. Run `make generate` to regenerate the Go code.
3. If you add new Tailwind classes, run `make css` to rebuild the stylesheet.
4. Rebuild and run.

When changing user-facing strings, update **both** locale files
(`internal/i18n/locales/en.json` and `internal/i18n/locales/de.json`) and use the
`T(...)` helper in templates rather than hard-coding text.

The layout is mobile-first: base classes target phones, `sm:`/`md:`/`lg:`
variants restore the wider layouts. Below `md` the navigation collapses into the
menu button handled in `assets/static/app.js`; below `sm` tables marked with
`table-cards` render as stacked cards and take their labels from the
`data-label` attribute of each cell. Because of the strict CSP there are no
inline scripts or styles — behaviour goes into `app.js`, styling into
`assets/input.css`.

## Linting

```bash
golangci-lint run ./...
```

The configuration lives in `.golangci.yml` (golangci-lint v2).

## Continuous integration

- `.github/workflows/ci.yml` — verifies generated templ code and CSS are up to
  date, checks the docs pin the newest release tag, runs `go vet`,
  golangci-lint, race-enabled tests and a build.
- `.github/workflows/release.yml` — builds and pushes multi-arch images to
  `ghcr.io`. `main` → `:latest`, git tags → semver tags, every commit → a
  `sha-…` tag. Images are scanned with Trivy.

## Branching & releases

- `main` is the only long-lived branch; every merge publishes the `:latest`
  image plus an immutable `sha-…` tag for rollbacks.
- Use short-lived feature branches and pull requests against `main`; CI runs on
  every PR.
- To publish a pinned, versioned image:
  1. `make docs-version VERSION=X.Y.Z` — points the pinned image examples in
     the docs at the release.
  2. Commit and push the result.
  3. Create and push the `X.Y.Z` tag.

  The order matters: CI fails when the docs still pin an older version than the
  newest tag, so the docs update has to land before the tag.

  Tags are plain semver without a `v` prefix, matching the published image tags
  — `docker/metadata-action` strips a leading `v`, so `ghcr.io/...:v1.2.3` would
  never exist. A `v`-prefixed tag still triggers a release and is normalised to
  `1.2.3`, but the plain form is the convention.
