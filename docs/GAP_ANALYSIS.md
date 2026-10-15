# Gap analysis: lenguaraz today vs. the India target

Phase 0. Upstream commit `b6ac70c`, read 2026-10-16. Every claim below was
checked against the code; file references are `path:line`.

Legend: **Present**: works today and fits the target. **Partial**: some
of it exists, or it exists in a form that has to change. **Missing**: not
in the code.

## 1. Brief section 2: claims about the repo, checked

| Claim | Verdict | Evidence / correction |
|---|---|---|
| Go 1.26+, mostly standard library | True | `go.mod` says `go 1.26.0`; deps are 2 WebSocket libs, genai, yaml, qr. Dockerfile builds with 1.27. |
| Frontend: embedded HTML/CSS/JS modules, no build step | True | `internal/web/server.go` `//go:embed static`. |
| Audio via AudioWorklet in the operator browser | True | `internal/web/static/js/pcm-worklet.js`, `operator.js`. |
| One pipeline goroutine per room | True, more precisely | `Room.Run` (`internal/room/room.go:184`) starts a meter goroutine, an ASR goroutine and a finals goroutine, and one goroutine per target language per segment (`room.go:346`). |
| `asr.Engine` and `translate.Translator` interfaces | True | `internal/asr/asr.go`, `internal/translate/translate.go`. |
| Gemini Live or Whisper (`ASR_URL`); Gemini or OpenAI-compatible (`TRANSLATE_URL`) | True | `cmd/lenguaraz/main.go` `backends()`. Translation via Gemini is wrapped in a hedger (`translate/hedge.go`, second request after 1.5 s). |
| SSE delivery, last 50 lines replayed on reconnect | True, with a caveat | `ringSize = 50` (`room.go:23`). Replay is "the last 50", not "everything after sequence N": a client that was away for more than 50 lines silently loses the gap. The client dedupes by segment id (`viewer.js:326`). |
| Per-room JSONL storage | True | `internal/store/store.go`. Written synchronously on the caption path (`room.go:371`) **before** the final is published, against the brief's "the live path must not wait for a database write". |
| QR pages at `/qr/{room}` | True | `server.go:195`. The QR encodes `/r/{room}`, a permanent public URL, not an opaque code. |
| Projector view, OBS/vMix overlay, posters | True | `viewer.js` `mode=screen` / `mode=overlay`; `posters.html`. |
| VTT/SRT/TXT export | True | `internal/export`. **Public, no auth** (`server.go:90`): anyone with the room id can download the whole transcript. |
| Rooms in `rooms.yaml` | True | Loaded once at start-up (`config.Load`); no reload, no API to create rooms. |
| `ADMIN_TOKEN`, `PUBLIC_URL` | True | One shared token for everything (`server.go:123`); accepted as a query parameter, so it appears in URLs and logs. The operator and admin HTML pages are served without auth; only their APIs and `/ingest` check the token. |
| Prometheus metrics | True | `internal/web/metrics.go`: per-room gauges and counters only. No histograms, so no p50/p95; "latency" is a mean of final→published. |
| Playwright E2E tests | True | `e2e/`: 49 checks, all pass (see TESTING.md). |
| Docker Compose | True | Refuses to start without `ADMIN_TOKEN`. |
| `-fake` mode | True | `asr.Fake`, `translate.Fake`. Works (checked). |
| Run with `go run ./cmd/lenguaraz` | True | |
| Operator console needs Chrome/Edge and a secure context | Mostly | The code checks features (`AudioWorkletNode`, `isSecureContext`), not the browser (`operator.js:204`); current Firefox and Safari have both, so the "Chrome/Edge only" claim is conservative. Untested on them. |
| Only six European languages | True | Hard-coded in 4 places: `config/config.go:38`, `translate/translate.go:18`, `asr/gemini.go:54`, `static/js/langs.js:2`, plus the viewer's UI string table (`viewer.js` `UI`). |
| No shared state between instances; manual sharding | True | README "Scaling to more rooms". |

Not in the brief but in the repo, and worth keeping:

- `cmd/lenguaraz-ingest`: headless ingest from ffmpeg (SRT/RTMP/HLS, capture card), reconnects with backoff and keeps only the last 5 s while disconnected (`internal/ingest/ingest.go:29`). This is the "clean feed from the mixer" path, without a browser.
- A sentence assembler for streaming ASR (`internal/asr/assemble.go`): commits stable sentences from interims, cuts run-ons at clause boundaries. Directly relevant to "provisional vs committed".
- Live-session rotation, stall detection and a bounded audio backlog in the Gemini engine (`gemini.go`, `maxBacklog = 10 s` at line 52).
- Talks within a room, and per-talk exports.
- Accessibility work (WCAG AAA contrast, reading settings, listen mode with device voices).

