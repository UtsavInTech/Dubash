# Provider notes

What we know about each speech and translation provider, where it came
from, and what is still unverified. **Read from documentation** means a
vendor page said so; **tested** means our own call or benchmark showed it.
Nothing here is "tested" yet unless marked. Re-check prices and limits
before quoting them: vendor docs changed between versions even while we
read them.

Last full pass: 2026-10-16.

## Sarvam (primary candidate for Indian languages)

### Speech to text

Read from documentation (docs.sarvam.ai, 2026-10-16):

- **Models.** `saaras:v4` is the default for REST and the legacy streaming WebSocket; `saaras:v3` is still accepted. `saaras:v3-realtime` (beta) is the model of a newer realtime endpoint. Older pages still call v3 current: the docs are mid-migration.
- **Endpoints.**
  - REST `POST https://api.sarvam.ai/speech-to-text`, multipart, header `api-subscription-key`. Meant for clips under 30 s. Fields: `file`, `model`, `language_code` (or `unknown`), `with_timestamps`, `input_audio_codec`, `keyterms`. `mode` only on v3.
  - Legacy streaming `wss://…/speech-to-text/ws`: **finals only, no partial transcripts**. VAD signals, manual flush, fine VAD tuning.
  - Realtime streaming `wss://api.sarvam.ai/speech-to-text-realtime/ws` (beta): `transcript.partial` and `transcript.final` messages, `vad.speech_start/end`, `session.end` with billed `audio_duration_s`, `error` with `is_fatal`. Audio as `{"event":"audio_input","audio":"<base64>"}`, PCM16 mono 8 or 16 kHz. `stream_type` `fast`/`balanced`/`simulated`. `prompt` (terminology hint, both models) and `keyterms` (v4 only). Live reconfiguration via `config.update`. Close 1008 on inactivity: needs pings.
