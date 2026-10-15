# Decisions

Every departure from the project brief, and every choice the brief left
open, with the reason. Newest last.

## Phase 0 (2026-10-16)

### D1. The benchmark is Go, inside the repo (`cmd/lenguaraz-bench`, `internal/bench`)

The brief asks for a "standalone benchmark script". It is standalone in use
(its own command, its own `bench/` directory, no server needed) but written
in Go in this module, not as a Python script. Reason: streaming providers
are then measured through the same `asr.Engine` code the server runs live,
so a latency number from the benchmark is a latency number for the
product. Phase 1's Sarvam streaming engine plugs in with one line.

### D2. Phase 0 talks to Sarvam through the REST API only

The benchmark's Sarvam recognizer uses `POST /speech-to-text` (clips of at
most 28 s, cut at the quietest moment), not streaming. Reason: the
streaming engine is phase 1's first deliverable and the brief says to stop
before phase 1. REST is enough to measure accuracy and the keyterm effect;
it cannot measure live latency, and the report says so. The REST client
lives in `internal/bench`, not `internal/asr`, so it is not mistaken for
the production adapter.

### D3. Sarvam translations run without the glossary mode

Sarvam's translate API has no glossary parameter. The benchmark skips the
"glossary on" run for translators that ignore the glossary (it would cost
the same and change nothing) and still reports how many keep-as-is and
approved terms survive. Protecting terms in our code is phase 2 work.

### D4. Translation is benchmarked on the reference transcript

The translation part of the benchmark translates the human reference
transcript, sentence by sentence, not the recognizer's output. Reason:
recognition and translation errors are then measured separately, and
translators are compared on identical input. End-to-end (recognized text
translated) belongs to rehearsal runs in phase 3.

### D5. Benchmark audio and results are not committed

`bench/recordings/*.wav` and `bench/results/` are git-ignored: event
recordings may be confidential, and results contain transcripts. The
YAML references for the synthetic smoke-test clips are committed; the
audio is regenerated with `bench/make-synthetic.sh` (macOS voices).

### D6. Synthetic clips are smoke tests, never accuracy figures

Synthetic recordings made with the macOS Indian voices are tagged
`synthetic`. They prove the pipeline and the provider integration work.
TTS speech is clean, unaccented and fluent, so it flatters every
recognizer; no accuracy claim may be based on it.

### D7. Character error rate alongside WER, chrF instead of BLEU

Telugu and Kannada are agglutinative: one wrong suffix makes a whole long
word wrong, so WER exaggerates small errors. The report gives CER too.
For translation it uses chrF (character n-grams), which is standard for
Indian languages and tolerant of segmentation differences.

### D8. Negation and number checks are word-list and digit based

Numbers are compared as digit sequences after mapping Indic digits to
ASCII; references must write figures as digits. Negations are counted from
per-language word lists. Telugu, Kannada and Tamil also negate with verb
suffixes, which a list cannot see, so those counts are a lower bound.
Hindi `ना` is also an emphatic particle, so the Hindi count can be
slightly high. Both limits are in the code comments and TESTING.md.

### D9. Go toolchain on the development machine

Homebrew's install hung in its auto-update, so Go 1.27.2 was installed
from go.dev into `~/sdk/go`. `go.mod` stays at 1.26.0, as upstream. The
CI's `staticcheck@v0.8.1` cannot read Go 1.27 export data, so it fails
locally under Go 1.27; CI uses the `go.mod` version and is unaffected.
`go vet` and gofmt are clean.