Upstream specifics that need changing for India:

- **The whole operator, admin and poster UI is in Spanish** (Argentine voseo), and every `<html lang="es">`. The audience page defaults to Spanish UI strings.
- **Sentence segmentation ignores the danda.** `isTerminal` accepts `.?!…` only (`assemble.go:167`), so Hindi text ending in `।` is never committed by punctuation; it waits for a pause, the run-on cut at 100–200 bytes (which in Devanagari is only ~35–70 characters, since each is 3 bytes in UTF-8), or the utterance end. The abbreviation list is Spanish/English.
- **The bundled font has no Indian scripts.** Inclusive Sans (`static/fonts/`) covers Latin only; Indic text falls back to whatever the phone has.

## 2. Brief section 3: target architecture, item by item

### Principles

| Item | Status | Notes |
|---|---|---|
| Control plane separate from media plane | Missing | One process, one binary; config is a file read at start. |
| Provisional vs committed text | Partial | ASR interims and finals exist (`asr.Interim`/`asr.Final`) and the viewer shows the live line in the original language only. Interims carry no id, revision or sequence; translations exist for committed lines only (correct per brief). Provisional text is not styled differently in translated views because they never receive it. |
| Explicit ordering on every message (event_id, stream_epoch, segment_id, sequence, revision, language, status, start/end ms) | Partial | `caption.Segment` has `id` (room-wide sequence), `room`, `talk`, `t0`/`t1` (seconds), `lang`. Missing: stream epoch, revision, status, per-language messages. The id is assigned after translation, in finish order (serialized by one goroutine, so order is kept). |
| Provider adapters behind interfaces | Present | `asr.Engine`, `translate.Translator`. Both need richer signatures later (phrase hints with weights, context lines, glossary entries with approved translations), but the shape is right. |

### Live pipeline

| Stage | Status | Notes |
|---|---|---|
| 1. Capture, transmitting indicator, sound check | Partial | Operator console: source picker, level meter, live preview, sample files. No "translation check" step, no explicit pre-live test. |
| 2. Ingestion: auth, epochs, ordering, silence/disconnect detection, recovery buffer | Partial | Token auth; one live operator per room (`ErrBusy`); WebSocket frames are ordered by TCP; level and "no audio arriving" detection in status; operator-side 5 s backlog (`ingest.go`) and engine-side 10 s backlog (Gemini). Server drops chunks when the pipeline lags (`server.go:376`, buffer 50). No stream epoch, no publisher identity beyond the shared token. |
| 3. Recognition: streaming, provisional/final, phrase hints, Sarvam primary, second provider, failover | Partial | Gemini Live streams with interims and custom vocabulary; Whisper-compatible servers by utterance. **No Sarvam adapter, no Indian language codes, no failover**: one engine per process, chosen at start. |
| 4. Context service (bank entries, recent speech, script section) | Missing | Only a flat per-room `glossary` list (`config.Room.Glossary`, max 1000). |
| 5. Bounded correction stage | Missing | |
| 6. Translation per committed phrase, context, glossary, faithful | Partial | Per committed sentence; glossary as "never translate" in the prompt; echo detection and retry; hedging. Missing: preceding-line context, approved translations per language, and **lazy translation: every target is translated for every line whether or not anyone reads it** (`room.go:346`). The source-language line also waits for the slowest translation before anyone sees it as final, because one `Segment` carries all languages. |
| 7. Distribution: scalable gateways, subscription per event and language, resume, "return to live", bounded buffers | Partial | In-process hub, bounded per-subscriber buffer (64), slow viewers dropped and reconnect. One subscription per room carrying every language. No resume-from-sequence, no multi-instance fan-out. The viewer has a "follow" button to jump back to the newest line (`viewer.js` `wireFollow`), which is the "return to live" action within one session. |
| 8. Translated audio interface | Partial | Listen mode uses the phone's own speech synthesis (`viewer.js`), with no server interface. `SynthesisProvider`/`AudioSegment` stubs are missing (deferred to v2 by the brief; only stubs needed). |

### Control plane

