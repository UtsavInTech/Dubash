# Testing

## Suites

| Suite | Command | Phase 0 result (2026-10-16, macOS arm64, Go 1.27.2) |
|---|---|---|
| Go unit and integration tests | `go test -race ./...` | All 10 tested packages pass (8 upstream + `internal/bench`) |
| Vet / format | `go vet ./...`, `gofmt -l .` | Clean |
| Browser end to end | `(cd e2e && npm ci && npx playwright install chromium && ./run.sh)` | 49 passed, 0 failed |
| Fake server smoke | `ADMIN_TOKEN=x go run ./cmd/lenguaraz -fake`, then `head -c 384000 /dev/zero \| go run ./cmd/lenguaraz-ingest -room sala-a -token x -realtime` | Interims, finals with translations, SRT export and metrics all work |
| staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...` | Fails locally under Go 1.27 (tool too old for its export data); CI runs it on Go 1.26 |

The e2e suite needs port 8080 free.

## Benchmark (`cmd/lenguaraz-bench`)

Measures recognition and translation providers on recorded audio. Every
measurement is taken on the same audio with and without the vocabulary,
so the vocabulary's effect is the difference between two rows.

```bash
# Offline: scripted recognizer and fake translator, no API calls
go run ./cmd/lenguaraz-bench -asr scripted -translate fake

# Sarvam: recognition (REST, saaras:v4) and translation
set -a; source .env; set +a          # SARVAM_API_KEY
go run ./cmd/lenguaraz-bench -asr sarvam -translate sarvam -to hi,te,kn

# Only some recordings, only with vocabulary
go run ./cmd/lenguaraz-bench -asr sarvam -translate '' -only hi- -vocab on
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
