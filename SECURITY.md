# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them
privately through GitHub:
[Security → Report a vulnerability](https://github.com/UtsavInTech/dubash/security/advisories/new).

Include what an attacker can do, the steps to reproduce it and the version
or commit you tested. We will acknowledge the report within a few days and
keep you informed until a fix is released.

## Supported versions

Security fixes land on the `main` branch.

## Deployment notes

See [docs/SECURITY_REVIEW.md](docs/SECURITY_REVIEW.md) for the review
behind these, and [docs/RUNBOOK.md](docs/RUNBOOK.md) for running an event.

- Set a strong `ADMIN_TOKEN` (`openssl rand -base64 24`). The server
  listens on 127.0.0.1 by default and refuses to listen on other addresses
  with an empty or weak token (`-insecure` overrides, never for an event).
- Give speakers a **speaker link** from the panel instead of the admin
  token: one event, a few hours, revocable at once.
- Serve Dubash over https (a reverse proxy or a tunnel); set
  `TRUST_PROXY=on` only behind a proxy that sets `X-Forwarded-For`, and
  let it pass the `Sec-WebSocket-Protocol` header.
- Provider keys stay on the server; the pages never see them. Error
  messages shown to the operator are stripped of URLs.
- Audience pages are public unless the event sets `audience_pin`;
  `exports` decides who may download transcripts; `retention_days` and
  `store_transcript` decide what is kept.