| Item | Status | Notes |
|---|---|---|
| Organisations, users, roles | Missing | One shared admin token. |
| Events in a database (keeping room behaviour) | Missing | `rooms.yaml`. |
| Event languages | Partial | `source` + `targets` per room, from the 6 European languages. |
| `/j/<opaque-code>` QR, short-lived audience token, optional PIN, revocable | Missing | QR holds `/r/{room}`; the audience needs no token; nothing is revocable. |
| Event states Draft/Ready/Live/Paused/Ended | Partial | A room is live or not, plus talks. No pause (stopping the operator ends the transmission), no ended state. |
| Audience page: welcome, picker, text size, reconnect, waiting and ended pages | Partial | Picker, 4 text sizes, themes, waiting message, reconnect via EventSource. Missing: event/speaker welcome, ended page, Indian languages and fonts, Indian-language UI strings. |
| Admin console: setup, bank, rehearsal, monitoring, live fixes, pause, end, export with retention | Partial | `/admin`: per-room live/level/audience/latency/errors, new talk, exports. Missing: setup UI, bank, rehearsal, listeners **per language** (only per room), live word fixes, pause, end, retention. |
| Dictation Bank | Missing | Glossary list only. |
| Document processing workers | Missing | |
| Rehearsal with and without bank | Missing | The benchmark (`cmd/lenguaraz-bench`, added in phase 0) is the offline precursor. |

### Data model

All of Organization … AuditLog: **Missing**. The only persisted record is
`caption.Segment` (committed text + translations) in JSONL. `UsageRecord`
can be derived later from `Status.AudioSeconds` and segment text, but
nothing records it today.

### Scaling, load balancing, resilience

| Item | Status | Notes |
|---|---|---|
| Separate scaling per workload | Missing | One process does everything. |
| Load balancer, sticky or stateless resume | Missing | |
| Redis presence, leases, replay buffers, pub/sub, rate limits | Missing | |
| Checkpoint committed sequence numbers | Partial | On start a room resumes `nextID` and talk from its JSONL (`room.New`). Single instance only. |
| Separate preparation/live worker pools | Missing | |
| Multi-provider with health checks, quotas, circuit breakers, failover | Partial | Retries and reconnects inside each engine; hedged translation; recoverable errors surface in the panel. No second provider at runtime, no breakers, no quotas. |
| Failure behaviour: speaker disconnect message, audience resume, one failing language isolated, context failure fallback, venue network message | Partial | A failing translation leaves that language empty for the line (falls back to the original via `Segment.In`) and others continue: isolation is there. Speaker disconnect shows "not live", not "connection lost". Audience reconnects automatically. |
| Security: short-lived credentials, server-side keys, rate limits, encryption, tenant isolation, recording off by default, retention, revocable links, idempotent webhooks | Partial | Provider keys are server-side only (good). Everything else is missing: the transcript is always stored and publicly exportable; there are no rate limits on joins or SSE connections. |
| Observability: p50/p95 provisional and translated latency, time to first caption, join/reconnect success, correction delay, cost per event hour per language | Partial | Metrics endpoint with per-room gauges; mean latency only. |

## 3. Quality rules (brief section 4)

| Item | Status |
|---|---|
| WER, bank-term recall, wrongly inserted terms, numbers, negations, terminology consistency, added latency, with/without bank | **Added in phase 0** as `cmd/lenguaraz-bench` (see TESTING.md). Correction-stage latency is not measurable until that stage exists. |
| Adversarial tests (changed number, ambiguous word, accent, code-switching) | Harness supports tagged recordings; the real recordings are needed. Synthetic code-switch and negation clips exist for smoke tests only. |
| Provider claims verified | See PROVIDER_NOTES.md. |

## 4. Deferred items (brief section 5)

| Item | Status |
|---|---|
| `SynthesisProvider` / `AudioSegment` stubs | Missing (phase 1–2, stub only). |
| Minimal `UsageRecord` | Missing. |
| Delivery behind an interface for a later WebRTC path | Partial: the hub (`room.Hub`) is the seam; SSE is written directly in `server.go:247`. |

## 5. What this means for phase 1

The vertical slice is closer than the list of "missing" suggests: capture,
streaming recognition with interims, sentence commit, per-sentence
translation, fan-out, reconnect, storage and export all work. The phase 1
work that touches the existing code most:

1. A language registry in one place, replacing the four hard-coded lists, with Indian languages and `xx-IN` provider codes.
2. Danda-aware and byte-length-safe sentence commit in `assemble.go`.
3. Splitting the per-line `Segment` into a source message and per-language messages, so the original is published immediately and each language only when someone reads it (with the ordering fields).
4. Sarvam streaming engine and translator, with a glossary-protection step around Sarvam translate (it has no glossary parameter).
5. Indic fonts and English (and Hindi) UI strings for operator, admin and audience pages.
6. Pause, distinct from stop.
