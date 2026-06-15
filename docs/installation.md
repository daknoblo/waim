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
docker pull ghcr.io/daknoblo/waim:stable   # stable channel (main branch)
docker pull ghcr.io/daknoblo/waim:dev      # development channel (develop branch)
docker pull ghcr.io/daknoblo/waim:v1.0.0   # a specific version tag
```

## Running with Docker

```bash
docker run -d \
  --name waim \
  -p 8080:8080 \
  -e WAIM_MASTER_KEY="$(openssl rand -base64 32)" \
  -v waim-data:/app/appdata \
  --read-only \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --tmpfs /tmp \
  ghcr.io/daknoblo/waim:stable
```

> Store the generated `WAIM_MASTER_KEY` somewhere safe. If you lose it, the
> encrypted API keys in `config.json` can no longer be decrypted and must be
> re-entered.

## Running with Docker Compose

Use the provided example:
[`deploy/docker-compose.example.yml`](../deploy/docker-compose.example.yml).

```bash
cp deploy/docker-compose.example.yml docker-compose.yml
echo "WAIM_MASTER_KEY=$(openssl rand -base64 32)" > .env
docker compose up -d
```

## Environment variables

| Variable          | Default        | Description                                           |
| ----------------- | -------------- | ----------------------------------------------------- |
| `WAIM_MASTER_KEY` | *(unset)*      | **Required** to store/decrypt API keys (AES-256-GCM). |
| `WAIM_DATA_DIR`   | `/app/appdata` | Directory for `config.json` and the SQLite database.  |
| `WAIM_ADDR`       | `:8080`        | Listen address.                                       |
| `WAIM_DEBUG`      | `false`        | Enable debug logging.                                 |

## Persistence

Everything waim needs lives in the data directory (`/app/appdata` by default):

- `config.json` — settings, with API keys stored encrypted.
- `waim.db` — SQLite database with scan runs and findings.

Back up this directory (and remember your `WAIM_MASTER_KEY`) to preserve your
configuration and history.

## First-time setup

1. Open <http://localhost:8080>.
2. Go to **Settings**.
3. Enter your Jellyfin server URL and API key.
4. Enter your TMDB API key.
5. Click **Refresh libraries from Jellyfin** and tick the libraries to scan.
6. Adjust the scan interval and rate limit if needed, then **Save**.
7. Trigger a scan with **Scan now** or wait for the scheduled run.
