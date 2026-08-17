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

# Passed through the environment rather than expanded into the recipe, so
# multi-line release notes survive instead of breaking the shell command.
export MESSAGE
export NOTES

LDFLAGS := -s -w \
	-X github.com/daknoblo/waim/internal/version.Version=$(VERSION) \
	-X github.com/daknoblo/waim/internal/version.Commit=$(COMMIT) \
	-X github.com/daknoblo/waim/internal/version.Date=$(DATE)

.PHONY: all generate css build run test vet tidy tools clean docker demo docs-version seed release

all: generate css build

## Install the local toolchain (run once): the templ CLI and the Tailwind
## standalone binary.
tools:
	go install github.com/a-h/templ/cmd/templ@v0.3.1020
	@$(MAKE) --no-print-directory $(TAILWIND)

# Fetch the platform-matching Tailwind standalone build. CI downloads its own
# copy, so this only has to cover developer machines.
$(TAILWIND):
	@mkdir -p $(dir $(TAILWIND))
	@os=$$(uname -s); arch=$$(uname -m); \
	case "$$os" in \
		Darwin) plat=macos;; \
		Linux)  plat=linux;; \
		*) echo "unsupported OS '$$os' — download Tailwind $(TAILWIND_VERSION) manually to $(TAILWIND)"; exit 1;; \
	esac; \
	case "$$arch" in \
		arm64|aarch64) cpu=arm64;; \
		x86_64|amd64)  cpu=x64;; \
		*) echo "unsupported architecture '$$arch' — download Tailwind $(TAILWIND_VERSION) manually to $(TAILWIND)"; exit 1;; \
	esac; \
	url="https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$$plat-$$cpu"; \
	echo "downloading tailwindcss $(TAILWIND_VERSION) ($$plat-$$cpu)"; \
	curl -fsSL "$$url" -o $(TAILWIND)
	@chmod +x $(TAILWIND)

## Generate Go code from .templ files.
generate:
	$(TEMPL) generate

## Compile the Tailwind CSS into the embedded static output.
css: $(TAILWIND)
	$(TAILWIND) -c tailwind.config.js -i $(CSS_INPUT) -o $(CSS_OUTPUT) --minify

## Build the static binary.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/waim

## Run locally.
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
## `make docs-version VERSION=1.3.0`. Only feature releases (X.Y.0) belong in
## the docs; CI verifies that the docs match the newest one.
docs-version:
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' \
		|| (echo "usage: make docs-version VERSION=X.Y.Z (got '$(VERSION)')" && exit 1)
	@files=$$(grep -rlE 'ghcr\.io/daknoblo/waim:v?[0-9]+\.[0-9]+\.[0-9]+' README.md docs/ || true); \
	test -n "$$files" || (echo "error: no pinned image version found in README.md or docs/" && exit 1); \
	sed -i.bak -E 's|(ghcr\.io/daknoblo/waim:)v?[0-9]+\.[0-9]+\.[0-9]+|\1$(VERSION)|g' $$files
	@find README.md docs -name '*.bak' -delete
	@echo "docs now pin $(VERSION)"

## Cut a release: regenerate assets, pin the docs (feature releases only),
## commit and create the annotated tag.
##
##   make release BUMP=patch          # test build, 1.2.1 -> 1.2.2
##   make release BUMP=minor          # feature release, 1.2.1 -> 1.3.0
##   make release VERSION=2.0.0       # explicit version
##
## The tag annotation becomes the body of the GitHub Release: $$EDITOR opens
## for it unless you pass MESSAGE="one line" or NOTES=path/to/notes.md.
## Pushing stays manual unless PUSH=1.
release: generate css
	@set -e; \
	v="$(VERSION)"; \
	if [ -n "$(BUMP)" ]; then \
		last=$$(git tag --list | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -n1); \
		[ -n "$$last" ] || last=0.0.0; \
		v=$$(echo "$$last" | awk -F. -v part="$(BUMP)" '\
			part=="major" { printf "%d.0.0", $$1+1 } \
			part=="minor" { printf "%d.%d.0", $$1, $$2+1 } \
			part=="patch" { printf "%d.%d.%d", $$1, $$2, $$3+1 }'); \
		[ -n "$$v" ] || { echo "usage: make release BUMP=major|minor|patch (got '$(BUMP)')"; exit 1; }; \
		echo "$$last -> $$v"; \
	fi; \
	echo "$$v" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' \
		|| { echo "usage: make release BUMP=major|minor|patch or VERSION=X.Y.Z (got '$$v')"; exit 1; }; \
	if git rev-parse -q --verify "refs/tags/$$v" >/dev/null; then \
		echo "error: tag $$v already exists"; exit 1; \
	fi; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "error: working tree is not clean — commit everything first"; \
		echo "(regenerated *_templ.go or app.css? CI rejects stale ones)"; \
		git status --short; exit 1; \
	fi; \
	case "$$v" in \
		*.0) $(MAKE) --no-print-directory docs-version VERSION=$$v; \
			git add README.md docs; \
			git diff --cached --quiet || git commit -q -m "docs: pin $$v";; \
		*) echo "patch release: docs keep pinning the newest feature release";; \
	esac; \
	if [ -n "$$NOTES" ]; then \
		[ -f "$$NOTES" ] || { echo "error: NOTES file '$$NOTES' not found"; exit 1; }; \
		git tag -a --cleanup=verbatim "$$v" -F "$$NOTES"; \
	elif [ -n "$$MESSAGE" ]; then \
		git tag -a --cleanup=verbatim "$$v" -m "$$MESSAGE"; \
	else \
		git tag -a "$$v"; \
	fi; \
	echo; \
	if [ -n "$(PUSH)" ]; then \
		git push origin HEAD && git push origin "$$v"; \
	else \
		echo "tag $$v created. Publish with:"; \
		echo "  git push origin main && git push origin $$v"; \
	fi

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
