# Configuration

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

All settings are managed in the web UI (**Settings** page) and persisted to
`config.json` inside the data directory. API keys are **always stored
encrypted** and never written in plaintext.

## `config.json` schema

```jsonc
{
  "schemaVersion": 1,
  "salt": "<base64>",          // non-secret salt for key derivation
  "locale": "en",              // default UI language: "en" or "de"
  "logLevel": "info",          // log verbosity: "info", "warn" or "debug"
  "jellyfin": {
    "url": "https://jellyfin.example.com",
    "apiKeyEnc": "<base64>",   // AES-256-GCM ciphertext (never plaintext)
    "userId": ""               // optional; auto-resolved if empty
  },
  "tmdb": {
    "apiKeyEnc": "<base64>",   // AES-256-GCM ciphertext (never plaintext)
    "language": "en-US",
    "region": "US"
  },
  "scan": {
    "intervalMinutes": 60,     // 0 disables periodic scans (manual only)
    "runOnStart": true,        // scan once on container startup
    "tmdbRateLimitRps": 4,     // TMDB requests per second
    "includeSpecials": false   // include season 0 / specials in comparisons
  },
  "libraries": [
    { "id": "...", "name": "Movies", "type": "movies", "enabled": true }
  ]
}
```

## Settings reference

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

### Scanning

| Field                  | Description                                                              |
| ---------------------- | ------------------------------------------------------------------------ |
| Scan interval (minutes) | How often to scan automatically. `0` means manual scans only.           |
| Run a scan on startup  | Trigger one scan when the container starts.                              |
| TMDB requests per second | Client-side rate limit for TMDB API calls.                            |
| Include specials (season 0) | When enabled, specials are included in the comparison; off by default. |

### Libraries

Use **Refresh libraries from Jellyfin** to load your current libraries, then tick
the ones you want included in scans. Only enabled libraries are scanned.

### Interface language

Switch between English and German. The choice is stored per browser (cookie) and
the default is taken from `config.json`.

### Logging

The **Log level** setting controls how verbose both the in-app activity log and
the console (container) output are. It is applied immediately on save:

| Level   | Shows                                              |
| ------- | -------------------------------------------------- |
| `info`  | Normal operation (default).                        |
| `warn`  | Warnings and errors only.                          |
| `debug` | Verbose, detailed diagnostics (per-request, etc.). |

`WAIM_DEBUG=true` only sets the verbosity during early startup before the config
is loaded; afterwards the value from `config.json` takes precedence.

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

## Exports

- **Export settings** — downloads `config.json` with API keys still encrypted
  (or omitted if encryption is disabled). Plaintext keys are never exported.
- **Export sync state** — downloads the latest successful scan and its findings
  as JSON. This contains no secrets.
