# Runbook: running an event

For the person running a pilot event. Settings are in `.env`
(see `.env.example`) and `rooms.yaml`.

## A week before

1. **Server.** One machine with https (a reverse proxy or a tunnel; the
   microphone needs a secure page). Set in `.env`:
   - `ADMIN_TOKEN`: `openssl rand -base64 24`. The server refuses weak ones
     on a public address.
   - `TOKEN_SECRET` (optional, same way): signs speaker links and join
     codes, so changing `ADMIN_TOKEN` does not break them.
   - `PUBLIC_URL=https://captions.example.org` (what the QR codes open).
   - `TRUST_PROXY=on` only behind a proxy that sets `X-Forwarded-For`.
   - `SARVAM_API_KEY`, and check the plan's request limit
     (`SARVAM_TRANSLATE_RPM`).
   Start: `go run ./cmd/dubash -addr :8080` (or Docker Compose).
2. **Event in `rooms.yaml`.** Source language, target languages, and:
   - `audience_pin: "482193"` if the captions are not public (6+ digits);
   - `exports: readers | admin | off` (who may download the transcript);
   - `retention_days: 30` and/or `store_transcript: false` as agreed with
     the organiser;
   - `join_revision: 1` (bump it to make printed QR codes stop working).
3. **Dictation Bank** (`/bank`). Names of speakers and places, religious or
   technical terms, known mis-hearings. Approve, then **Publish** the
   event's version. Add the script if there is one.
4. **Rehearsal.** Record one or two minutes with the hard words (WAV), run
   it in the bank page and read the corrections. Fix entries and publish
   again.
5. **Posters.** Open `/admin` with the admin token, then `/posters`, and
   print. For an event with a PIN the poster's QR code admits without it:
   put it only inside the hall.

## The day before

- Make a **speaker link** in the panel (**Speaker link**, 12 to 24 hours)
  and send it privately (not in a group chat). Opening it on the console
  laptop saves it there; the link then leaves the address bar.
- Test the console with the venue's microphone or mixer: start, speak,
  check captions on a phone in each language, stop.
- Check the panel's cost column after the test: about ₹30 per hour for
  recognition plus about ₹4 per hour for each language being read.

## During the event

- Panel (`/admin`): **Live** on, audio level moving, translation delay
  under 2 s, errors at 0.
- **Pause** stops recognition (and cost) during breaks or private
  moments; readers see "paused".
- A wrong word on screen: click it in the console and type the fix. It
  reaches readers at once and is suggested to the bank.
- **New session** splits the transcript between talks without stopping.

## Incidents

| Symptom | Action |
|---|---|
| Console says "Could not connect" | Check the token field; check whether someone else is live in the event (one console at a time). |
| Console says the link was revoked or the token was rejected | Make a new speaker link in the panel. Links expire on their own after their hours. |
| Lost phone or laptop with the speaker link | Panel → **Revoke links** (cuts a live console off), then make a new one. |
| Lost or leaked admin token | Change `ADMIN_TOKEN`, restart. Speaker links survive if `TOKEN_SECRET` is set; otherwise make new ones. |
| PIN leaked | Change `audience_pin`, restart. Readers already in keep their 12-hour pass. To also stop the printed QR codes, bump `join_revision` and reprint. |
| Readers see "too many failed attempts" | Someone on the same Wi-Fi entered wrong PINs or tokens; it clears after 5 minutes. |
| Captions late | Panel delay column: one language slow means its provider is slow (the others continue). All slow: check the Sarvam rate limit and the server's network. |
| Phones get 503 | `MAX_VIEWERS_PER_IP` reached (whole hall behind one address): raise it and restart. |
| Translation errors in the panel | The line shows untranslated; it is retried for later lines. Persistent: check the key and credit. |

## After the event

- **End event** in the panel (the console stops, readers see it ended).
- Download the transcripts the organiser needs (SRT/TXT links in the
  panel work for every exports policy except `off`).
- Revoke the speaker links.
- If agreed, **Delete transcript** (ended events only): the stored
  lines, live fixes and the replay history go. `retention_days` does
  this by itself after that many days.
- Note the cost from the panel.

## Merging phase 4

Phase 4 (cluster, PostgreSQL transcripts) and phase 5 (this) were built in
parallel. After merging:

- `store.Store.Append`: check `s.off[seg.Room]` before the PostgreSQL
  branch.
- `store.Store.Delete` with PostgreSQL: `DELETE FROM dubash_segments
  WHERE room = $1`.
- `store.Store.Purge` with PostgreSQL: `DELETE FROM dubash_segments
  WHERE room = $1 AND payload ? 'at' AND (payload->>'at')::timestamptz <
  $2`.
- Keep speaker-link revocations in the shared database when several
  servers run, and run them with `TRUST_PROXY=on` behind nginx, which
  must forward `Sec-WebSocket-Protocol`.
