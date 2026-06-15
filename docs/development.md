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
make docker     # build the Docker image locally
```

Run locally:

```bash
WAIM_MASTER_KEY=dev-secret WAIM_DATA_DIR=./appdata WAIM_ADDR=:8080 make run
```

Then open <http://localhost:8080>.

## Editing the UI

1. Edit the relevant `internal/web/*.templ` file.
2. Run `make generate` to regenerate the Go code.
3. If you add new Tailwind classes, run `make css` to rebuild the stylesheet.
4. Rebuild and run.

When changing user-facing strings, update **both** locale files
(`internal/i18n/locales/en.json` and `internal/i18n/locales/de.json`) and use the
`T(...)` helper in templates rather than hard-coding text.

## Linting

```bash
golangci-lint run ./...
```

The configuration lives in `.golangci.yml` (golangci-lint v2).

## Continuous integration

- `.github/workflows/ci.yml` — verifies generated templ code and CSS are up to
  date, runs `go vet`, golangci-lint, race-enabled tests and a build.
- `.github/workflows/release.yml` — builds and pushes multi-arch images to
  `ghcr.io`. `main` → `:stable`, `develop` → `:dev`, git tags → semver tags.
  Images are scanned with Trivy.

## Branching & releases

- `main` is the stable channel; merges here publish the `:stable` image.
- `develop` is the development channel; pushes here publish the `:dev` image.
- Create a `vX.Y.Z` tag to publish a pinned, versioned image.
