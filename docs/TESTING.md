# Testing

## Suites

| Suite | Command | Result (2026-10-16, end of phase 1, macOS arm64, Go 1.27.2) |
|---|---|---|
| Go unit and integration tests | `go test -race ./...` | All tested packages pass (15); bank stores also against PostgreSQL with `DUBASH_TEST_PG` |
| Vet / format | `go vet ./...`, `gofmt -l .` | Clean |
| Browser end to end | `(cd e2e && npm ci && npx playwright install chromium && ./run.sh)` | 78 passed, 0 failed (49 upstream; a Hindi event read in Telugu; a live fix, the bank, a script and a rehearsal; phase 5: headers, a PIN event, a speaker link revoked while live) |
| Fake server smoke | `ADMIN_TOKEN=x go run ./cmd/dubash -fake`, then `head -c 384000 /dev/zero \| go run ./cmd/dubash-ingest -room sala-a -token x -realtime` | Interims, finals with translations, SRT export and metrics all work |
| staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...` | Fails locally under Go 1.27 (tool too old for its export data); CI runs it on Go 1.26 |

The e2e suite needs port 8080 free and uses `e2e/rooms.yaml`.

After merging phases 4 and 5 (branch `pilot`, 2026-10-16), also run
locally with a test PostgreSQL and Redis 7.2:

| Suite | Command | Result |
|---|---|---|
| Integration (PostgreSQL, Redis) | `TEST_DATABASE_URL=… TEST_REDIS_URL=… DUBASH_TEST_PG=… go test -race ./...` | All pass, including two-node ownership and cross-node SSE, the catalog without rooms.yaml, PostgreSQL transcripts across restarts, PostgreSQL retention and deletion |
| Two servers | `deploy/test-cluster.sh` | 100 readers per event through the non-owner, 0 errors, first caption p95 3.05 s; owner restart keeps its transcript |
| Load | `VIEWERS=200 DURATION=9s deploy/loadtest.sh` | 200 readers, 600 lines and 600 translations, 0 errors |
| Browser end to end | `e2e/run.sh` | 78 passed, 0 failed |

Not run locally: the Docker Compose stack (nginx, Prometheus, Grafana) —
Docker is not installed here; CI validates the Compose file and alert
rules.

### Rules with tests (phase 1)

| Rule | Test |
|---|---|
| Committed line published before its translation | `room.TestRoomPublishesSourceLineBeforeTranslation` |
| Translate once per line per language, only languages read (plus always_translate) | `room.TestRoomTranslatesOnlyLanguagesBeingRead` |
| A slow or failing language holds back nothing; every language in order | `room.TestRoomIsolatesLanguages` |
| Waiting lines batched; fallback line by line | `room.TestRoomBatchesWaitingLines` |
| Resume after Last-Event-ID; stale ids replay | `room.TestHubResumesAfterTheLastIDSeen`, `web.TestEventsResumeFromLastEventID` |
| Pause keeps audio from the recognizer | `room.TestPauseKeepsAudioFromTheRecognizer` |
| End stops the transmission; ingest told not to reconnect | `room.TestEndStopsTheTransmission`, `web.TestPauseResumeEnd` |
| Danda ends sentences; run-on limits in characters | `asr.TestSentenceEndsDanda`, `asr.TestRunOnCutCountsCharacters` |
| Committed text aligned, not counted, against rewritten finals | `asr.TestFinalAlignsRewrittenWords` |
| Sarvam: reconnect keeps text and audio, fatal errors stop, pings while idle | `asr.TestSarvam*` |
| Keep-as-is terms masked and restored, lost placeholder retried | `translate.TestMaskUnmask`, `translate.TestSarvamTranslator` |
| Approved translations used, by any spelling | `translate.TestApprovedTranslations` |
| Chat translator: numbered batches, unnumbered answers, transliteration retried then fallback | `translate.TestSarvamChat*`, `translate.TestParseNumbered`, `translate.TestTransliterated`, `translate.TestGarbled` |
| Bank: precedence event > speaker > org, approval, versions, merge of duplicates | `bank.TestResolvePrecedence`, `bank.TestStoreContract` |
| A version applies from the next line; editing the bank changes nothing live | `room.TestRoomUsesThePinnedVersion`, `web.TestBankFlow` |
| Live fix: revision to readers, re-translated, suggested to the bank | `room.TestCorrectPublishesARevision`, `web.TestBankFlow` |
| History translated for a new reader, once | `room.TestBackfillTranslatesTheHistory` |
| Cuts only at phrase ends | `asr.TestPhraseEnd`, `asr.TestRunOnCutsAtPhraseEnds` |
| Correction rules: numbers, negations, no insertion/deletion, evidence for ambiguous terms, deadline | `correct.TestRules`, `correct.TestNoInsertionsOrDeletions`, `correct.TestDeterministicCorrections`, `correct.TestModelEditsAreChecked`, `correct.TestModelDeadline` |
| Scripts: extraction, chunks, alignment, term suggestions | `script.Test*`, `bank.TestScriptSuggestions` |
| Cost per event | `web.TestCosts` |
| Speaker link limited to its event; revoked tokens refused; revocations persist | `web.TestPublisherTokenIsLimitedToItsEvent`, `web.TestPublisherTokens` |
| Token as WebSocket subprotocol; revoking cuts a live console (4003) | `web.TestIngestTokenAsSubprotocolAndRevocationCutsIt` |
| Lockout after 10 failures | `web.TestFailedAttemptsLockOut` |
| Audience PIN, QR join code, revoked codes, exports policy | `web.TestAudiencePIN`, `web.TestExportsOff` |
| Security headers; staff pages not framable | `web.TestSecurityHeaders` |
| Caption connection caps | `web.TestViewerCaps` |
| Retention and deletion (store, live fixes, audit text) | `web.TestRetentionAndDelete`, `store.TestDisableDeleteAndPurge`, `bank.TestPurgeCorrectionsContract` |
| Transcript files private | `store.TestTranscriptsArePrivate` |
| Weak or empty admin token refused on a public address | `main.TestCheckToken` |
| Privacy settings validated | `config.TestParseErrors`, `config.TestPrivacySettings` |

### Live checks against real providers (manual, cost money)

```bash
set -a; source .env; set +a
go run ./cmd/sarvam-smoke -lang hi-IN -mode codemix < clip.pcm      # raw realtime messages
SARVAM_PROBE=1 go test -run ProbeSarvam -v ./internal/translate    # placeholder, line-break, chat-model probes
SARVAM_PROBE=1 go test -run ProbeModel -v ./internal/correct        # correction model on the adversarial cases
SARVAM_REPLAY=capture.txt go test -run ReplaySarvam -v ./internal/asr  # replay a capture offline
go run ./cmd/dubash-bench -asr sarvam-stream -translate '' -only hi-   # streaming accuracy and latency
```

## Benchmark (`cmd/dubash-bench`)

Measures recognition and translation providers on recorded audio. Every
measurement is taken on the same audio with and without the vocabulary,
so the vocabulary's effect is the difference between two rows.

```bash
# Offline: scripted recognizer and fake translator, no API calls
go run ./cmd/dubash-bench -asr scripted -translate fake

