# Architecture

## Today (upstream lenguaraz, phase 0)

One Go process. Everything below runs in it.

```
operator browser ─┐   PCM16 16 kHz over WebSocket (/ingest/{room}, ADMIN_TOKEN)
lenguaraz-ingest ─┘
        │
        ▼
  room.Run (one per live room)
    meter ──▶ asr.Engine (Gemini Live | Whisper-compatible | Fake)
                 │ Interim / Final events (asm: sentence commit from interims)
                 ▼
    finals queue ──▶ publishFinal: translate into every target in parallel
                                   (translate.Translator, hedged)
                     ──▶ store.Append (JSONL, synchronous)
                     ──▶ room.Hub.Publish ("final": one Segment, all languages)
        │
        ▼
  room.Hub: ring of last 50 finals + bounded per-viewer channels
        │  SSE /events/{room}: status, replay, caught-up, interim, final
        ▼
  audience browsers (/r/{room}), projector, OBS overlay
```

Configuration: `rooms.yaml` (rooms, source, targets, glossary), env vars
for provider URLs and keys, one `ADMIN_TOKEN`.

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

The benchmark (`cmd/lenguaraz-bench`) sits beside this: it drives the same
`asr.Engine` and `translate.Translator` implementations on recorded audio.
