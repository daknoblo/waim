# syntax=docker/dockerfile:1

# ---- Build stage ----------------------------------------------------------
ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder

# git is needed for module metadata; ca-certificates for HTTPS module fetches.
RUN apk add --no-cache ca-certificates git

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the sources (generated *_templ.go and app.css are committed).
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Build a fully static, CGO-free binary (modernc.org/sqlite is pure Go).
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/daknoblo/waim/internal/version.Version=${VERSION} \
      -X github.com/daknoblo/waim/internal/version.Commit=${COMMIT} \
      -X github.com/daknoblo/waim/internal/version.Date=${DATE}" \
    -o /out/waim ./cmd/waim

# Pre-create the data directory so a freshly mounted named volume inherits the
# correct (non-root) ownership.
RUN mkdir -p /out/appdata

# ---- Runtime stage --------------------------------------------------------
# distroless/static: no shell, no package manager, minimal attack surface.
FROM gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /out/waim /app/waim
COPY --from=builder --chown=65532:65532 /out/appdata /app/appdata

ENV WAIM_DATA_DIR=/app/appdata \
    WAIM_ADDR=:8080

EXPOSE 8080
VOLUME ["/app/appdata"]

# Run as the built-in non-root user provided by the distroless image.
USER nonroot:nonroot

# The binary implements its own healthcheck (distroless has no curl/wget).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/waim", "-healthcheck"]

ENTRYPOINT ["/app/waim"]
