# Copilot-Instruktionen — waim

Diese Datei beschreibt die verbindlichen Konventionen, Best Practices und
Sicherheitsvorgaben für das Projekt **waim**. GitHub Copilot soll sich bei allen
Vorschlägen (Code, Dockerfile, GitHub Actions, Tests, Doku) an diese Vorgaben
halten. Sie sind konsistent mit den Standardisierungsregeln für die
Go/Docker-Repositories von `daknoblo`.

> **Kontext:** waim (What Am I Missing?) ist ein kleines Dashboard zum Abgleich
> einer Jellyfin-Mediathek mit TMDB. Es zeigt fehlende Serienfolgen, Staffeln,
> Filme und Collection-Einträge und läuft als einzelnes Binary in Docker.

---

## 1. Sprache, Runtime & Grundprinzipien

- **Sprache: Go** (aktuelle stabile Minor-Version, aktuell **Go 1.26**).
- **Module-Pfad:** `github.com/daknoblo/waim`.
- **Ein einzelnes, statisches Binary** als Auslieferungsartefakt — keine
  externen Runtime-Abhängigkeiten.
- **Pure-Go / CGO-frei:** Immer `CGO_ENABLED=0` bauen. Für SQLite die
  reine-Go-Implementierung **`modernc.org/sqlite`** verwenden (kein
  `mattn/go-sqlite3`, kein C-Toolchain).
- **Server-gerendertes Web-UI:** **templ + HTMX + Tailwind CSS**. Generierte
  Dateien (`*_templ.go`, kompiliertes `internal/web/assets/static/app.css`)
  werden **committet**, damit das Projekt ohne templ-/Tailwind-Toolchain baubar
  ist.
- **Standardbibliothek zuerst.** Abhängigkeiten nur einführen, wenn sie klaren
  Mehrwert bieten. Bevorzugt sind `log/slog`, `net/http`, `modernc.org/sqlite`
  und bereits vorhandene Projektabhängigkeiten.

## 2. Projektstruktur

```
waim/
├── cmd/waim/main.go              # Einstiegspunkt (Flags, Wiring, Start)
├── internal/
│   ├── ai/                       # OpenAI/Azure-kompatible Vorschläge
│   ├── config/                   # Settings, Validierung, verschlüsselte API-Keys
│   ├── crypto/                   # AES-256-GCM für Secrets at-rest
│   ├── i18n/                     # Lokalisierung (EN/DE)
│   ├── jellyfin/                 # Jellyfin-Client
│   ├── logbuf/                   # In-Memory-Aktivitätslog
│   ├── scanner/                  # Abgleichslogik Jellyfin ↔ TMDB
│   ├── scheduler/                # Periodische Scans/Refreshes
│   ├── server/                   # HTTP-Server, Routing, Handler
│   ├── store/                    # SQLite-Zugriff
│   ├── suggest/                  # Vorschlagslogik
│   ├── tmdb/                     # TMDB-Client
│   ├── tmdbcache/                # Lokaler TMDB-Cache
│   ├── version/                  # Build-Metadaten
│   └── web/                      # templ-Templates + Assets
├── deploy/docker-compose.example.yml
├── Dockerfile
├── Makefile
├── go.mod / go.sum
├── .golangci.yml
└── .github/workflows/
```

- `main.go` bleibt schlank: Healthcheck-Flag, Konfiguration, Dependency-Wiring,
  Signal-Handling und Graceful Shutdown.
- Nicht öffentlich wiederverwendbarer Code gehört unter `internal/`.
- User-facing Strings müssen über die i18n-Locale-Dateien gepflegt werden; keine
  hart codierten UI-Texte in Templates ergänzen.

## 3. Build-Metadaten (`internal/version`)

`internal/version` hält per `-ldflags` injizierte Werte:

```go
var (
    Version = "dev"
    Channel = "local"
    Commit  = "unknown"
    Date    = "unknown"
)
```

Injektion beim Build:

```
-X github.com/daknoblo/waim/internal/version.Version=$(VERSION)
```

## 4. Makefile

Das Makefile stellt die üblichen Targets bereit:

- `tools` — templ CLI installieren.
- `generate` — Go-Code aus `.templ`-Dateien generieren.
- `css` — Tailwind CSS nach `internal/web/assets/static/app.css` kompilieren.
- `build` — statisches Binary bauen (`CGO_ENABLED=0 go build -trimpath ...`).
- `run` — lokal starten (benötigt `WAIM_MASTER_KEY`).
- `test` — `go test ./...`.
- `vet` — `go vet ./...`.
- `tidy` — `go mod tidy`.
- `docker` — Image bauen (Build-Args für Version/Channel/Commit/Date).
- `clean` — Build-Artefakte entfernen.

Versionsformat: `VERSION ?= $(shell date -u +v%Y%m%d-%H%M)`.

## 5. Docker

**Docker-Vorgaben (verbindlich):**

- Mehrstufiges Dockerfile mit `ARG GO_VERSION=1.26` und Build-Stage auf
  `golang:${GO_VERSION}-alpine`.
- `go.mod`/`go.sum` vor dem restlichen Quellcode kopieren und `go mod download`
  separat ausführen.
