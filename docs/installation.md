# Installation

> This project is "vibe-coded" (AI-assisted). Review before relying on it.

waim is distributed as a multi-arch container image (`linux/amd64`,
`linux/arm64`) on the GitHub Container Registry.

## Requirements

- Optional: one or more [Jellyfin](https://jellyfin.org/) instances and API keys.
  TMDB alone is sufficient for the watch collection.
- A [TMDB](https://www.themoviedb.org/settings/api) API key (a v3 key or a v4
  read access token both work — the format is auto-detected).
- Docker / Docker Compose (or any OCI runtime).

## Pulling the image

```bash
docker pull ghcr.io/daknoblo/waim:latest   # approved stable build of main
docker pull ghcr.io/daknoblo/waim:1.4.0   # a specific version tag
```

### Stable and development channels

`main` publishes `:latest`; `develop` publishes `:dev`. A development push
never updates the stable image or the public demo. Changes reach stable only
after the maintainer merges the promotion pull request and CI passes.
New version tags (`X.Y.Z`, including patches) are stable releases from `main`.

For development testing, change the image tag to `dev` in a separate Compose
setup and use a different container name, host port and host data directory.
Never let stable and dev share the same persistent data: database/configuration migrations
may not be reversible. If testing with existing data, use a separate backup
copy, including its `master.key`; keep that backup private.

## Running with Docker

Create `./appdata` on the host first. The container runs as UID/GID **65532**
and needs write access to this directory and its contents. For a new directory
on Linux, create it with `mkdir -p ./appdata` and assign its ownership with
`sudo chown 65532:65532 ./appdata`. Existing data must remain accessible to
that UID; do not loosen permissions to world-writable.

```bash
docker run -d \
  --name waim \
  -p 8080:8080 \
  --mount type=bind,src="$(pwd)/appdata",dst=/data \
  --read-only \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --tmpfs /tmp \
  ghcr.io/daknoblo/waim:latest
```

> On first start waim generates the encryption key for the stored API keys and
> writes it to `/data/master.key` (`./appdata/master.key` on the host).
> Keep the directory: without that file the
> API keys in `config.json` can no longer be decrypted and must be re-entered.

## Running with Docker Compose

Use the provided example:
[`deploy/docker-compose.example.yml`](../deploy/docker-compose.example.yml).

```bash
cp deploy/docker-compose.example.yml docker-compose.yml
docker compose up -d
```

Prepare `./appdata` as described above before starting Compose. The bind mount
deliberately refuses to create a missing host directory, avoiding a silently
created root-owned empty directory. The path is relative to the Compose file.

## Environment variables

| Variable          | Default        | Description                                           |
| ----------------- | -------------- | ----------------------------------------------------- |
| `WAIM_ADDR`       | `:8080`        | Listen address.                                       |
| `WAIM_DATA_DIR`   | `/data` (image), `./appdata` (local) | Persistent data directory; mount storage at the same container path if overridden. |
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

Everything waim needs lives in the container data directory `/data`
(mounted from `./appdata` on the host in the Compose example):

- `config.json` — settings, with API keys stored encrypted.
- `master.key` — the generated encryption key for those API keys.
- `waim.db` — SQLite database with scan runs and findings.

Back up this directory to preserve your configuration and history. Treat the
backup as a secret: it contains `master.key` and therefore everything needed to
decrypt the stored API keys.

## Upgrading the container data path

This change is available on `develop` / `:dev` first. Until it is promoted,
existing `:latest` and older pinned images still use `/appdata`. Change the
image and mount target together; do not apply a `/data` mount to an old image.

For an existing **bind mount** `./appdata:/appdata`, stop the old container,
back up the whole host directory, then change only the container target to
`./appdata:/data` and start the new image. The host directory, database,
configuration and `master.key` stay where they are; no data move is needed.

If using a **named volume**, retain that exact volume and change its target
from `/appdata` to `/data`, for example `waim-data:/data`. Do not replace a
named volume with the new bind-mount example without explicitly copying your
data first, and never use `docker compose down -v` during this change.

An explicit `WAIM_DATA_DIR` override must agree with your mount target.
There is no automatic copy or fallback to the old directory. A missing or
wrong mount may otherwise look like a fresh installation; stop and correct
the mount instead of overwriting your existing data. Test dev against a
separate copy, not the live stable directory.

## First-time setup

1. Open <http://localhost:8080>.
2. Go to **Settings**. Everything you enter is saved automatically, and each
   connection is tested as soon as its details are complete.
3. Optionally add named Jellyfin instances on **Media sources**.
4. Enter your TMDB API key.
5. For each saved source, click **Refresh libraries from Jellyfin**, select
   libraries and **Save source**. Or open **Watch collection** and add TMDB titles.
6. Adjust the scan interval and rate limit if needed.
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
   API keys on the **Settings** page. They are saved as you enter them, and the
   banner disappears once the keys are stored with the new key.
