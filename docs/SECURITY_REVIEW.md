# Security review (phase 5)

Reviewed on 2026-10-16, branch `phase5`, before the first pilot. Scope: the
Go server (`internal/web`, `internal/store`, `internal/bank`,
`cmd/dubash`), the pages in `internal/web/static`, and `dubash-ingest`.
Phase 4's cluster code (Redis, PostgreSQL transcripts, nginx) was being
written at the same time and is not covered: see "Open" below.

Threat model: a public event venue. Anyone in the hall (or who gets a
photo of the poster) can reach the audience pages; the speaker console
and admin panel run on laptops and phones on the same Wi-Fi; the server
pays per minute of audio and per character translated.

## Findings and fixes

| # | Finding | Severity | Fix |
|---|---|---|---|
| F1 | One shared admin token for everything: the speaker's console held the token that also edits the bank, ends every event and reads costs. A lost phone exposed all of it. | High | Speaker links: a token per event, signed (HMAC-SHA256), at most 72 hours, revocable at once ("Revoke links" cuts a live console off with close code 4003). It can stream, pause, resume and fix lines of its own event only; everything else answers 403. Revocations survive restarts (`data/access.json`). |
| F2 | Tokens in URLs: the admin panel linked to `/operator/{room}?token=…` and `/metrics?token=…`; the console opened its WebSocket with `?token=`; `dubash-ingest` too. URLs end up in proxy logs, browser history and screenshots. | High | Speaker links carry the token in the URL fragment (never sent to a server) and the console removes it from the address bar. The console sends the token as a WebSocket subprotocol (`dubash-token.…`, the server answers `dubash`); `dubash-ingest` sends an Authorization header; the panel downloads metrics and exports with the header. The query parameter is still accepted from old clients. |
| F3 | No limit on wrong tokens: the token could be guessed at network speed. | Medium | 10 failures in 5 minutes from one address blocks that address for 5 minutes (429), for tokens and audience PINs alike. `TRUST_PROXY=on` takes the address from `X-Forwarded-For` behind a proxy. |
| F4 | An empty `ADMIN_TOKEN` opened everything, with only a log line; the example `.env` said `change-me`. | High | The server listens on 127.0.0.1 by default. On any other address, or with a public `PUBLIC_URL`, it refuses to start with an empty or weak token (under 20 characters, or a known example) unless run with `-insecure`. Docker passes `-addr :8080` and Compose already requires the token. |
| F5 | Captions, history and transcripts of every event were public. | Medium (by design upstream) | Per event: `audience_pin` (readers enter it once; a signed 12-hour cookie, HttpOnly, SameSite=Lax), and scanning the hall's QR code admits without it. QR codes point to an opaque `/j/{code}` (HMAC of the room and `join_revision`): bumping `join_revision` kills printed codes. `exports: readers / admin / off`. Open events behave as before. |
| F6 | No security headers. | Low | Every response: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, a Content-Security-Policy allowing only this server's scripts (no inline scripts exist), HSTS over https. Staff pages also `X-Frame-Options: DENY`, `frame-ancestors 'none'`, `Cache-Control: no-store`. Caption pages may still be framed (venue sites, streaming overlays). |
| F7 | No cap on caption connections: one client could open thousands of SSE streams. | Medium | `MAX_VIEWERS` (default 20,000) and `MAX_VIEWERS_PER_IP` (default 300: a venue's Wi-Fi often puts everyone behind one address) answer 503 with Retry-After; EventSource retries by itself. |
| F8 | Rehearsals cost money (billed twice) and could be started in parallel. | Low | One rehearsal per event at a time (429); still admin only, 3 minutes, 40 MB. |
| F9 | Transcripts kept forever, files world-readable (0644, directory 0755); live fixes and their audit lines keep the spoken text too. | Medium | Directory 0700, files 0600 (tightened on start). `retention_days` purges older lines, live fixes and their text in the audit log every hour; "Delete transcript" in the panel removes an ended event's transcript, fixes and replay history. `store_transcript: false` keeps nothing on disk. |
| F10 | Provider keys and error messages. | — | Already fine: keys stay on the server, errors shown to operators are stripped of URLs (`closeReason`). |

Checked and found no issue: every page builds the DOM with
`textContent` and `createElement` (no `innerHTML` sinks with data); the
bank and the correction model treat scripts and lines as data, and every
model edit passes the deterministic rules; uploads are size-limited
(scripts 10 MB, rehearsal 40 MB, fixes 2,000 characters); the bank's SQL
uses parameters only; token comparison is constant-time.

## Residual risks

- A 4-digit PIN is weak against many addresses at once (the lockout is per
  address). Use 6 digits or more; the PIN is a convenience, not a
  password. Rotating it is a `rooms.yaml` change and a restart.
- Anyone who got in can share the captions (a screenshot, a forwarded
  join link until `join_revision` changes). Captions are not DRM.
- A speaker link is a bearer token for its lifetime: send it over a
  private channel, keep the lifetime short, revoke after the event.
- The admin token is still one shared secret for the organisers. Named
  accounts and roles are future work.
- In-memory replay: deleting a transcript cannot reach screens that
  already show the lines.
- The lockout and viewer counts are per server process. With phase 4's
  several servers each enforces its own.

## Open (after merging phase 4)

- Retention and deletion must also cover PostgreSQL transcripts
  (`dubash_segments`): `Store.Delete`, `Store.Purge` and `Store.Disable`
  need their PostgreSQL branches (sketched in RUNBOOK.md, "Merging").
- Speaker-link revocations live in `data/access.json` per server; with
  several servers they need the shared database (or Redis).
- nginx in phase 4 must pass `Sec-WebSocket-Protocol`, set
  `X-Forwarded-For`/`X-Forwarded-Proto`, and the servers run with
  `TRUST_PROXY=on`.
