# waim — What Am I Missing?

[![Demo](https://img.shields.io/badge/live-demo-6366f1?logo=githubpages&logoColor=white)](https://daknoblo.github.io/waim/)
[![CI](https://github.com/daknoblo/waim/actions/workflows/ci.yml/badge.svg)](https://github.com/daknoblo/waim/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/daknoblo/waim)](https://github.com/daknoblo/waim/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/daknoblo/waim)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![GHCR](https://img.shields.io/badge/ghcr.io-waim-blue?logo=docker)](https://github.com/daknoblo/waim/pkgs/container/waim)

> [!TIP]
> **[Try the live demo →](https://daknoblo.github.io/waim/)** — the real UI
> rendered with sample data, no server required. It is static: nothing is saved
> and no scan runs there.

> [!NOTE]
> This project is built with heavy AI assistance. Every change goes through CI,
> golangci-lint, CodeQL and Dependabot, but it is a personal side project — see
> [Disclaimer](#disclaimer).

**waim** connects to your [Jellyfin](https://jellyfin.org/) server, reads your
movies and series, and compares them against
[The Movie Database (TMDB)](https://www.themoviedb.org/) to tell you **what you
are missing**:

- 📺 **Series** — whole seasons that are absent, or individual episodes missing
  from a season you already have.
- 🎬 **Movies & collections** — missing predecessors/sequels or other entries of
  a collection (e.g. owning *The Lord of the Rings* part 1 & 3 but not part 2).

It is a small, single-binary Go application with a modern, server-rendered web
UI (templ + HTMX + Tailwind), built to run in Docker.

The main motivation was the lack of a simple UI to track which episodes or
movies are missing, without extra features — a lightweight alternative to
Huntarr or Missingarr.

---

## Features

- Read-only Jellyfin integration (your library is never modified).
- TMDB matching that prefers Jellyfin's stored provider IDs and falls back to a
  title/year search.
- Detects missing seasons, missing episodes and missing collection entries.
- Periodic scans (configurable interval), scan-on-startup and a manual
  **Scan now** button.
- Per-library selection: choose exactly which Jellyfin libraries to scan.
- Dashboard with grouped findings, sortable columns, a live search box and a
  per-library quick filter.
- **Statistics** page: completeness per library, most incomplete series and
  collections, top/lowest rated titles per library — in separate sections for
  owned media and for missing ones (movies *and* series, so you can decide
  what's worth getting), each expandable up to 50 entries.
- **Coming up** section: announced episodes of the series you own and
  unreleased entries of your movie collections, on a release timeline plus a
  poster grid grouped by timeframe. Collected during the regular scan, so it
  costs no extra TMDB requests.
- General statistics: library facts (total runtime, average rating, episode
  completeness, specials share), biggest sagas and series binge times,
  genre/decade donut charts, rating distribution, original language and
  production country, average rating per genre, growth per scan, a season
  completion heatmap and a sankey chart of episodes per season for any series.
- **Suggestions** page: what to watch next from TMDB trending and
  recommendations, upcoming releases matching your most-watched genres and
  theatrical/on-air releases for your region, with optional AI-generated picks.
- Optional **AI suggestions** via any OpenAI/Azure-compatible chat endpoint.
- Configurable TMDB request rate limit.
- Local TMDB response cache with an incremental background refresher (spread
  across the day) and a nightly cleanup of orphaned entries, so scans and
  suggestions reuse data instead of re-loading everything from TMDB.
- Settings stored as JSON in the data directory; **API keys are encrypted at
  rest** (AES-256-GCM with a key generated on first start).
- Export of settings (keys stay encrypted, never plaintext) and of the current
  sync state.
- Bilingual UI (English / German) with an in-app language switch.
- **Responsive layout**: on phones the navigation collapses into a menu button
  and wide tables turn into stacked cards.
- Activity log and live scan status in the dashboard.
- Multi-arch images published to GitHub Container Registry.

## Screenshots

Prefer clicking around? The **[live demo](https://daknoblo.github.io/waim/)**
serves these pages with sample data.

|  |  |
| :--: | :--: |
| **Dashboard** | **Statistics** |
| [![Dashboard](docs/images/dashboard.png)](docs/images/dashboard.png) | [![Statistics](docs/images/statistics.png)](docs/images/statistics.png) |
| **Suggestions** | **Settings** |
| [![Suggestions](docs/images/suggestions.png)](docs/images/suggestions.png) | [![Settings](docs/images/settings.png)](docs/images/settings.png) |
| **Activity log** | **About** |
| [![Activity log](docs/images/logs.png)](docs/images/logs.png) | [![About](docs/images/about.png)](docs/images/about.png) |

## Quick start (Docker Compose)

```bash
# 1. Grab the example compose file.
curl -fsSL https://raw.githubusercontent.com/daknoblo/waim/main/deploy/docker-compose.example.yml -o docker-compose.yml

# 2. Start it.
docker compose up -d
```

Then open <http://localhost:8080>, go to **Settings**, and enter your Jellyfin
URL + API key and your TMDB API key. Use **Refresh libraries from Jellyfin** to
load your libraries, select the ones to scan, and save.

> **Upgrading from 1.3 or older?** `WAIM_MASTER_KEY` was removed and the
> encryption key is now generated automatically, so the stored API keys have to
> be entered once more. See
> [Upgrading](docs/installation.md#upgrading-from-13-or-older).

### Image tags

| Tag                            | Source        | Purpose                       |
| ------------------------------ | ------------- | ----------------------------- |
| `ghcr.io/daknoblo/waim:latest` | `main`        | Current build of `main`       |
| `ghcr.io/daknoblo/waim:X.Y.Z`  | git tag       | Pinned versions               |
| `ghcr.io/daknoblo/waim:X.Y`    | git tag       | Latest patch of a minor line  |
| `ghcr.io/daknoblo/waim:sha-…`  | every commit  | Exact commit, for rollbacks   |

## Configuration

All runtime configuration is done in the web UI and persisted to `config.json`
in the data directory. Only a few environment variables are needed:

| Variable          | Default        | Description                                           |
| ----------------- | -------------- | ----------------------------------------------------- |
| `WAIM_ADDR`       | `:8080`        | Listen address.                                       |
| `TZ`              | `Etc/UTC`      | Timezone (IANA name) for timestamps and log display.  |

The data directory is fixed at `/appdata` inside the container (mount a
volume there to persist it). All other configuration lives in the web UI.

See [docs/configuration.md](docs/configuration.md) for the full settings
reference.

## Security notes

- **No built-in authentication.** waim is meant to run on a trusted network or
  behind a reverse proxy / VPN that provides access control. Do not expose it
  directly to the internet.
- **API keys are encrypted at rest** with AES-256-GCM. The key is generated on
  first start and stored as `master.key` in the data directory, so a backup of
  that directory is as sensitive as the keys themselves. Losing the file means
  the stored API keys have to be entered again.
- The container runs as a non-root user with a read-only root filesystem and a
  minimal distroless base image.

## Documentation

- [Installation](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)

## Contributing

Bug reports, ideas and pull requests are welcome — see
[Contributing](.github/CONTRIBUTING.md) for the development setup and the
conventions this project follows. Participation is governed by the
[Code of Conduct](.github/CODE_OF_CONDUCT.md).

Found a security problem? Please report it privately as described in the
[Security Policy](.github/SECURITY.md) rather than in a public issue.

## Tech stack

Go · [templ](https://templ.guide/) · [HTMX](https://htmx.org/) ·
[Tailwind CSS](https://tailwindcss.com/) ·
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (pure-Go, CGO-free) ·
Docker (distroless) · GitHub Actions.

## Disclaimer

This project is developed with heavy AI assistance and validated by CI,
golangci-lint, CodeQL and Dependabot on every change. It is still a personal
side project rather than a supported product: it talks to your Jellyfin server
in read-only mode and to the TMDB API, and it is provided as-is, without
warranty — see [LICENSE](LICENSE).


## License

Released under the [MIT License](LICENSE).
