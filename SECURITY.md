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

- Always set `ADMIN_TOKEN` on a server reachable from the Internet: it
  guards audio ingest, the operator console's stream, the production panel
  and the metrics. Docker Compose refuses to start without it.
- Serve Dubash over https (a reverse proxy or a tunnel). The token
  travels in the operator's WebSocket URL, and browsers only allow
  microphone capture on secure origins anyway.
- `GEMINI_API_KEY` and the model servers' keys stay on the server; the
  pages never see them. Error messages shown to the operator are stripped of
  URLs, which may carry credentials.
- Audience pages and transcript exports are public by design.
