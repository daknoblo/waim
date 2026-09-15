# Configuration

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

Global settings are managed on **Settings**, and instances on **Media sources**, persisted to
`config.json` inside the data directory. API keys are **always stored
encrypted** and never written in plaintext. The encryption key is generated on
first start and kept as `master.key` next to `config.json`.

No manually supplied encryption key is required. Later starts reuse the same
key file, including after image updates when the same data volume is mounted.
AES-256-GCM protects the stored Jellyfin, TMDB and AI API keys, including their
configuration exports. Other settings and the SQLite sync database are not
encrypted by this mechanism. Back up the complete data volume and protect it:
the key file allows the stored credentials to be decrypted.

## `config.json` schema

```jsonc
{
  "schemaVersion": 3,
  "locale": "en",              // default UI language: "en" or "de"
  "logLevel": "info",          // log verbosity: "info", "warn" or "debug"
  "sources": [
    {
      "id": "stable-instance-id",
      "type": "jellyfin",
      "name": "Living room",
      "enabled": true,
      "revision": 1,
      "credentialGeneration": "<manager-generated public token>",
      "jellyfin": {
        "url": "https://jellyfin.example.com",
        "apiKeyEnc": "<base64>",
        "userId": ""
      },
      "libraries": [
        { "id": "...", "name": "Movies", "type": "movies", "enabled": true }
      ]
    },
    { "id": "virtual", "type": "virtual", "name": "Virtual collection", "enabled": true, "revision": 0 }
  ],
  "tmdb": {
    "apiKeyEnc": "<base64>",   // AES-256-GCM ciphertext (never plaintext)
    "language": "en-US",
    "region": "US"
  },
  "ai": {
    "enabled": false,          // enable AI-generated suggestions
    "endpoint": "",            // full chat-completions URL (OpenAI/Azure-compatible)
    "apiKeyEnc": "<base64>",   // AES-256-GCM ciphertext (never plaintext)
    "model": ""                // model / deployment name
  },
  "scan": {
    "intervalMinutes": 60,     // 0 disables periodic scans (manual only)
    "runOnStart": true,        // scan once on container startup
    "tmdbRateLimitRps": 1,     // TMDB requests per second
    "includeSpecials": false,  // include season 0 / specials in comparisons
    "episodeRatings": false    // collect per-episode ratings for the statistics
  },
  "cache": {
    "refreshEnabled": true,        // run the background TMDB cache refresher
    "refreshIntervalMinutes": 15,  // minutes between refresh batches
    "refreshPercent": 1,           // percent of oldest entries refreshed per batch
    "cleanupEnabled": true,        // prune orphaned entries once a night
    "cleanupMaxAgeDays": 30        // remove entries unused for this many days
  }
}
```

## Settings reference

![Settings page](images/settings.png)

Global changes are saved as soon as you leave a field, and switches and dropdowns take
effect right away — there is no save button. Whenever a connection setting
changes, that section is tested immediately and the result appears underneath
it.

Each source has a separate explicit save form and revision. An outdated form is
rejected instead of overwriting another edit. Changing a Jellyfin address
(including its path) requires re-entering the key and clears its library choices.
Blank keys otherwise retain the saved value. Test/refresh buttons use **saved**
settings, not unsaved form fields. AI host changes also require a key.

Adding a Jellyfin source immediately fetches its available libraries. They start
unselected so you can choose what to scan. If discovery fails, the saved source
is retained with a visible retry message; do not add it again. A later manual
refresh preserves existing library selections. Source removal is the red,
right-aligned action alongside refresh/test and still requires confirmation.

### Migration and snapshot identity

Legacy schema 2 configuration migrates once to `jellyfin-default`, preserving
user, library selection and encrypted key bytes. Legacy global Jellyfin settings
are no longer a runtime configuration path. Losing `master.key` leaves unreadable
ciphertexts intact through migration, export and unrelated saves; each source
shows its warning until its key is replaced.

Disabling or removing a source excludes its snapshot immediately. Changing the
address, user, key or enabled libraries changes the snapshot identity; the old
snapshot is never reused under the new identity. A failed same-identity refresh
keeps the last good snapshot and marks it stale. Without any successful snapshot
inventory is **unknown**, not empty, and gaps/completion are unconfirmed.

Source fingerprints contain a non-secret `credentialGeneration`, never the API
key or a hash of it. The manager generates and persists this token; submitted
tokens are ignored on all save paths. Changing or explicitly replacing a key,
including restoring an earlier key, creates a fresh generation. Renames and
ordinary encryption-at-rest rewrites retain it. A persisted `keyUnreadable` flag
records the last observed readability state so losing/restoring `master.key`
also advances the generation once, without deleting unreadable ciphertext.

Existing configs acquire generation tokens automatically without rewriting
encrypted keys. Snapshots created with the previous key-derived fingerprint need
one successful real-source refresh after this upgrade; until then their inventory
is reported as unknown. No credential-derived fingerprint fallback is used.

The reserved virtual source is always enabled and cannot be deleted. Its entries
live in SQLite, not the config export. Use the sync export for evaluated metadata.
Collection search returns at most the first 20 TMDB matches; refine your query
if a desired result is not on that page. Add/remove is idempotent and only edits
watch membership. No media-server files are written.

Dashboard findings show their source references in the Library column as
`type / server URL / library`, with one label per source/library membership.
Virtual entries use a localized virtual-collection label. Collection origins
are represented by these labels rather than a separate context link below
the title. Individual missing movie parts do not offer virtual-collection actions;
use collection search when intentionally adding a title to the virtual collection.

### Jellyfin

