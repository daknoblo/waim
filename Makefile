# waim — developer Makefile
#
# Common tasks for building, generating assets and running locally.

BINARY      := bin/waim
PKG         := ./...
TEMPL       := templ
TAILWIND    := ./bin/tailwindcss
TAILWIND_VERSION := v3.4.17
CSS_INPUT   := internal/web/assets/input.css
CSS_OUTPUT  := internal/web/assets/static/app.css

VERSION ?= $(shell date -u +%Y%m%d-%H%M)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

SEED_OUT ?= appdata

LDFLAGS := -s -w \
	-X github.com/daknoblo/waim/internal/version.Version=$(VERSION) \
	-X github.com/daknoblo/waim/internal/version.Commit=$(COMMIT) \
	-X github.com/daknoblo/waim/internal/version.Date=$(DATE)

.PHONY: all generate css build run test vet tidy tools clean docker demo docs-version seed release

all: generate css build

## Install the templ CLI (run once).
tools:
	go install github.com/a-h/templ/cmd/templ@v0.3.1020

## Generate Go code from .templ files.
generate:
	$(TEMPL) generate

## Compile the Tailwind CSS into the embedded static output.
css:
	$(TAILWIND) -c tailwind.config.js -i $(CSS_INPUT) -o $(CSS_OUTPUT) --minify

## Build the static binary.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/waim

## Run locally (requires WAIM_MASTER_KEY).
run: build
	$(BINARY)

## Render the static GitHub Pages demo into ./dist.
demo:
	go run ./cmd/demo -out dist

## Fill a database with a synthetic scan run so the UI can be reviewed without a
## Jellyfin server, e.g. `make seed` or `make seed SEED_OUT=/tmp/waim`. Refuses
## to clobber an existing database unless SEED_FORCE=1.
seed:
	go run ./cmd/seed -out $(SEED_OUT) $(if $(SEED_FORCE),-force,)

## Point the pinned image examples in the docs at a release, e.g.
## `make docs-version VERSION=1.2.0`. Run this before creating the tag;
## CI verifies that the docs match the newest tag.
docs-version:
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' \
		|| (echo "usage: make docs-version VERSION=X.Y.Z (got '$(VERSION)')" && exit 1)
	@files=$$(grep -rlE 'ghcr\.io/daknoblo/waim:v?[0-9]+\.[0-9]+\.[0-9]+' README.md docs/ || true); \
	test -n "$$files" || (echo "error: no pinned image version found in README.md or docs/" && exit 1); \
	sed -i.bak -E 's|(ghcr\.io/daknoblo/waim:)v?[0-9]+\.[0-9]+\.[0-9]+|\1$(VERSION)|g' $$files
	@find README.md docs -name '*.bak' -delete
	@echo "docs now pin $(VERSION)"

## Prepare a release: regenerate assets, pin the docs, commit and create the
## annotated tag. Pushing stays manual, so there is a point of no return you
## control. `make release VERSION=1.3.0` opens $$EDITOR for the notes (they
## become the GitHub Release body for X.Y.0 tags); pass MESSAGE="..." to skip
## the editor, which is what you want for throwaway test tags.
release: generate css
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' \
		|| (echo "usage: make release VERSION=X.Y.Z (got '$(VERSION)')" && exit 1)
	@if git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null; then \
		echo "error: tag $(VERSION) already exists"; exit 1; \
	fi
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "error: working tree is not clean — commit everything first"; \
		echo "(regenerated *_templ.go or app.css? CI rejects stale ones)"; \
		git status --short; exit 1; \
	fi
	@$(MAKE) --no-print-directory docs-version VERSION=$(VERSION)
	@git add README.md docs
	@git diff --cached --quiet || git commit -q -m "docs: pin $(VERSION)"
	@if [ -n "$(MESSAGE)" ]; then \
		git tag -a "$(VERSION)" -m "$(MESSAGE)"; \
	else \
		git tag -a "$(VERSION)"; \
	fi
	@echo
	@echo "tag $(VERSION) created. Publish with:"
	@echo "  git push origin main && git push origin $(VERSION)"

## Run tests.
test:
	go test $(PKG)

## Static analysis.
vet:
	go vet $(PKG)

## Tidy modules.
tidy:
	go mod tidy

## Build the Docker image.
docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t waim:$(VERSION) .

clean:
	rm -rf bin out dist
