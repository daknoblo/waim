# Installation

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

waim is distributed as a multi-arch container image (`linux/amd64`,
`linux/arm64`) on the GitHub Container Registry.

## Requirements

- A running [Jellyfin](https://jellyfin.org/) server and an API key.
- A [TMDB](https://www.themoviedb.org/settings/api) API key (a v3 key or a v4
  read access token both work — the format is auto-detected).
- Docker / Docker Compose (or any OCI runtime).

## Pulling the image

```bash
docker pull ghcr.io/daknoblo/waim:latest   # current build of the main branch
docker pull ghcr.io/daknoblo/waim:1.4.0   # a specific version tag
```

## Running with Docker

```bash
docker run -d \
  --name waim \
  -p 8080:8080 \
  -v waim-data:/appdata \
  --read-only \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --tmpfs /tmp \
  ghcr.io/daknoblo/waim:latest
```

> On first start waim generates the encryption key for the stored API keys and
> writes it to `/appdata/master.key`. Keep the volume: without that file the
> API keys in `config.json` can no longer be decrypted and must be re-entered.

## Running with Docker Compose

Use the provided example:
[`deploy/docker-compose.example.yml`](../deploy/docker-compose.example.yml).

```bash
cp deploy/docker-compose.example.yml docker-compose.yml
docker compose up -d
```

## Environment variables

| Variable          | Default        | Description                                           |
| ----------------- | -------------- | ----------------------------------------------------- |
| `WAIM_ADDR`       | `:8080`        | Listen address.                                       |
| `TZ`              | `Etc/UTC`      | Timezone (IANA name) for timestamps and log display.  |

Everything else is configured in the web UI. Log verbosity is set on the
**Settings** page (not via an environment variable).

## Running behind a reverse proxy

waim has no built-in authentication — put it behind a proxy that handles TLS
and access control. Two headers matter:

- Forward the original `Host` header (nginx: `proxy_set_header Host $host;`).
  waim rejects state-changing cross-origin requests (CSRF protection) by
  comparing the browser's `Origin` against `Host`.
- Forward `X-Forwarded-Proto: https` so the language cookie is marked `Secure`
  and `Strict-Transport-Security` is sent.

waim already emits a strict `Content-Security-Policy`, `X-Frame-Options: DENY`
and `X-Content-Type-Options: nosniff`; the proxy does not need to add them.

## Persistence

Everything waim needs lives in the container data directory `/appdata`
(this path is fixed; mount a volume there to keep your data):

- `config.json` — settings, with API keys stored encrypted.
- `master.key` — the generated encryption key for those API keys.
- `waim.db` — SQLite database with scan runs and findings.

Back up this directory to preserve your configuration and history. Treat the
backup as a secret: it contains `master.key` and therefore everything needed to
decrypt the stored API keys.

## First-time setup

1. Open <http://localhost:8080>.
2. Go to **Settings**.
3. Enter your Jellyfin server URL and API key.
4. Enter your TMDB API key.
5. Click **Refresh libraries from Jellyfin** and tick the libraries to scan.
6. Adjust the scan interval and rate limit if needed, then **Save**.
7. Trigger a scan with **Scan now** or wait for the scheduled run.

## Upgrading from 1.3 or older

> **Breaking change:** your stored API keys have to be entered once more.

The `WAIM_MASTER_KEY` environment variable is gone. The encryption key is now
generated automatically and stored as `master.key` in the data directory, so
nothing has to be configured — but API keys encrypted by an older version can no
longer be decrypted.

1. Drop `WAIM_MASTER_KEY` from your compose file, `.env` or `docker run`
   command (a leftover variable is simply ignored).
2. Start the new version. All other settings — Jellyfin URL, selected
   libraries, scan and cache options, scan history — are preserved.
3. The UI shows a warning banner: re-enter your Jellyfin, TMDB and (if used) AI
   API keys on the **Settings** page and save. The banner disappears once the
   keys are stored with the new key.
