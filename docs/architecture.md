# Architecture

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

waim is a single Go binary that serves a server-rendered web UI and runs
background scans. It has no external service dependencies beyond your Jellyfin
server and the TMDB API.

## Component overview

```
                +-------------------+
   Browser <--->|   HTTP server     |  templ + HTMX + Tailwind (embedded)
                |  internal/server  |
                +---------+---------+
                          |
            +-------------+--------------+
            |                            |
   +--------v--------+          +--------v--------+
   |   Scheduler     |          |   Config (JSON) |
   | internal/       |          | internal/config |
   | scheduler       |          |  + crypto (AES) |
   +--------+--------+          +-----------------+
            |
   +--------v--------+
   |    Scanner      |  comparison logic
   | internal/scanner|
   +----+-------+----+
        |       |
 +------v-+  +--v------+        +-----------------+
 |Jellyfin|  |  TMDB   |        |  Store (SQLite) |
 | client |  | client  |        | internal/store  |
 +--------+  +---------+        +-----------------+
```

## Packages

| Package               | Responsibility                                                        |
| --------------------- | --------------------------------------------------------------------- |
| `cmd/waim`            | Entry point, wiring, graceful shutdown, container healthcheck mode.   |
| `internal/config`     | Settings model, JSON load/save, transparent API-key encryption.       |
| `internal/crypto`     | Argon2id key derivation + AES-256-GCM encrypt/decrypt.                 |
| `internal/store`      | SQLite persistence (scan runs, findings, key/value) + migrations.     |
| `internal/jellyfin`   | Read-only Jellyfin API client (libraries, items, episodes).           |
| `internal/tmdb`       | TMDB API client with a client-side rate limiter.                      |
| `internal/scanner`    | Core comparison logic producing findings.                             |
| `internal/scheduler`  | Runs scans on start / interval / on demand; tracks status.            |
| `internal/server`     | HTTP routing, localisation, rendering.                                |
| `internal/web`        | templ templates, embedded static assets, view models.                 |
| `internal/i18n`       | Embedded message catalogs (en/de) and translator.                     |
| `internal/logbuf`     | In-memory ring buffer that mirrors logs into the UI.                  |
| `internal/version`    | Build metadata injected via `-ldflags`.                               |

## Data flow of a scan

1. The **scheduler** triggers a scan (startup, interval, or the *Scan now*
   button) and records a new run in the store.
2. The **scanner** asks the **Jellyfin client** for items in each enabled
   library.
3. For each movie/series it resolves a TMDB ID (provider ID first, then a
   title/year search) via the **TMDB client** (rate-limited).
4. It compares TMDB's seasons/episodes and collection parts against what is
   present in Jellyfin.
5. Findings are written to **SQLite**; the run is marked complete.
6. The **dashboard** displays the latest run's findings, status and log,
   refreshed via HTMX polling.

## Encryption model

- A non-secret random `salt` is stored in `config.json`.
- `Argon2id(WAIM_MASTER_KEY, salt)` derives a 32-byte key.
- API keys are encrypted with AES-256-GCM (random nonce per value) and stored as
  base64(`nonce` + ciphertext).
- Without `WAIM_MASTER_KEY`, encryption is disabled: existing keys cannot be
  read and new keys cannot be saved. The UI surfaces a warning.

## Persistence

- `config.json` — settings (encrypted keys).
- `waim.db` — SQLite database. Older scan runs are pruned automatically (the
  most recent runs are kept).

## Runtime & deployment

- Pure-Go SQLite driver (`modernc.org/sqlite`) means the binary is built with
  `CGO_ENABLED=0` and is fully static.
- The container uses a `distroless/static:nonroot` base, runs as non-root, and
  works with a read-only root filesystem (only the data volume is writable).
- The binary provides a `-healthcheck` mode used by the Docker `HEALTHCHECK`
  (the distroless image has no shell or curl).