- Build immer **CGO-frei** (`CGO_ENABLED=0`) und mit Version-Ldflags.
- Runtime-Basis: **`gcr.io/distroless/static:nonroot`**.
- Non-root: `USER nonroot:nonroot` (UID/GID `65532`).
- Persistenz: Datenverzeichnis `/app/appdata`, als Volume gemountet und im Image
  mit korrektem Ownership vorangelegt.
- Healthcheck im Binary: `-healthcheck` fragt lokal `/healthz` ab.
- Multi-Arch-Images für `linux/amd64` und `linux/arm64`.
- Zeitzone: `_ "time/tzdata"` im Go-Code verwenden, damit `TZ` auf distroless
  funktioniert.

## 6. GitHub Actions

### `ci.yml` — Lint, Test & Build

- Läuft bei Push/PR auf `main` und `develop`.
- `permissions: contents: read`.
- Go 1.26 mit Modul-Cache einrichten.
- Generierten templ-Code und kompiliertes Tailwind-CSS prüfen (`git diff --exit-code`).
- `go vet ./...`, `golangci-lint`, `govulncheck`, `go test -race ./...` und
  `CGO_ENABLED=0 go build ./...` ausführen.

### `codeql.yml` — CodeQL

- Läuft bei Push/PR auf `main` und `develop` sowie wöchentlich.
- Für waim ist `build-mode: manual` geeignet, weil generierte templ-/CSS-Dateien
  committet sind und `go build ./...` vor der Analyse ausreicht.

### `release.yml` — Build, Push, Sign & Scan

- Läuft bei Push auf `main`/`develop` und Tags `v*`.
- Veröffentlicht nach `ghcr.io/daknoblo/waim`: `main` → `stable`, `develop` →
  `dev`, Git-Tags → Semver-Tags.
- Nutzt Buildx/QEMU für Multi-Arch, `docker/metadata-action`, Provenance und SBOM.
- Signiert das gebaute Image keyless mit **cosign** über OIDC (`id-token: write`).
- Scannt das Digest-Image mit Trivy und lädt SARIF in den Security-Tab hoch.

**Allgemein:** Action-Versionen immer pinnen; keine ungepinnten `@master`/`@main`.

## 7. Linting & Formatierung

- `.golangci.yml` nutzt golangci-lint v2.
- Code ist immer `gofmt`-formatiert.
- `go vet ./...` muss fehlerfrei sein.
- Fehler grundsätzlich behandeln; bewusst ignorierte `Close()`-Aufrufe über die
  zentrale errcheck-Konfiguration erlauben.

## 8. Sicherheit

- Minimale Angriffsfläche: distroless-Basis, non-root, statisches Binary.
- Read-only Root-Filesystem anstreben; nur `/app/appdata` ist beschreibbar.
- **Secrets/API-Keys niemals im Klartext speichern.** Jellyfin-, TMDB- und
  optionale AI-API-Keys werden at-rest mit **AES-256-GCM** verschlüsselt. Der
  Schlüssel wird aus **`WAIM_MASTER_KEY`** abgeleitet.
- Ohne `WAIM_MASTER_KEY` dürfen API-Keys nicht gespeichert oder entschlüsselt
  werden.
- Keine Secrets committen (keine Keys/Tokens/`.env`).
- Keine eingebaute Authentifizierung: waim ist für vertrauenswürdige Netze bzw.
  Reverse-Proxy/VPN gedacht und darf nicht direkt ins Internet exponiert werden.
- SQL ausschließlich parametrisiert verwenden; keine String-Konkatenation mit
  Nutzereingaben.
- Eingaben validieren, insbesondere URLs, API-Keys, IDs, Scan-Intervalle, Limits
  und Sprach-/Anzeigeeinstellungen.

## 9. Konfiguration & Env-Variablen

- Konfiguration primär über die Web-UI und persistiert in `config.json` im
  Datenverzeichnis.
- Env-Variablen verwenden das Präfix **`WAIM_`**.
- Etablierte Variablen:
  - `WAIM_MASTER_KEY` — erforderlich für AES-256-GCM-Verschlüsselung von API-Keys.
  - `WAIM_ADDR` (Default `:8080`) — Listen-Adresse.
  - `TZ` — Zeitzone (IANA-Name).
- Datenverzeichnis im Container: `/app/appdata`.

## 10. Tests

- Tests mit dem Standard-`testing`-Package, ausführbar über `go test ./...`.
- In CI mit `-race` laufen lassen.
- Für Crypto-, Scanner-, Config- und Store-Logik gezielte Unit-Tests ergänzen.

## 11. Git & Repo-Hygiene

- Branch-Konvention: `main` (stable) und `develop` (dev); Releases über
  `vX.Y.Z`-Tags.
- `.gitignore` deckt Build-Output, lokale Daten/DB-Dateien, `.env*`, Scan-Output
  und OS-/Editor-Rauschen ab.
- `.dockerignore` schließt `.git`, `.github`, lokale Daten/DB-Dateien,
  Build-Artefakte, Doku und Editor-Dateien aus.
- Generierte, aber für den Build nötige Dateien (`*_templ.go`, `app.css`) werden
  committet.

## 12. Definition of Done

1. `gofmt -l .` ist leer.
2. `go vet ./...` ist fehlerfrei.
3. `CGO_ENABLED=0 go build ./...` ist erfolgreich.
4. `go test ./...` ist grün.
5. Docker- und GitHub-Actions-Konfiguration bleiben konsistent zu den Vorgaben.
6. Keine Secrets im Code/Repo.
