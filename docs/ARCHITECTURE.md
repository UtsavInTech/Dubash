# Architecture

## Today (after phase 1)

One Go process. Everything below runs in it.

```
speaker console (browser mic) ─┐   PCM16 16 kHz over WebSocket (/ingest/{room}, ADMIN_TOKEN)
dubash-ingest (mixer feed)    ─┘
        │
        ▼
  room.Run (one per live event; Pause drops audio here, End stops it)
    meter ──▶ asr.Engine (Sarvam realtime | Gemini Live | Whisper-compatible | Fake)
                 │ interim (provisional) / final (committed) events
                 │ assembler: sentence commit, alignment against committed words
                 ▼
    correction (≤ 800 ms): bank mis-hearings, near misses, then the chat
      model with bank entries + aligned script passage; every edit checked
    commit: segment id, stream epoch, raw + fixes ──▶ hub "final" (SSE id)
         └─▶ per-language worker (only languages being read, plus always_translate)
                batch the lines waiting ──▶ translate.Translator / Batcher
                (Sarvam: keep-as-is terms masked, shared rate limiter)
                ──▶ hub "translation" (readers of that language, SSE id)
         └─▶ store writer (async, in order, once a line's translations are in)
        │
        ▼
  room.Hub: resume buffer of numbered messages, subscribers per language
        │  SSE /events/{room}?lang=xx: status, replay (or resume after
        │  Last-Event-ID), caught-up, interim, final, translation
        ▼
  phones (/r/{room}), stage screen, OBS overlay
```

Control plane: the Dictation Bank (`internal/bank`, PostgreSQL or a
file): versioned entries with scopes and approval, scripts, corrections,
audit; publishing compiles an immutable version (resolved entries +
script chunks) that the room holds in memory with a BM25 index and an
aligner. The `/bank` page manages it, takes scripts and runs
rehearsals; the speaker console fixes lines live (a correction revision,
re-translated, suggested to the bank).

Configuration: `rooms.yaml` (source, targets, glossary, speaker,
asr_mode, always_translate), env vars for providers and keys, one
`ADMIN_TOKEN`. Languages come from `internal/lang`.

## Target (from the brief)

```
             ┌──────────── control plane (Postgres) ────────────┐
             │ orgs, users, roles · events, languages, QR codes │
             │ Dictation Bank · scripts · context versions      │
             │ document workers (separate pool)                 │
             └─────────────┬────────────────────────────────────┘
                           │ immutable event context package (pinned version)
publisher ──▶ ingestion ──▶ recognition ──▶ context + bounded correction ──▶ translation
 (admin web,   (auth, epoch,  (Sarvam primary,   (bank retrieval, script      (per active
  mixer feed)   order, buffer) failover)          alignment, deadline)          language only)
                                                                │
                                Redis: lease per live event, pub/sub, replay buffers
                                                                ▼
                                             gateways (LB, per event+language subscriptions,
                                             resume from sequence) ──▶ phones
```

Persistence of committed text, translations and usage is asynchronous; a
control-plane outage must not stop a live event.

## How we get there

Each phase extends the existing packages rather than replacing them:

| Phase | Main changes |
|---|---|
| 1 | Language registry (Indian languages) replacing 4 hard-coded lists; danda-aware sentence commit; Sarvam streaming `asr.Engine` and `translate.Translator`; per-language messages with ordering fields; lazy translation; Indic fonts; English UI; pause; resume from sequence. |
| 2 | Postgres; Dictation Bank with scopes and versions; term protection around translation; live fixes → suggested entries. |
| 3 | Script upload and extraction; context retrieval; bounded correction with timeout; rehearsal. |
| 4 | Events in the database; Redis leases and pub/sub; gateways behind a load balancer; provider failover with breakers; load and failure tests. |
| 5 | Security review, retention, exports, runbook. |

The benchmark (`cmd/dubash-bench`) sits beside this: it drives the same
`asr.Engine` and `translate.Translator` implementations on recorded audio.