- **Languages.** Realtime lists 23 + `auto`: en, hi, bn, kn, ml, mr, or, pa, ta, te, gu, ur, ne, kok, ks, sd, sa, sat, mni, brx, mai, doi, as (all `-IN`). **Odia is `or-IN` in realtime and `od-IN` in the legacy and translate APIs.**
- **Code-switching.** Modes `codemix` (English words stay in Latin script), `translit`, `verbatim`, `translate` (to English). On realtime, `mode` applies to finals only.
- **Custom vocabulary ("keyterms").** `saaras:v4` only; REST, batch and both WebSockets. **At most 50 terms of at most 64 characters each.** On WebSockets set at connect time for the whole session (the realtime endpoint's `config.update` may change that: unverified). Docs: keyterms "bias" recognition and "do not guarantee" the term appears. The Python and JS SDK `connect()` helpers do not expose `keyterms`; a raw WebSocket client is needed (fine: we write Go).
  - **Language coverage of keyterms is not documented.** The only examples are Hindi. **Whether keyterms help in Telugu, Kannada or Tamil is unverified**; the benchmark measures it (vocab off vs on, same audio).
- **Concurrency.** STT WebSocket connections per account: Starter 20, Pro 100, Business 100. The limiter also reacts to how fast connections open.
- **Session length limits.** Not stated anywhere we found. Must be tested (a 60–90 min talk).

### Translation

Read from documentation:

- `POST https://api.sarvam.ai/translate`, JSON, header `api-subscription-key`. Fields `input`, `source_language_code`, `target_language_code`, `model`, `mode` (`formal`, `modern-colloquial`, `classic-colloquial`, `code-mixed`), `speaker_gender`, `output_script`, `numerals_format`.
- `mayura:v1`: 11 languages, 1000 characters per request, all modes, `auto` source. `sarvam-translate:v1`: all 22 scheduled languages, 2000 characters, `formal` only.
- **No glossary or "do not translate" parameter.** Approved translations and keep-as-is terms must be protected in our own code (placeholder masking before the call, restoring after) or handled by an LLM translator. The benchmark's "terms kept" column shows how often such terms survive unprotected.
- **Rate limit: `POST /translate` 60 requests per minute on Starter**, 200 on Pro, 1,000 on Business, per account. Live captions make about 10–20 sentences per minute per event, one request per sentence per language: an event with 3 target languages is already 30–60 requests per minute, the whole Starter budget. **Starter cannot run a 3-language pilot reliably; plan for Pro, or batch.** Not yet tested.

### Prices

Read from docs.sarvam.ai/api-reference-docs/pricing (2026-10-16):

| Item | Price |
|---|---|
| Speech to text | ₹30 per hour, billed per second, rounded up per request |
| Speech to text with diarization | ₹45 per hour |
| Speech to text + translate (to English) | ₹30 per hour |
| Sarvam Translate V1, Mayura V1 | ₹20 per 10,000 characters |
| Transliteration | ₹20 per 10,000 characters |
| Bulbul v3 TTS, instant voice cloning | ₹30 per 10,000 characters (v2 feature) |

These match the figures in the brief. No separate streaming price is listed; we assume streaming bills at ₹30/h. **Free credit:** the docs page says ₹100 for new users; sarvam.ai/api-pricing was reported to say ₹1,000. The account we are using was granted ₹100 (as reported by the account owner).

Rough cost of one event hour, as an estimate to check against the benchmark: recognition ₹30 once, plus per target language about 12–15 characters of speech per second ≈ 45,000–55,000 characters per hour ≈ ₹90–110 per language per hour. **Translation, not recognition, dominates cost**, so translating only languages someone is reading matters.

### Tested (2026-10-16, REST API, synthetic clips only)

First real calls, on the five synthetic macOS-voice clips (about 51 s of
audio; total spend about ₹5.5). Synthetic speech is clean and fluent, so
these show *behaviour*, not accuracy to expect at an event.

- **Keyterms work in English and change output.** Without them: "Our partner Savmaya", "Linguaaz Pilot"; with `Sarvam AI` and `Lenguaraz` (the project's name at the time) as keyterms: "Our partner Sarvam", "Linguaraz pilot" (closer, still wrong: an invented brand name needs more than a hint, i.e. a correction stage). Term recall 3/6 → 4/6, no wrongly inserted terms.
- **Keyterms had no visible effect on the Hindi, Telugu and Kannada clips**, but those clips were already transcribed (near) perfectly, so this does not show whether they help. Real recordings with hard names are needed.
- **`saaras:v3` rejects keyterms** (`400: 'keyterms' is only supported by model 'saaras:v4'`), confirming the docs.
- **`saaras:v4` accepts `mode=codemix` on REST**, although the REST docs list `mode` for v3 only. This matters most for Hinglish:
  - default mode writes English words in Devanagari: "कुबेरनेट्स और क्लाउड माइग्रेशन … टीम … कॉस्ट … फीचर … फ्री" (WER 30.8% against a Latin-script reference);
  - `codemix` keeps them in Latin: "Kubernetes और cloud migration … team … cost … feature … free" (WER 3.8%).
  - On plain Hindi, `codemix` also writes common loanwords in Latin ("phone", "recording", "scan", "QR code", "Doctor"), which is a *style* choice, not an error. Which style an audience prefers should be an event setting; it also changes what the bank's terms must look like.
- **Number formatting varies.** "499 rupees" came back as "₹499"; "10 बजे" once as "10:00 बजे". Numbers stayed correct; scoring must normalize such forms (it does for ₹ and the digits).
- **Latency (REST, not streaming):** 0.6–0.9 s median per clip, 1.4–1.5 s p95. Live latency awaits the streaming adapter.
- **Translate latency: about 1.1 s median, 1.4 s p95 per sentence.** With recognition and sentence commit on top, this uses much of the 2–4 s translated-caption target.
- **Translate quality, first look:** numbers, negations and dates preserved in every output we read. Problems seen:
  - unprotected names transliterated: "Lenguaraz" → "लेंगुआरज़ / లెంగ్వారజ్ / ಲೆಂಗುರಾಜ್"; approved "बेंगलुरु" came out "बैंगलोर" from Kannada (terms kept: 33% hi, 20% te, 0% kn). Confirms term protection is needed.
  - **gender**: Hindi "दोस्तों" (friends) became feminine "friends" in Telugu (స్నేహితురాళ్ళు) and Kannada (ಗೆಳತಿಯರೇ).
  - slight embellishment: "40% cost बचाई" (saved 40% cost) → "saved 40% of the total bill" (te, kn).
- **Cost observed:** ₹31.3 per audio hour for recognition (per-second rounding per request), about ₹96–98 per audio hour per translated language. Matches the estimate above.

### First real speech (2026-10-16, REST, draft references)

Four 2-minute clips of Dhirendra Krishna Shastri (Bageshwar Dham), cut from
YouTube: a pravachan, a live Hanumant Katha with crowd and PA, a Divya
Darbar with devotees, and a TV9 studio interview. Fast, Bundeli-accented
Hindi; spend ₹7.9. Audio and transcripts stay local (`/bench/recordings/yt-*`
is git-ignored: third-party content).

References are YouTube's Hindi auto-captions, **not checked by a person**,
so the numbers measure agreement between two machines, not accuracy:

| Clip | Sarvam vs YouTube WER / CER | Vocabulary effect |
|---|---|---|
| pravachan | 9.8% / 5.7% | none |
| katha, live crowd | 9.9% / 5.6% | +0.6 pt (noise) |
| darbar | 10.3% / 6.1% | +1.3 pt (noise) |
| TV9 interview | 6.6% / 3.7% | +0.2 pt (noise) |

What the disagreements are, from a word diff (about 22–29 per clip):

- Mostly **spelling conventions**, not mishearings: यह/ये, वह/वो, यही/यहीं, है/हैं (respectful plural), ठीक-ठाक/ठीक ठाक, "4:00 बजे"/"4 बजे". Hindi has no single standard spelling for speech, so references need a written convention, and scoring may need a lenient Hindi mode that treats such pairs as equal.
- **Bundeli dialect forms** spelled differently (रुकिया/रुकियो): the reference convention must say whether dialect is kept.
- **One real omission:** Sarvam dropped "कांग्रेस" (a party name) in the interview. A dropped proper noun is the kind of error the Dictation Bank exists for.
- The domain vocabulary (`bench/vocab/bageshwar.yaml`, 23 terms) changed nothing measurable: every bank term present in the drafts was already recognized without it.
- REST latency per ~28 s clip: 0.5–0.8 s median, under 1 s p95.

### Not yet verified (needs a test)

- Accuracy on real Indian conference speech, Hinglish especially (benchmark pending recordings).
- Whether keyterms help outside Hindi.
- Realtime endpoint latency (partials and finals) and maximum session length.
- Whether the realtime endpoint's `config.update` can change keyterms mid-session (a live bank update at a segment boundary would need it).
- Translate latency at p95, and behaviour at the 60 rpm limit.

## Alternates

Everything below is from documentation only. None are integrated or tested.

### Deepgram

- Nova-3 supports Hindi (`hi`, monolingual, and inside `multi` code-switching), Telugu (`te`), Kannada (`kn`), Tamil (`ta`). Nova-2 has Hindi only (developers.deepgram.com, models overview).
- **Keyterm prompting** on Nova-3 and Flux, streaming included: up to 100 terms, 500 tokens total; can be replaced mid-stream with a `Configure` message, **except on the India, EU and Australia regional endpoints**. The page does not say which languages keyterms work for: **unverified for Indian languages.**
- Price: not checked.

### Google Cloud Speech-to-Text (Chirp 3)

- `StreamingRecognize` supports hi-IN (GA); te-IN, kn-IN, ta-IN are listed at **Preview**.
- Speech adaptation (phrase hints) is GA, up to 1,000 phrases; the page does not restrict it by language, so whether it helps in Indian languages is unverified.
- Available only in the `us` and `eu` multi-regions: **no India region**, so audio leaves India and latency is higher. Relevant for clients with data-residency requirements.
- Price: not checked.

### Azure AI Speech

- hi-IN, te-IN, kn-IN, ta-IN are supported for speech to text.
- **Phrase list (runtime vocabulary) is listed only for hi-IN** among the four. Custom Speech (trained models) supports all four with labelled audio or text. Custom Speech needs training data and time before an event; not a fit for "upload tomorrow's script".
- Price: not checked.

### Gemini (already integrated upstream)

- The repo uses `gemini-3.5-transcribe-live` with `CustomVocabulary` (`internal/asr/gemini.go:148`) and `gemini-3.5-flash-lite` for translation.
- Docs: custom vocabulary up to 1,000 terms, 100 recommended, incompatible with diarization; 85+ languages auto-detected with code-switching; the supported-languages table includes kn-IN. **hi-IN, te-IN, ta-IN not confirmed** (the table we saw was cut off).
- The repo's `languageCodes` map has no Indian entries, so today an Indian language is sent with no language hint.
- Gemini translation honours a glossary in the prompt (an LLM can be told "keep these terms"), which Sarvam translate cannot.
- Price: not checked. No key available, so not tested.

## Failover order (proposed, to confirm by benchmark)

Proposed, not decided: recognition Sarvam realtime → Deepgram Nova-3 (or Gemini Live); translation Sarvam translate (with our own term protection) → Gemini Flash-Lite. The choice waits for the recorded benchmark on representative audio (accents, Hinglish, names, numbers, noise), per the brief.

## Sources

- Sarvam streaming (legacy): https://docs.sarvam.ai/api-reference-docs/speech-to-text/apis/streaming
- Sarvam realtime streaming: https://docs.sarvam.ai/api/api-guides-tutorials/speech-to-text/realtime-streaming
- Sarvam keyterms: https://docs.sarvam.ai/api/api-guides-tutorials/speech-to-text/how-to/keyterms
- Sarvam REST STT: https://docs.sarvam.ai/api-reference-docs/speech-to-text/transcribe
- Sarvam translate: https://docs.sarvam.ai/api-reference-docs/text/translate-text
- Sarvam pricing: https://docs.sarvam.ai/api-reference-docs/pricing, https://sarvam.ai/api-pricing
- Sarvam rate limits: https://docs.sarvam.ai/api/getting-started/ratelimits
- Deepgram keyterms: https://developers.deepgram.com/docs/keyterm
- Deepgram languages: https://developers.deepgram.com/docs/models-languages-overview
- Google Chirp 3: https://docs.cloud.google.com/speech-to-text/v2/docs/chirp_3-model
- Azure language support: https://learn.microsoft.com/en-us/azure/ai-services/speech-service/language-support?tabs=stt
- Gemini transcribe: https://ai.google.dev/gemini-api/docs/models/gemini-3.5-transcribe
