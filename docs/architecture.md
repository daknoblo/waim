# Architecture

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

waim is a single Go binary that serves a server-rendered web UI and runs
background scans. TMDB is required for metadata; Jellyfin instances are optional.

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
   | Catalog/Scanner |  source snapshots + comparison logic
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
| `internal/crypto`     | Key file handling + AES-256-GCM encrypt/decrypt.                       |
| `internal/httpx`      | Shared upstream HTTP behaviour: redirect policy and error sanitising.  |
| `internal/store`      | SQLite persistence (scan runs, findings, key/value) + migrations.     |
| `internal/jellyfin`   | Read-only Jellyfin API client (libraries, items, episodes).           |
| `internal/media`      | Credential-free instances, occurrences, qualified identities and global episode union. |
| `internal/source`     | Small adapter factory, atomic snapshot acquisition, catalog loading and shared identity resolution. |
| `internal/tmdb`       | TMDB API client with a client-side rate limiter.                      |
| `internal/ai`         | OpenAI/Azure-compatible chat client for AI suggestions.               |
| `internal/scanner`    | Core comparison logic producing findings.                             |
| `internal/scheduler`  | Runs scans on start / interval / on demand; tracks status.            |
| `internal/suggest`    | Builds watch suggestions from TMDB and the AI client.                 |
| `internal/server`     | HTTP routing, localisation, rendering.                                |
| `internal/web`        | templ templates, embedded static assets, view models.                 |
| `internal/i18n`       | Embedded message catalogs (en/de) and translator.                     |
| `internal/logbuf`     | In-memory ring buffer that mirrors logs into the UI.                  |
| `internal/version`    | Build metadata injected via `-ldflags`.                               |

## Data flow of a scan

1. The **scheduler** triggers a scan (startup, interval, or the *Scan now*
   button) and records a new run in the store.
2. **source.Catalog** invokes adapters for all enabled real instances. A complete
   successful snapshot replaces that instance's previous one atomically. Failures
   retain same-identity data with stale warnings; first-fetch failures are unknown.
   Saved snapshots and current virtual entries form the normalized catalog.
3. The source layer resolves each movie/series TMDB ID (provider ID first, then a
   unique exact title/year search) via the **TMDB client** (shared process-wide rate
   budget). Verified resolutions are stored in the source snapshot before catalog
   evaluation. Live views and recommendations therefore reuse the same identity
   and episode union without repeating a search.
4. It compares TMDB's seasons/episodes and collection parts against what is
   present in Jellyfin. Episodes and collection parts that have not been
   released yet are recorded as *upcoming* releases instead of gaps — they come
   from the same TMDB responses, so this costs no additional requests.
5. Findings, metadata, summaries and success are committed in one transaction,
   conditional on the virtual revision still matching. Source revisions are
   checked before publishing. Changes during work queue a follow-up computation.
6. The **dashboard** displays the latest run's findings, status and log,
   refreshed via HTMX polling.

The **statistics** and **suggestions** pages are derived from the same persisted
scan data. Suggestions additionally query TMDB (trending, recommendations and
the discover endpoints for upcoming titles) and, when enabled, the configured AI
endpoint. Their real-owned set comes from the same saved catalog/resolution,
never from a separate Jellyfin scan. Purely watched titles remain recommendable.

## Recalculation and current membership

Virtual mutations persist immediately and queue a coalesced scheduler job.
Recalculation reads snapshots/TMDB cache, without a Jellyfin request. Title
actions/badges read current membership even while derived results are pending.
Source disablement, removal and identity changes cannot lend ownership or links
to current views. Series episodes are unioned by season/episode, movie and series
TMDB IDs occupy different namespaces, and unresolved items remain source-local.
Collection parts and individually watched movies share missing/release units.
Resolved identities are bound to the source fingerprint plus the original local
ID, media type, title, year and provider IDs. A refresh preserves an alias only
when those inputs still match; address changes and local-ID reuse cannot inherit
an unrelated identity. The aliases live in the latest snapshot, not an unbounded
history. Snapshots created before this binding was introduced acquire bindings
on their next scan or recalculation.
The source fingerprint hashes only public identity metadata, including a
manager-owned credential-generation token rather than API-key material.
Credential replacement and readability changes advance that persisted token;
ordinary source renames do not invalidate inventory.

Watch-only titles have ratings and gap/release evaluations but contribute no
owned count, runtime or growth. Source/library memberships overlap; global owned
counts do not. New growth points use `owned-v1` and complete real refresh jobs only;
legacy runs remain readable but are not synthetic source snapshots or directly
comparable growth points. Legacy and incomplete states are visibly labelled.
Per-library gap titles and rating indexes use distinct provenance memberships;
shared titles appear once in each participating library, but once globally.
Retention independently keeps the latest 20 result snapshots and the latest 20
complete refresh runs (at most 40 runs). Repeated virtual recalculations cannot
erase real refresh growth history.

Scheduler, refresher and suggestion workers are cancelled/joined before SQLite
closes. No plugin framework, Plex/Emby implementation or media write API is added.

## Encryption model

- A random 32-byte key is generated on first start and stored as `master.key`
  in the data directory (owner-only permissions, never overwritten).
- The Jellyfin, TMDB and AI API keys are each encrypted with AES-256-GCM (random
  nonce per value) and stored as base64(`nonce` + ciphertext).
- The key lives next to `config.json`, so this protects a leaked or exported
  config file — not an attacker with access to the data directory. Treat backups
  of that directory as secrets.
- If the key file is missing while `config.json` still holds encrypted values, a
  new key is generated, the stale values are reported as unreadable in the UI
  and the API keys must be entered again. A malformed key file is a hard startup
  error instead: replacing it would destroy the stored keys.

## Persistence

- `config.json` — settings (encrypted keys).
- `master.key` — generated encryption key.
- `waim.db` — SQLite database. Older scan runs are pruned automatically (the
  most recent runs are kept).
- `source_snapshots` — one last-success payload per source plus attempt/success,
  safe failure text and identity fingerprint (including configuration boundary).
- `virtual_entries` / `catalog_revision` — unique `(media_type, tmdb_id)` watch
  membership and monotonic mutation revision.
- Scan metadata records basis, refresh/recompute mode, source token, virtual
  revision and warnings. Findings and media/release payloads retain provenance.

## Runtime & deployment

- Pure-Go SQLite driver (`modernc.org/sqlite`) means the binary is built with
  `CGO_ENABLED=0` and is fully static.
- The container uses a `distroless/static:nonroot` base, runs as non-root, and
  works with a read-only root filesystem (only the data volume is writable).
- The binary provides a `-healthcheck` mode used by the Docker `HEALTHCHECK`
  (the distroless image has no shell or curl).