# Sarvam: recognition (REST, saaras:v4) and translation
set -a; source .env; set +a          # SARVAM_API_KEY
go run ./cmd/dubash-bench -asr sarvam -translate sarvam -to hi,te,kn

# Only some recordings, only with vocabulary
go run ./cmd/dubash-bench -asr sarvam -translate '' -only hi- -vocab on
```

It prints a cost estimate and **refuses to run above `-budget` rupees
(default ₹10)** unless given `-yes`. Results go to
`bench/results/<timestamp>/report.md` and `results.json` (git-ignored).

### Adding recordings

See `bench/README.md`. In short: `NAME.wav` (16-bit PCM, any rate; mono
preferred) and `NAME.yaml` with `lang`, `reference`, optional
`vocabulary`, `tags` and reference `translations`, in `bench/recordings/`.

### Metrics

| Metric | Definition | Caveat |
|---|---|---|
| WER | word edit distance / reference words, after normalization (NFC, lower case, punctuation and danda removed, Indic digits → ASCII, `1,00,000` → `100000`) | Hurts agglutinative languages; read with CER. A recognizer that writes English words in Devanagari scores badly against a Latin-script reference even when it heard right. |
| CER | the same on characters | |
| Term recall | vocabulary terms (and aliases) in the reference that the hypothesis also has | Counted per occurrence, longest spelling first, no overlaps. |
| Inserted terms | occurrences of a term in the hypothesis beyond those in the reference | The "bank inserted something never said" failure. |
| Mis-heard | known wrong spellings (`misheard:` in the vocabulary) still present | |
| Numbers right | reference digit sequences reproduced | References must write figures as digits. |
| Negations agree | same count of negation words in reference and hypothesis | Word-list based; suffix negation in te/kn/ta is invisible. |
| First final, lag p50/p95 | streaming engines: arrival time minus send time of the reported audio position, paced in real time (`-speed 1`). REST: request time per clip | REST lag is not live latency. |
| chrF | chrF2 against a reference translation, when the YAML provides one | |
| Terms kept | keep-as-is terms unchanged, and terms with an approved translation rendered as approved | Checked per sentence. |
| Cost | billed seconds × rate; characters × rate (`bench/pricing.yaml`) | Unknown rates show as `?`, never as free. |

### Adversarial cases to record (wanted)

Each should exist as a real recording with a tag:

- `changed-number`: the speaker says a figure different from the script.
- `ambiguous`: an ordinary word that is also a bank alias (e.g. "apple" the fruit with "Apple" in the bank).
- `accent`: the same text read by speakers from different regions.
- `code-switch`: Hindi and English inside one sentence.
- `negation`: "this is not free", "we will not record".
- `noise`: venue noise, a PA system, applause.

### Know Your Speaker (Phase A)

The `speaker` package store contract runs against the JSON store and, when
`DUBASH_TEST_PG` is set, PostgreSQL. Web tests cover organiser authentication,
required consent before upload, written-permission enforcement for organiser
acceptance, rejection of synthetic-voice consent in Phase A, allowed retention
periods and audio signal checks. The hourly retention pass removes expired
sample metadata and files. `e2e/e2e.mjs` exercises profile creation, separate
consent recording, warning/document links, a suggested speaker phrase, WAV
upload checks and mobile layout.

Voice-sample duration is checked server-side. WAV is decoded with the existing
`bench.DecodeWAV`; MP3/M4A must contain readable duration metadata and are
explicitly shown as not analysed. No paid speech or translation provider is
called by the feature or its tests. Run `go test -race ./...`, `go vet ./...`,
`staticcheck ./...` (v0.8.2), and `e2e/run.sh` when the Go toolchain and browser
runtime are installed.