| Field    | Description                                                                 |
| -------- | --------------------------------------------------------------------------- |
| Server URL | Base URL of your Jellyfin server, e.g. `https://jellyfin.example.com`.     |
| API key  | Created under Jellyfin → Dashboard → API Keys. Used read-only.               |
| User ID  | Optional. If empty, the first available user is used for library queries.   |

### TMDB

| Field           | Description                                                            |
| --------------- | --------------------------------------------------------------------- |
| API key / token | A TMDB v3 API key **or** a v4 read access token. Format auto-detected. |
| Metadata language | TMDB language code, e.g. `en-US`, `de-DE`.                           |
| Region          | Optional region code used to bias search results, e.g. `US`, `DE`.    |

> **Token format detection:** a credential starting with `eyJ` (a JWT) is sent
> as a `Bearer` token (v4); anything else is sent as the `api_key` query
> parameter (v3). You only ever need to paste the single key TMDB gives you.

### AI suggestions (optional)

On the **Suggestions** page waim can ask an OpenAI/Azure-compatible chat endpoint
for extra recommendations based on your library. This is entirely optional and
turned off by default.

| Field                 | Description                                                       |
| --------------------- | ---------------------------------------------------------------- |
| Enable AI suggestions | Master switch for the AI integration.                            |
| Endpoint URL          | The full chat-completions URL (e.g. an Azure AI Foundry deployment). |
| API key               | Stored encrypted, like the Jellyfin and TMDB keys.               |
| Model                 | Model / deployment name to request.                              |

### Scanning (all active real sources)

When and how waim reads your Jellyfin libraries.
Before a TMDB key is configured, the UI shows one central setup notice linking
to Settings. Scanning waits for setup and the manual scan button is disabled;
missing initial configuration is not recorded as a failed scan. Actual scan
errors remain visible in the scan status.

| Field                  | Description                                                              |
| ---------------------- | ------------------------------------------------------------------------ |
| Scan interval (minutes) | How often a scan starts automatically. One scan is a single pass over all enabled libraries; `0` means only the *Scan now* button starts one. |
| Run a scan on startup  | Trigger one scan when the container starts.                              |
| Include specials (season 0) | When enabled, specials count as gaps and appear in the statistics; off by default. |

### TMDB requests & data refresh

Everything that talks to TMDB. Responses are cached locally in `waim.db`
(`tmdb_cache` table), so scans and suggestions reuse data instead of re-fetching
everything. A background job keeps the cache fresh by re-fetching the oldest
entries first, and a nightly cleanup (03:00) prunes entries no longer used by any
scan or suggestion.

| Field                          | Description                                                                                          |
| ------------------------------ | ---------------------------------------------------------------------------------------------------- |
| TMDB requests per second       | Upper bound for **all** TMDB calls together (scans, suggestions, background refresh). The settings page previews the resulting requests per minute and hour while you type. |
| Collect episode ratings        | Loads every season of every series from TMDB so the statistics page can show the episode rating heatmap and exact series runtimes. The first scan takes noticeably longer (one request per season, bounded by the rate limit); afterwards the responses come from the local cache. Off by default. |
| Refresh cached TMDB data       | Master switch for the background refresher.                                                          |
| Refresh interval (minutes)     | How often a refresh batch runs.                                                                      |
| Share refreshed per run (%)    | Percentage of the oldest cache entries re-fetched each run. The defaults (1% every 15 min) spread a full refresh across the day. The settings page previews the request volume and how long a full refresh takes. |
| Remove orphaned entries nightly | Master switch for the nightly cleanup.                                                              |
| Remove entries unused for (days) | Cache entries not requested by any scan or suggestion for this many days are deleted (e.g. media removed from the library). |

### Libraries

On each source, use **Refresh libraries from Jellyfin**, select libraries and
**Save source**. Only enabled instances/libraries are scanned. Namespaced library
and source filters select title provenance; they do not recompute a different
local missing inventory.

### Interface language

Switch between English and German. The choice is stored per browser (cookie) and
the default is taken from `config.json`.

### Logging

The **Log level** setting controls how verbose both the in-app activity log and
the console (container) output are. It is applied immediately:

| Level   | Shows                                              |
| ------- | -------------------------------------------------- |
| `info`  | Normal operation (default).                        |
| `warn`  | Warnings and errors only.                          |
| `debug` | Verbose, detailed diagnostics (per-request, etc.). |

The log level is configured only here — there is no environment variable for it.
Until `config.json` is loaded at startup, waim logs at `info` level.

## Matching logic

- For each Jellyfin movie/series, waim prefers the **TMDB provider ID** stored
  by Jellyfin. If none is present, it falls back to a TMDB **title + year**
  search and uses the best match.
- **Series:** the TMDB season list is compared against the episodes present in
  Jellyfin. A season with no local episodes is reported as a *missing season*;
  a partially present season is reported as *missing episodes*. Only episodes
  that have already aired are counted.
- **Movies/collections:** if a movie belongs to a TMDB collection, waim lists
  the collection's parts that you do not own (and that have already been
  released).

## The "coming up" section

The statistics page shows releases for titles that are already in your library,
in two directions:

- **Coming up** (default) — episodes and collection entries that are announced
  but not released yet.
- **Already released** — what came out while your library still does not have
  it. This is derived from the gaps of the latest scan, so an entry disappears
  by itself once the title shows up in Jellyfin.

Both directions share the timeframe and media type filters; in the
retrospective a timeframe of 90 days means the *last* 90 days.

Release dates for the retrospective are recorded from version 1.4.1 on. Right
after the update the view therefore stays empty and says so — run a scan once
to fill it.

## Exports

- **Export settings** — downloads `config.json` with API keys still encrypted.
  Plaintext keys are never exported, so the file is only usable on an instance
  that has the matching `master.key`.
- **Export sync state** — downloads the latest successful scan and its findings
  as JSON. This contains no secrets.
