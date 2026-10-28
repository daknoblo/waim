# Configuration

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

Global settings and media instances are managed in the four **Settings** tabs, persisted to
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
      "scanIntervalMinutes": 60, // this source only; 0 = manual only
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
    "intervalMinutes": 60,     // legacy migration/new-source default; no global periodic timer
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

| Tab | Contents |
| --- | --- |
| Media management (`/settings?tab=media`) | Source tiles/dialogs, per-source libraries and scan intervals, shared startup/specials. |
| Metadata (`?tab=metadata`) | Provider tiles: TMDB opens credentials, shared rate limit, episode ratings and cache settings in its own dialog. IMDb is an unavailable placeholder. AI recommendations remain a separate section below. |
| Interface (`?tab=interface`) | UI language plus metadata language and region. |
| Other (`?tab=other`) | Database size including WAL/SHM, configuration size, data directory, cache count, exports, log level and Danger Zone. |

Old `/sources` bookmarks redirect to the media tab; source POST endpoints remain
available. The virtual collection remains a separate main-navigation page.

Global changes are saved as soon as you leave a field. **Save changes** is shown
only as a no-JavaScript fallback; failed automatic saves offer a retry button.
Only the active section is submitted. Other tabs'
fields, including checkboxes and credentials, are preserved atomically.
Tab navigation waits for pending autosave to finish, and remains on the draft
if saving fails or a replacement key is required. Validation errors retain
non-secret input; password fields are deliberately never echoed back.
Connection edits still probe only the relevant provider. Language changes and
source feedback preserve the active settings tab.

Each existing source has an independent autosave form and revision. An outdated form is
rejected instead of overwriting another edit. Changing a Jellyfin address
(including its path) requires re-entering the key and clears its library choices.
Blank keys otherwise retain the saved value. Test/refresh buttons use **saved**
settings, after waiting for pending saves. AI host changes also require a key.

The source overview displays two large tiles per row on wider screens and one
per row on phones. The virtual collection is always first; it opens the existing
collection page and has no media-server scan timer. Real-source tiles open an
accessible dialog containing connection settings, library selection, the
source's scan interval, a source-only scan button, library refresh, connection
test and confirmed removal. Dialog links also work without JavaScript.
The **Add source** button alongside the Media sources heading opens a dialog with a provider
dropdown. Jellyfin is selectable; Emby and Plex are disabled as not yet available.
Provider badges are purple for Jellyfin, green for Emby and gold for Plex.
Desktop dialogs are wide enough for the scan/refresh/test/remove actions to fit
next to each other; on mobile the actions wrap and the dialog scrolls.

Existing source edits save after field changes, with overlapping edits queued
against the last acknowledged revision. Closing the dialog (button, Escape or
backdrop click), navigating away or invoking an action waits for saving to finish.
A failure keeps the source dialog and its draft open; errors are explicit and
can be retried. New sources are still created only through **Add source** and
closing an unfinished creation asks before discarding it. Removal still requires
explicit confirmation. Passwords are never returned by save responses, and
acknowledged password inputs are cleared without losing newer edits.
Manual Save buttons remain available only when JavaScript is disabled.

Adding a Jellyfin source immediately fetches its available libraries. They start
unselected so you can choose what to scan. If discovery fails, the saved source
is retained with a visible retry message; do not add it again. A later manual
refresh preserves existing library selections. Source removal is the red,
right-aligned action alongside refresh/test and still requires confirmation.

## Danger Zone

All reset operations require a dedicated POST, a checked confirmation and the
exact text `RESET`. Unknown scopes and stale forms are rejected. Resets are
**not cancellation commands**: ongoing scans, cache refresh/cleanup, suggestions,
source discovery/tests, saves and data-serving requests prevent admission.
Busy responses return HTTP 409 and require a retry when idle. Workers arriving
during maintenance are skipped, not held until reset finishes. Queued scans and
timer ticks from before/during the reset are discarded; future scheduled work
or explicit user actions can load data again.

| Scope | Deleted | Retained |
| --- | --- | --- |
| Metadata | TMDB response cache; scan history, findings, evaluated media metadata and upcoming results; in-memory suggestions; derived TMDB aliases in source snapshots. | Sources, credentials, raw inventory and its native provider IDs/episode ownership; virtual membership and its saved display title/year/poster. |
| Imported media | Imported source snapshots, scan history/results and in-memory suggestions. Sources become **unknown**, not empty. | Sources, credentials, virtual entries and TMDB response cache. |
| Factory | All real-source configuration and credentials, virtual entries, snapshots, cache, scan results, UI preferences and retained activity/log entries. | Exactly one enabled empty **Virtual collection**, default settings, `master.key`, the database file, migration/reset counters and the persistent data directory. |

