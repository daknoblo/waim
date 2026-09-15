# Development

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

## Prerequisites

- Go 1.25+
- [templ](https://templ.guide/) CLI (code generation)
- The Tailwind CSS standalone CLI (CSS generation) — no Node.js required

Both tools are installed by:

```bash
make tools
```

This runs `go install` for the pinned templ version and downloads the matching
Tailwind standalone binary for your platform into `./bin`. If `templ` is not
found afterwards, your Go bin directory is not on the `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
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
make run        # build and run
make seed       # fill ./appdata with a synthetic scan run
make demo       # render the static GitHub Pages demo into ./dist
make docker     # build the Docker image locally
make release VERSION=1.5.0   # tag approved main (push stays manual)
make release BUMP=patch      # compute and tag the next stable patch
```

The demo site is the real UI rendered with sample data (`cmd/demo`): htmx
attributes are stripped and routes are rewritten to file names, so it works from
any static host. `.github/workflows/pages.yml` publishes it to GitHub Pages on
every approved push to `main`, after CI passes. Manual demo deployment is also
restricted to `main`; running the workflow on `develop` skips deployment.

Run locally:

```bash
WAIM_ADDR=:8080 make run
```

Locally, waim stores its data in `./appdata` (gitignored) in the working
directory, including the generated `master.key`. Then open
<http://localhost:8080>.

Without Jellyfin, the watch collection works with TMDB alone. For completely
offline UI work, `make seed` writes two synthetic source snapshots, overlapping
provenance, virtual entries, ratings and releases in a **fresh** data directory:

```bash
make seed                      # into ./appdata
make seed SEED_OUT=./appdata-demo # separate local fixture directory
make seed SEED_FORCE=1         # overwrite an existing database
```

It never overwrites an existing config. It refuses an existing database unless
`SEED_FORCE=1`. Automatic scans/cache refresh are disabled and no TMDB key is set;
no real credentials or services are needed. Its random seed is fixed.

For an entirely static, offline preview of all eight pages:

```bash
go run ./cmd/demo -out ./dist -locale en
go run ./cmd/demo -out ./dist-de -locale de
# Open dist/index.html in your browser. Forms are illustrative, not functional.
```

Feature tests use local HTTP fakes/cache fixtures:

```bash
go test -race ./internal/media ./internal/config ./internal/source ./internal/store ./internal/scanner ./internal/scheduler ./internal/server ./internal/web
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
golangci-lint run
go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
make css
```

Browser acceptance should cover both locales/mobile, two named instances,
same-TMDB overlapping season ownership, watch add/remove during a scan, a
standalone movie before/after release, per-instance badges/links and stale or
unknown sources. Static demo checks do not replace live mutation acceptance.

## Editing the UI

1. Edit the relevant `internal/web/*.templ` file.
2. Run `make generate` to regenerate the Go code.
3. If you add new Tailwind classes, run `make css` to rebuild the stylesheet.
4. Rebuild and run.

Use the pinned **standalone** Tailwind compiler for committed CSS. The npm CLI
can produce different minifier ordering even at the same Tailwind version,
which fails CI's byte-for-byte generated-asset check. If the native compiler
cannot run, use the pinned Linux standalone binary in a local Linux container,
matching CI rather than substituting the npm CLI.

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
  date, checks that documentation pins reference tags approved on `main`,
  tests the release guards, runs `go vet`, golangci-lint, race-enabled tests
  and a build. Runs on pushes/PRs to both branches and is reused as a
  prerequisite by image and demo publishing.
- `.github/workflows/release.yml` — builds and pushes multi-arch images to
  `ghcr.io`. `main` → `:latest` and `sha-…`; `develop` → `:dev` and
  `sha-dev-…`. Version tags on commits already in `main` publish `X.Y.Z` and
  `X.Y` and create a GitHub Release, including patches. Tag builds do not
  update `:latest` or commit-specific branch images. Images are scanned with
  Trivy. Branch builds display `stable-YYYYMMDD-HHMM` or `dev-YYYYMMDD-HHMM`
  on the About page; tagged builds display the release version.
- `.github/workflows/codeql.yml` — analyses both branches and their PRs.
- `.github/dependabot.yml` — sends dependency updates to `develop`, not stable.
- `.github/workflows/prune-images.yml` — weekly retention for the `sha-…`
  images (including `sha-dev-…`), keeping the newest 20 combined.
  Version tags, `:latest` and `:dev` are never touched, and untagged versions
  are left alone because they are the per-architecture
  children of the multi-arch manifests. Run it manually with `dry-run` first if
  you change the retention.

## Branching & releases

### Development and promotion

- `main` is stable and remains the default branch. `develop` is the long-lived
  integration branch. Create feature/fix branches from `develop` and send PRs
  back to `develop`.
- Test the automatically published `:dev` image (or a specific `sha-dev-…`)
  with a **separate data volume**, container name and host port. Do not share
  the host data directory between stable and dev; migrations may make downgrades unsafe.
- When ready, open a promotion PR from `develop` to `main`. The maintainer
  manually merges it after testing. No auto-merge: a green test alone is not
  a release approval.
- Use a merge commit for promotion, not squash/rebase, to preserve shared
  ancestry. Keep `develop` after merging. Merge `main` back into `develop`
  when it contains changes not already there, especially after a stable hotfix.
- Protection on `main` requires a PR, the `Lint, Test & Build` check on an
  up-to-date branch and resolved review conversations, including for admins.
  Force pushes and branch deletion are disabled. This is a solo-maintainer
  flow: no second-person approving review is required; the manual merge is
  the maintainer's approval.
- Merging promotion updates `:latest` and the public demo only after publishing
  CI passes. Development publishing can never move either.

### Tagging a stable version

Every **new** `X.Y.Z` tag is stable and gets release notes, including patch
versions. Historical patch tags created under the old test-build convention
are not retroactively converted. Tests now use `:dev` rather than version tags.

After promoting, switch to the approved commit:

```bash
git switch main
git pull --ff-only origin main
make release BUMP=patch MESSAGE="Fix missing release dates"
# Or: make release BUMP=minor, which opens $EDITOR for notes.
# Or: make release VERSION=2.0.0 NOTES=/path/to/release-notes.md
```

`make release` first requires clean local `main`, fetches `origin/main` and
tags, and verifies local `HEAD` is exactly the approved remote commit. It
creates only an annotated tag: no generated-file changes, documentation
commits, or branch pushes. Generate/test changes before promotion instead.
It refuses duplicate versions, including an existing `v`-prefixed equivalent.

Push the tag printed by the command, or use `PUSH=1` to publish it immediately.
The publishing workflow independently rejects tags outside `main` before it
can push an image. Tagging an older approved commit does not roll `:latest`
back; that channel follows approved `main` pushes only. The `X.Y` alias tracks
the most recently published tag in that minor line, so publish patches in order.

Tags use plain semver without a `v` prefix, matching the image tags.
A `v`-prefixed tag is still accepted and normalised for the image version.

Once the version is published, update pinned installation examples through
a normal PR with `make docs-version VERSION=X.Y.Z`. Pins must refer to an
existing version tag on `main`, but need not change with every release.
This avoids creating an unreviewed docs commit on protected `main` or a
release/CI dependency cycle.

### Repository settings versus workflow changes

Branch protection is a GitHub repository setting, not something YAML enables
on its own. Keep `main` protection enabled, repository auto-merge disabled,
and automatic head-branch deletion disabled to retain `develop`. The initial
workflow setup PR must be merged before these new rules govern publishing on
`main`; until then its existing release workflow still applies.

## Contributing

See [CONTRIBUTING.md](../.github/CONTRIBUTING.md) for the pull request workflow,
commit conventions and the checks CI runs on every change.
