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

VERSION ?= $(shell date -u +v%Y%m%d-%H%M)
CHANNEL ?= local
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/daknoblo/waim/internal/version.Version=$(VERSION) \
	-X github.com/daknoblo/waim/internal/version.Channel=$(CHANNEL) \
	-X github.com/daknoblo/waim/internal/version.Commit=$(COMMIT) \
	-X github.com/daknoblo/waim/internal/version.Date=$(DATE)

.PHONY: all generate css build run test vet tidy tools clean docker

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
		--build-arg CHANNEL=$(CHANNEL) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t waim:$(VERSION) .

clean:
	rm -rf bin out
