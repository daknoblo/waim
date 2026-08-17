# Security Policy

> This project is "vibe-coded" (AI-assisted) and maintained as a personal side
> project. Please read the expectations below before reporting.

## Supported versions

Only the most recent release receives fixes. There are no backports to older
minor lines.

| Version | Supported |
| ------- | --------- |
| 1.4.x   | ✅        |
| < 1.4   | ❌        |

Released images are published to `ghcr.io/daknoblo/waim`. The `latest` tag
follows the `main` branch and may contain changes that are not part of a
tagged release yet.

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Use GitHub's private vulnerability reporting instead:
[**Report a vulnerability**](https://github.com/daknoblo/waim/security/advisories/new).
It is enabled for this repository, so the report stays private until a fix is
available, and no email address is involved.

Helpful things to include:

- The affected version (shown on the **About** page) and how it is deployed.
- What an attacker can achieve, and what access they need to get there.
- Steps to reproduce, ideally against a fresh instance.

Please redact API keys, tokens and server URLs from anything you attach.

### What to expect

This is a side project, not a supported product — expect a first reply within
a couple of weeks rather than the same day. You will get an acknowledgement,
an assessment of whether the report is in scope, and a note once a fix ships.
If you would like credit in the advisory, say so in your report.

## Known design limitations

These are documented, deliberate properties rather than vulnerabilities. Please
do not report them; a report that only restates one of these will be closed.

- **No built-in authentication or authorisation.** waim is meant to run on a
  trusted network or behind a reverse proxy that handles TLS and access
  control. Anyone who can reach the port can use the UI and read the
  configured library data. Do not expose it directly to the internet.
- **The encryption key sits next to the data it protects.** `master.key` is
  generated on first start and stored in the same data directory as
  `config.json`. Encrypting the API keys protects a leaked or exported config
  file, not an attacker who can already read the data directory. Treat backups
  of that directory as secret.
- **The activity log may contain server URLs.** It is rendered in the UI for
  anyone who can reach the instance. API keys are redacted.

Findings that go beyond these — for example a way to extract API keys without
data-directory access, to bypass the CSRF origin check, or to make waim issue
requests to an attacker-controlled host — are in scope and welcome.

## What the project does on its side

Every change runs through CI (`go vet`, golangci-lint, tests with the race
detector) and CodeQL static analysis. Released images are scanned with Trivy,
with the results published to the Security tab. Dependencies and GitHub Actions
are kept current by Dependabot, and secret scanning with push protection is
enabled on the repository.