Metadata reset clears `ResolvedTMDBID` and `ResolutionInput` aliases, including
episode aliases, without altering raw names, provider IDs, references, inventory
timestamps or source warnings. It does not silently claim the retained inventory
has just been verified. None of the resets immediately queues a refetch.

These are logical application resets, not secure disk erasure. Existing backups,
external log sinks and recoverable free space on storage are not wiped. No files
or media on Jellyfin servers are modified. Do not delete the mounted volume or
`master.key` to perform a reset.

### Factory reset recovery

SQLite deletion and a small `reset_state.factory_pending` journal flag commit
together, with full synchronous durability, **before** replacing `config.json`
with defaults. The configuration file and its directory are flushed before the
completion flag is cleared. A database transaction failure rolls back and does
not overwrite configuration or keys.

Marker completion itself uses a pinned `synchronous=FULL` transaction, including
after reopening the database during startup recovery. This prevents an
acknowledged completion from relying on the reopened connection's weaker normal
write mode before new user settings can be saved.

If the database commits but configuration persistence or completion fails, waim
reports failure and blocks data access/work (HTTP 503). Correct the storage
problem and retry the confirmed factory reset, or restart. Startup recovery
finishes the pending configuration reset idempotently before starting workers
or HTTP; it does not repeat database deletion. Failed recovery prevents startup.
The encryption key is always reused. Factory-reset generations also invalidate
old language cookies in other browsers.

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

Jellyfin inventory fingerprints also include an episode-normalization version.
The first version explicitly requests `IsMissing=false`, rejects missing/virtual
placeholders defensively, and expands combined episode files using
`IndexNumberEnd` before the season/episode ownership union. A combined E01–E02
file and another source's E02 therefore represent two owned episodes, not three.
Ranges are limited to 1,000 episode numbers per physical item. An invalid,
reversed or larger range retains only a valid starting episode and adds a warning;
episodes without a valid season/start are omitted with an explicit warning.

Old Jellyfin snapshots lack this normalization guarantee and are excluded once
after this upgrade: inventories become **unknown** until a successful full scan
(startup, scheduled, or **Scan now**). This is not a silent repair of old episode counts. The upgrade itself
does not delete the previous snapshot; the next refresh attempt follows the
normal fingerprint rules, so a failed attempt cannot reuse its incompatible
payload. Configured sources, credentials and all virtual-collection entries are
retained. Historical scans are not rewritten. Old derived scans remain pending or
unconfirmed until their inputs have been refreshed/recomputed. Source-native
provider IDs remain authoritative; incompatible old snapshot aliases are resolved
again from the fresh inventory when needed.

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
`type · instance name · library`, with one single-line label per source/library
membership. The configured instance name is shown rather than its server URL;
the underlying media links are unchanged.
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

In **Settings → Metadata**, provider tiles use locally served wordmark graphics
and the same two-column layout as media sources. **Open source** on the TMDB tile
opens its autosaving dialog; the key status on the tile updates after saving.
Save feedback and retry controls are visible inside the dialog. Closing via the
button, Escape or backdrop waits for pending saves; failures preserve the edits.
The optional AI settings remain below the tile grid and are not part of the
TMDB dialog. Metadata language and region are still configured under Interface.
IMDb is only a clearly labeled future-provider placeholder: no IMDb requests,
credentials, or enabled configuration actions are introduced.

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

### Scanning (independent media sources)

When and how waim reads your Jellyfin libraries.
New installations show separate setup cards for **Metadata** and **Media
sources**, each linking to its settings tab. The metadata card requests a
missing TMDB key. The media card distinguishes an absent/incomplete server
connection from an unselected library list, and offers the virtual collection
as an alternative. A configured source with selected libraries, or existing
virtual entries, satisfies the media-input requirement; a media server is not
mandatory. Each card disappears independently as its requirement is fulfilled.
These checks read saved configuration only and do not make connection probes.
Without a TMDB key, scanning waits for setup and the manual scan button is
disabled; missing initial configuration is not recorded as a failed scan.
Actual scan errors remain visible in the scan status.

| Field                  | Description                                                              |
| ---------------------- | ------------------------------------------------------------------------ |
| Scan interval (minutes, per source) | How often this source's enabled libraries are scanned. `0` disables periodic scans for this source; allowed range is 0–525600. Other sources keep their schedules. |
| Run a scan on startup  | Trigger one scan when the container starts.                              |
| Include specials (season 0) | When enabled, specials count as gaps and appear in the statistics; off by default. |

When an older source has no interval of its own, its previous global interval is
copied automatically and persisted without replacing credentials or snapshots.
An interval-only edit does not change inventory identity. Source-specific and
scheduled scans refresh only the selected/due sources, then evaluate the combined
catalog using the other saved inventories. The dashboard **Scan now** still
refreshes all active sources; its next-scan time is the earliest individual
deadline. Recalculation and another source's refresh do not postpone unchanged
sources. Startup scanning remains a shared setting.

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
