# Dubash

[![ci](https://github.com/UtsavInTech/dubash/actions/workflows/ci.yml/badge.svg)](https://github.com/UtsavInTech/dubash/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**Live captions and translation for events in India, on the audience's own phones.**
The speaker talks; the audience scans a QR code, picks Hindi, Telugu,
Kannada, Tamil or English, and reads the speaker's words live in that
language. No app to install.

> *Dubash* (from *do-bhashi*, "two-language person"): the interpreters who
> stood between merchants, courts and traders in Madras, Bombay and Calcutta
> from the 17th century on.

> **Status: work in progress.** Dubash is a fork of
> [Lenguaraz](https://github.com/RchrdHndrcks/lenguaraz) (Apache-2.0; see
> [NOTICE](NOTICE)) being adapted for Indian languages. The sections below
> still describe the upstream feature set (six European languages, Gemini or
> local Whisper); the India plan and progress are in [docs/](docs/), starting
> with [docs/GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md).

📖 **[Leer en español](README.es.md)**: guía de despliegue para organizadores.

![A talk in English reaches every phone in the room, translated to Spanish](docs/screenshots/hero.png)

## Quick start (India)

```bash
cp .env.example .env            # put SARVAM_API_KEY and ADMIN_TOKEN in it
set -a; source .env; set +a
go run ./cmd/dubash             # http://localhost:8080, rooms from rooms.yaml
```

- Speaker console: `http://localhost:8080/operator/main-hall?token=…`, choose
  the microphone, **Go live**. **Pause** stops recognition (nothing is
  captioned or billed) without disconnecting.
- Audience: scan the QR at `/posters`, or open `/r/main-hall?lang=te`.
- Admin: `/admin?token=…`: readers and delay per language, pause, end.
- Without a key: `go run ./cmd/dubash -fake` (scripted captions, including
  Hindi, Telugu, Kannada and Tamil).

With `SARVAM_API_KEY` set, Sarvam recognizes (saaras:v4, realtime) and
translates; a language is translated only while someone reads it. See
[docs/](docs/) for the architecture, decisions, provider notes and tests.

## What it does (upstream Lenguaraz features)

- One **operator page per room** captures a microphone / line-in from the
  sound desk, or plays an audio file, and streams it to the server.
- Or skip the browser: **`dubash-ingest`** pulls the stage's own stream
  (SRT, RTMP, HLS, a capture card: anything ffmpeg reads) and reconnects on
  its own, so each room runs unattended for the whole event.
- Each room transcribes with Gemini's live transcription model
  (`gemini-3.5-transcribe-live`) and translates every finished sentence with a
  fast text model (`gemini-3.5-flash-lite`), keeping glossary terms intact.
- **Or run 100% local**: Whisper (whisper.cpp, Speaches…) for transcription
  and Gemma through Ollama for translation, with no Internet connection. See
  [Run it fully local](#run-it-fully-local).
- The **audience** opens `/`, picks a room and a language and reads live
  captions on their phone, or scans the room's **QR code**, which opens the
  captions in their phone's language. The same page has a **projector** mode
  (with the QR in the corner) and a transparent **OBS / vMix overlay** mode.
- **Reading settings** for accessibility: text size, light, dark and
  high-contrast colors, wider letter and line spacing (WCAG 1.4.12), and a
  bilingual mode that shows the original under each translated line. The
  page's own interface follows the caption language.
- **Listen mode**: with headphones, the audience's phone reads each new
  line aloud in the chosen language with the device's own voices, turning
  the captions into simultaneous interpretation for blind and low-vision
  attendees or anyone who prefers to listen. No server cost, nothing leaves
  the phone, and it stays live by reading faster or skipping ahead when the
  talk outruns the voice.
- **Transcripts per talk**: every room's transcript is split into talks, and
  each talk downloads as **VTT, SRT or TXT** with times starting at zero,
  ready to publish next to the talk's recording.
- A **production panel** shows every room's state, input audio level,
  recognizer activity, audience, latency and errors, lets the team start a
  new talk, and exports **Prometheus metrics** for alerting.
- **Printable A4 posters** with each room's QR for doors and seats.

<p>
  <img src="docs/screenshots/phone.png" alt="Audience page on a phone: live captions in Spanish, language switch, Listen and Settings buttons" height="360">
  <img src="docs/screenshots/projector.png" alt="Stage screen: the last three lines in huge type with the room's QR code" height="360">
</p>
<img src="docs/screenshots/admin.png" alt="Production panel: two rooms live, audience, audio level, latency and errors per room" width="720">

## Quick start

Requirements: Go 1.26+ (or Docker) and a Gemini API key from
[Google AI Studio](https://aistudio.google.com/apikey).

```bash
git clone https://github.com/UtsavInTech/dubash && cd dubash
export GEMINI_API_KEY=...        # required unless you run fully local
export ADMIN_TOKEN=change-me     # protects operator and admin pages
go run ./cmd/dubash           # http://localhost:8080
```

Try it without a key (scripted captions): `go run ./cmd/dubash -fake`.

With Docker: `GEMINI_API_KEY=... ADMIN_TOKEN=... docker compose up --build`.
Compose refuses to start without `ADMIN_TOKEN`, so a container is never
exposed with open operator and admin pages.

The operator console captures audio with an AudioWorklet, so it needs
**Chrome or Edge** (Chromium) and a **secure context**: open it over https
(for example through your tunnel) or at `http://localhost`. Browsers block
microphone and worklet access on plain `http://<LAN-IP>`; the page says so
instead of starting. Audience pages work in any browser, over any origin.

### Try it with the bundled samples

1. Open `http://localhost:8080/operator/sala-a?token=change-me`, click the
   **en inglés** sample, then **Transmitir**.
2. Open `http://localhost:8080/operator/sala-b?token=change-me`, click the
   **en español** sample, then **Transmitir**: two sessions in parallel.
3. Open `http://localhost:8080/` and pick a room and a language.

Picking a sample also sets the console's spoken language, so either sample
works in either room.

The clips are synthetic speech from open-licensed Piper voices; see
[samples/README.md](samples/README.md) for how they were generated and the
voice licenses.

To use a real talk: download its audio (for example with
`yt-dlp -x --audio-format m4a <youtube-url>`) and choose it with the file
picker in the operator page.

## Pages and endpoints

| URL | Who | What |
|---|---|---|
| `/` | audience | rooms and languages |
| `/r/{room}?lang=es` | audience | captions on a phone (history, language switch, downloads) |
| `/r/{room}` | audience (QR) | same, in the reader's browser language |
| `/r/{room}?lang=es&mode=screen` | stage screen | last 3 lines, huge type, QR corner (`&qr=0` hides it) |
| `/r/{room}?lang=es&mode=overlay` | OBS / vMix | transparent Browser Source; `lines`, `size`, `hold` tune it: see the [OBS guide](docs/obs.md) |
| `/operator/{room}?token=…` | tech desk | audio source, spoken language, talk title, level meter, live preview |
| `/admin?token=…` | production | totals, per-room status, audio level, audience, latency, errors, new talk, exports |
| `/posters` | production | printable A4 QR poster per room (`?room=sala-a` for one) |
| `/qr/{room}` | anyone | SVG QR code that opens the room |
| `/export/{room}/{vtt,srt,txt}?lang=es` | anyone | the room's whole transcript; add `&talk=N` for one talk |
| `/api/rooms/{room}/talks` | anyone | the room's talks: number, title, start, lines, duration |
| `/metrics` | monitoring | Prometheus metrics (Bearer `ADMIN_TOKEN`) |
| `/healthz` | orchestrator | liveness probe |

## Configuration

Rooms live in `rooms.yaml`:

```yaml
rooms:
  - id: sala-a
    title: "Sala A — Keynotes"
    source: en            # language usually spoken on stage
    targets: [es, pt]     # the room's other languages
    glossary: [Nerdearla, Kubernetes, eBPF]
```

Languages are `en`, `es`, `pt`, `fr`, `de` and `it`. The room serves
`source` plus `targets`. Talks in the same room can switch language: the
operator picks the language spoken for each transmission (the console's
**Idioma que se habla** selector, or `dubash-ingest -lang es`), the
recognizer is told that language, and the room's other languages become the
translations. Without a choice, the room keeps its last language.

`glossary` terms bias the transcription (custom vocabulary, or the Whisper
prompt) and are never translated.

| Env / flag | Default | |
|---|---|---|
| `GEMINI_API_KEY` | — | required unless `-fake` or both `ASR_URL` and `TRANSLATE_URL` are set |
| `ADMIN_TOKEN` | empty (open) | guards `/operator` streaming, `/ingest`, `/admin`, `/metrics` |
| `PUBLIC_URL` | request host | audience URL encoded in QR codes, e.g. `https://subs.example.org` |
| `ASR_URL` | — | OpenAI-compatible transcription server (`…/v1`); replaces Gemini Live |
| `ASR_MODEL` | `gemini-3.5-transcribe-live`, or `whisper-1` with `ASR_URL` | transcription model |
| `ASR_API_KEY` | — | bearer token for `ASR_URL`, if it needs one |
| `TRANSLATE_URL` | — | OpenAI-compatible chat server (`…/v1`); replaces Gemini |
| `TRANSLATE_MODEL` | `gemini-3.5-flash-lite`, or `gemma3` with `TRANSLATE_URL` | translation model |
| `TRANSLATE_API_KEY` | — | bearer token for `TRANSLATE_URL`, if it needs one |
| `-addr` | `:8080` | listen address |
| `-config` | `rooms.yaml` | rooms file |
| `-data` | `data` | transcript directory (JSONL per room) |
| `-fake` | off | scripted engines, no API calls |

## Talks and transcripts

Every caption line belongs to a talk. A room starts a new talk when:

- the operator starts a transmission with a new title (the console's
  **Charla** field, or `dubash-ingest -talk "…"`);
- production clicks **Nueva charla** in the panel, which splits the talk
  without interrupting the audio (for rooms fed around the clock by
  `dubash-ingest`);
- the room was idle for more than 5 minutes, so a reconnecting operator
  continues the talk but the next talk after a break starts afresh.

At the end of each talk its transcript is one click away: the audience page
offers **This talk** and **The whole room**, and
`/export/sala-a/srt?lang=es&talk=3` returns that talk alone with times that
start at zero, to load over its recording.

## Captioning a stage stream (no browser)

`dubash-ingest` sends a stage's audio to its room from any machine that
can see the feed (the streaming PC, the mixer's network, a small VM). It
decodes with ffmpeg, keeps only the last 5 s while the server is unreachable
(captions resume live instead of lagging), and reconnects with backoff when
the network, the server or the source drops.

```bash
go install ./cmd/dubash-ingest   # from the repo; lands in $(go env GOPATH)/bin
export DUBASH_URL=https://subs.example.org ADMIN_TOKEN=change-me

dubash-ingest -room sala-a -i srt://mixer.local:9000          # SRT from the mixer
dubash-ingest -room sala-b -i rtmp://stream.local/live/sala-b  # RTMP feed
dubash-ingest -room sala-c -i "$(yt-dlp -g -f bestaudio <url>)" # a YouTube live
dubash-ingest -room sala-a -lang es -talk "eBPF" -i talk.mp4 -realtime  # a talk in Spanish, from a file
arecord -f S16_LE -r 16000 -c 1 -t raw | dubash-ingest -room sala-a  # raw PCM on stdin
```

Run one per room (a systemd unit or a `tmux` pane each); the production
panel shows every room's state either way.

## Run it fully local

For venues without a reliable uplink, or events that cannot send audio to a
cloud provider, both halves of the pipeline can run on your own hardware.
Dubash speaks the OpenAI-compatible APIs that local model servers expose:

```bash
# Transcription: whisper.cpp's server (or Speaches, LocalAI…)
whisper-server -m models/ggml-large-v3-turbo.bin --host 0.0.0.0 --port 8000 \
  --inference-path /v1/audio/transcriptions

# Translation: Gemma through Ollama (or llama.cpp's server, vLLM…)
ollama pull gemma3

ASR_URL=http://localhost:8000/v1 TRANSLATE_URL=http://localhost:11434/v1 \
  ADMIN_TOKEN=change-me go run ./cmd/dubash
```

With Docker Compose, point them at `http://host.docker.internal:…` instead.
You can also mix: Gemini for transcription and Gemma for translation, or the
other way around.

How the local engine works: the OpenAI transcription API takes files, not
streams, so Dubash cuts the audio at the speaker's pauses (or at the
quietest moment of a long run, at most 10 s), sends each utterance as a WAV,
and transcribes the utterance in progress every 1.5 s while the server is
idle, for a live line in the original language. Captions arrive one
utterance at a time instead of word by word as with Gemini Live, and the
latency depends on your hardware: a GPU, or Apple silicon, is recommended
for `large-v3-turbo` and `gemma3` with several rooms.

## Monitoring

The production panel (`/admin`) refreshes every 2 seconds. Beyond status and
errors it shows each room's **input level** ("Silencio" when the channel is
live but quiet, "No llega" when audio stopped arriving) and **when the
recognizer last produced text**, so a muted mixer channel or a stuck
transcription is spotted before the audience notices.

For a large event, scrape `/metrics` with Prometheus
(`authorization: {credentials: <ADMIN_TOKEN>}`) and alert on, for example:

```promql
dubash_room_live == 1 and dubash_room_audio_level_dbfs < -50   # live but silent
time() - dubash_room_last_text_timestamp_seconds > 30 and dubash_room_live == 1
rate(dubash_room_errors_total[5m]) > 0
```

## How it works

```
operator browser ─┐                            ┌─▶ Gemini Live, or Whisper (local)
dubash-ingest ─┴─PCM 16 kHz / WebSocket──▶ room      │ interim + final text
                                               │        ▼
                                               │  translate each sentence (Gemini, or Gemma)
                                               ▼        │ one call per language
audience browsers ◀──── Server-Sent Events ─── hub ◀── store (JSONL per room)
```

- One goroutine pipeline per room; audio is billed once per room and every
  extra language is only a cheap text call.
- Gemini Live sends the utterance so far every half second. Dubash
  commits a sentence as soon as later words confirm it, or once the speaker
  pauses after it, so translations do not wait for the utterance to end;
  long run-on sentences are cut at a clause boundary.
- Live sessions are rotated before their 10-minute limit, replaced when their
  transcript stalls, and reconnected with backoff; audio is buffered
  meanwhile and never blocks the operator's socket.
- Slow translations are hedged: a request that has not answered after
  1.5 s is sent again and the first answer wins.
- Slow viewers are dropped instead of slowing a room down; their browser
  reconnects and replays the last 50 lines.
- On shutdown the server waits for the lines still being translated, so
  nothing said before a restart is lost from the transcript.

## Scaling to more rooms

A single instance handles many rooms: each room is one Live session plus a
handful of goroutines, and viewers are plain SSE connections. The limits you
will hit first are the Gemini API quotas (concurrent Live sessions, requests
per minute): check them for your key and request increases for big events.

To go beyond one machine:

1. **Shard rooms across instances.** Run N copies with different
   `rooms.yaml` files and route `/r/{room}`, `/events/{room}`,
   `/ingest/{room}`, `/export/{room}` to the instance that owns the room (a
   path-based rule in any reverse proxy). No shared state is needed.
2. **Scale viewers separately.** For very large audiences, put the SSE
   fan-out behind a shared bus (Redis Pub/Sub, NATS or Google Pub/Sub): rooms
   publish segments, stateless edge instances subscribe and serve viewers.
3. **Swap models.** Transcription and translation sit behind two small Go
   interfaces (`asr.Engine`, `translate.Translator`); Gemini, Whisper and any
   OpenAI-compatible chat model already implement them.

## Accessibility and design

Captions exist for accessibility, so the interface follows public-service
design practice rather than decoration: one typeface, conventional
components, captions and body text at 7:1 contrast or more (WCAG AAA), 44 px
touch targets and a single high-visibility focus style (yellow with a dark
bar) on every theme.

- **Typeface:** [Inclusive Sans](https://github.com/LivKing/Inclusive-Sans)
  (SIL OFL), designed for legibility: it tells `I`, `l` and `1` apart and
  `0` from `O`, and stays compact enough to fit a caption line on a phone.
  It ships inside the binary, so pages make no third-party requests: the
  audience is not tracked and the site works on an isolated venue network.
- **Listen** reads new lines aloud with the browser's speech synthesis in
  the caption language; while it is on, the `aria-live` region goes quiet
  so screen readers do not announce each line twice, and a screen wake lock
  keeps the phone from pausing the page.
- **Reader settings**, remembered per device: 4 text sizes, colors (device,
  light, dark, high contrast), wider letter and line spacing, and the
  original under each translation.
- Captions carry the right `lang` attribute so screen readers pronounce them
  in the correct language; new lines are announced through an `aria-live`
  region; the page's own text follows the caption language (ES / EN / PT /
  FR / DE / IT); motion is disabled with `prefers-reduced-motion`.

## Development

```bash
go test -race ./...
(cd e2e && npm install && npx playwright install chromium && npm test)  # browser end-to-end suite
go run ./cmd/dubash -fake &                 # scripted engines
head -c 960000 /dev/zero | go run ./cmd/dubash-ingest -room sala-a -realtime  # 30 s of fake captions
samples/make-samples.sh          # regenerate the Piper TTS samples (Docker + ffmpeg)
ffmpeg -loglevel error -i samples/en.wav -f s16le -ac 1 -ar 16000 - | \
  go run ./cmd/asr-smoke -lang en   # check the Live API from the terminal
```

The code is plain Go with the standard library wherever it can be (the
frontend is HTML, CSS and JavaScript modules with no build step, embedded in
the binary):

| Package | Role |
|---|---|
| `cmd/dubash` | the server |
| `cmd/dubash-ingest` | stream a stage's audio into a room without a browser |
| `cmd/asr-smoke` | stream PCM from stdin to Gemini Live and print the events |
| `promo` | the promo video, as code ([Remotion](https://www.remotion.dev)) |
| `internal/asr` | speech to text: Gemini Live, Whisper-compatible servers, a fake |
| `internal/translate` | text translation: Gemini, OpenAI-compatible chat, a fake |
| `internal/room` | one stage's pipeline, talks, status and viewer fan-out |
| `internal/web` | pages, SSE, ingest WebSocket, exports, admin API, metrics |
| `internal/store`, `internal/export` | JSONL transcripts; VTT, SRT and TXT |

See [CONTRIBUTING.md](CONTRIBUTING.md) to get involved and
[SECURITY.md](SECURITY.md) to report a vulnerability.

## License

[Apache-2.0](LICENSE). Typeface: Inclusive Sans (Olivia King), under the SIL
Open Font License.


### Phase 4 deployment

PostgreSQL can now store the event catalog and transcripts, in addition to the
vocabulary bank. Redis coordinates exclusive room shards; nodes proxy microphone
WebSockets, captions and controls to the room owner behind nginx. Independent
speech/translation backups, a provisioned Grafana dashboard, Prometheus alerts
and offline load/failure tests are included. See [Phase 4 setup and validation](docs/PHASE4.md)
for deployment commands, test coverage and operational limits. The two-node
Compose stack defaults to fake providers and spends no Sarvam credits.
