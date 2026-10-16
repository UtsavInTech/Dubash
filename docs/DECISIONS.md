# Decisions

Every departure from the project brief, and every choice the brief left
open, with the reason. Newest last.

## Phase 0 (2026-10-16)

### D1. The benchmark is Go, inside the repo (`cmd/dubash-bench`, `internal/bench`)

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

### D10. Scoring ignores the Devanagari nukta

"फ़ोन" and "फोन" are the same word to a reader; recognizers and people use
the nukta inconsistently. The first Sarvam run counted it as an error, so
normalization now drops it before comparing.

### D11. Caption script style for Hinglish is an open product decision

Sarvam's `codemix` mode writes English words in Latin script inside Hindi
captions; the default writes them in Devanagari. Both are correct, and the
audience's preference may differ by event (a tech conference vs. a
government training). Phase 1 should expose it per event; until the user
decides, the default stays the provider's.

### D12. Renamed to Dubash (2026-10-16, decided by the project owner)

*Dubash* (from *do-bhashi*, "two-language person") were the interpreters
of colonial Madras, Bombay and Calcutta: the Indian counterpart of
*lenguaraz*, the interpreters of the Río de la Plata. The rename is
complete: product name in every page and doc, binaries (`dubash`,
`dubash-ingest`, `dubash-bench`), Go module path
`github.com/UtsavInTech/dubash`, Prometheus prefix `dubash_`, the
ingest URL variable `DUBASH_URL`, Docker service and volume, and
localStorage keys (viewers' saved reading settings reset once). NOTICE
and the README keep crediting Lenguaraz. README.es.md and docs/obs.md
remain the upstream Spanish guides, renamed only. A synthetic benchmark
run recognized "Dubash" correctly with and without it as a keyterm, where
"Lenguaraz" was misheard both ways.

The git remote `origin` still points at the upstream repository; nothing
has been pushed. The module path assumes a future
`github.com/UtsavInTech/dubash` repository.

## Phase 1 (2026-10-16)

### D13. Dialect is kept as spoken (decided by the project owner)

References and captions keep the speaker's dialect and spelling: Bundeli
forms (रुकियो, भए), ये/यह as said. Nothing is standardized, in the
benchmark references or in the product.

### D14. Sarvam commits finals only, by default

Sarvam's partial transcripts are rougher than its finals: no
punctuation, "दस" where the final has "10", run-together words, and
words the final corrects. Committing from them (stable words in long
utterances, or sentences a partial happens to punctuate) raised word
error by 1 to 13 points on our Hindi clips and, before the alignment
fix (D15), duplicated text. So the default commits only Sarvam's
utterance finals, which arrive 0.1–0.25 s after the speaker pauses
(p95 0.3 s on real speech). The cost: a long utterance (we measured up
to 18 s in a fast interview) reaches the audience only when it ends.
`SARVAM_EARLY=on` trades accuracy for latency. **Open for the owner:**
which to prefer, ideally decided on recordings of a real pilot speaker.

### D15. Committed text is found by alignment, not by counting

The upstream assembler skipped already-committed text in a later
interim or final by counting words and sentences. Sarvam's finals split
sentences differently from its partials and revise earlier words, which
repeated a sentence and dropped a word on a TV9 interview. Committed
words are now located by edit distance (characters first, so "मुख्यवक्ता"
matches "मुख्य वक्ता"), searching a few words around their count. The
Gemini engine uses the same code; its tests pass unchanged.

### D16. One message per line, then one per translation

Upstream sent a line once, with every translation inside, after the
slowest translation. Now the committed line goes out at once and each
translation follows as its own `translation` event, only to readers of
that language. Brief section 3 asks for exactly this; it changed the SSE
protocol, so the upstream tests that read translations from the final
event were rewritten (same checks, new messages), and the 49 upstream
browser checks pass against the new viewer.

### D17. Translate only languages being read, with an export escape hatch

A language is translated while at least one viewer reads it. Rooms may
list `always_translate` languages, translated regardless, so their
transcript export is complete. Lines spoken while nobody read a
language stay untranslated in it; a viewer who switches language sees
them as "(not translated)". **Open for the owner:** whether to
translate the last few lines when a language gains its first reader.

### D18. Batching and a burst-tolerant limiter for Sarvam's rate limit

Sarvam Starter allows 60 translate requests a minute per account. With
one request per line per language and a limiter spacing requests a
second apart, a five-sentence utterance in three languages waited up
to 15 s. Each language's worker now sends the lines already waiting in
one request (Sarvam keeps one output line per input line), and the
limiter lets a tenth of the minute's budget go at once. Measured on the
same live test: translation delay p50 1.0–2.0 s, p95 2.3–4.5 s (was
4.7–6.7 s and 13.5–15.6 s). Context from neighbouring lines also fixed a
wrong word sense (शर्त: "conditions" became "bet"). A busy event with
four or more languages still needs Sarvam's Pro plan.

### D19. Keep-as-is terms are masked with placeholders for Sarvam

Sarvam translate has no glossary. Keep-as-is terms are replaced by
"[n]" before the call and restored after; Sarvam kept all seven
placeholder shapes tried in hi, te and kn. A translation that loses a
placeholder is redone unmasked, so no word is lost. A placeholder hides
what the term is (a person, a product), which can change the grammar
around it; acceptable for names and brands.

### D20. Indian scripts: Noto Sans subsets in the binary

Inclusive Sans has no Indian scripts. Noto Sans subsets for nine
scripts (Google Fonts, SIL OFL) are embedded, 830 KB in all, under one
family with unicode ranges, so a phone downloads only the scripts on
screen (34–124 KB each); characters several subsets share (danda, ₹)
are left to Devanagari, or a Hindi line also pulled the Punjabi font.
The UI strings for Hindi, Telugu, Kannada and Tamil were written by us
and **need review by native speakers** before a pilot.

### D21. Pause discards audio before recognition

Pause keeps the speaker's connection but sends nothing to the
recognizer: nothing said is captioned, stored or billed. The Sarvam
session pings while idle. End stops the transmission with WebSocket
close code 4001, which `dubash-ingest` and the console treat as final
(no reconnect), and readers see an ended page.

### D22. The browser suite keeps its own rooms file

`rooms.yaml` is now an Indian example (Hindi main hall, English hall B).
The E2E suite runs with `e2e/rooms.yaml`, the upstream Spanish/English
rooms plus a Hindi room, so the upstream checks keep testing what they
were written for.

## Phases 2 and 3 (2026-10-16)

### D23. Owner decisions recorded

- Commit as the speaker speaks, in natural phrases, never mid-phrase
  ("भाई, तुम क्या | कर रहे हो" is wrong). See D27.
- Readers select a language and then see the history in it (D25).
- Cost target: ₹30-40 for an event hour (D26).
- Accuracy matters more than structure (D28, D29).

### D24. Dictation Bank storage: PostgreSQL, or a file

The bank (entries, versions, pins, corrections, scripts, audit log) is
in PostgreSQL when DATABASE_URL is set, else in data/bank.json. Both
pass one contract suite (`bank.TestStoreContract`,
`bank.TestScriptsContract`; PostgreSQL with DUBASH_TEST_PG). Migrations
are embedded and applied under an advisory lock. Events stay in
rooms.yaml until phase 4. pgx (MIT) is the driver.

The live path never reads the store: a room holds the pinned version as
an immutable in-memory context, so a database outage cannot stop an
event; a new version applies from the next committed line, never
mid-line, and recognition hints at the next session (Sarvam fixes
keyterms per connection).

### D25. History for every reader

When a reader opens a language, the recent lines (up to 50) without a
translation into it are translated in batches of up to 20, once per
language; later readers get them from the replay. Replaces D17's open
question.

### D26. Translate with Sarvam's chat model by default

Sarvam Translate costs ₹20 per 10,000 characters, about ₹100 per
language per caption hour. Sarvam's chat model (sarvam-105b, reasoning
off) is priced per token. Measured live (60 s of Hindi read in English,
Telugu, Kannada): ₹39.5 per hour in all, ₹30 of it recognition, and
translations in 0.4-0.6 s instead of 1-2 s. Quality on our lines was at
least as good (correct word senses, approved terms kept), except that
it sometimes only re-spelled dialect lines in the target script; such
lines (detected by mapping the scripts' parallel Unicode layout back to
Devanagari) are retried with their neighbours, then sent to Sarvam
Translate. The admin panel shows the cost so far and per hour.

### D27. Phrase boundaries for every cut

Early commits (long utterances) and the run-on guard cut only after
sentence punctuation, a clause-final verb or auxiliary, or before a
clause-opening conjunction; never after a pronoun, question word,
postposition, vocative or article, nor before a trailing tag (यार, जी).
With no phrase end, the guard waits for twice its length limit. The
upstream test for the old cut-anywhere behaviour was changed to match.

### D28. Correction is bounded and every edit is checked

Order: the bank's known mis-hearings; near misses of bank terms (same
script, one letter in five); then, with evidence, Sarvam's chat model
given the matching bank entries, the aligned script passage (only when
the aligner is confident) and the previous lines, within 800 ms. Every
edit, rule or model, must: exist in the line as whole words; keep
numbers and negations; not insert unspoken words nor drop spoken ones;
write only words from the bank or the script passage. Ambiguous entries
(ordinary words) need the script. On a live adversarial probe the model
proposed harmful edits in half the lines (a number "fixed" to match the
script, a dropped नहीं, a translated word, an inserted script word); the
rules refused all of them. The model alone is not trusted.

### D29. Scripts: text, .md and .docx; PDF later

PDF text extraction in Indian scripts is often broken by font encodings
and would need OCR; admins paste text or upload .docx for now. Scripts
are data: stored, chunked, searched, shown to the correction model as a
passage, never followed as instructions and never shown to readers.

### D30. Wispr Flow

Wispr Flow has no public speech-to-text API (its developer page
mentions one without details and points to sales, 2026-10-16). Its
accuracy comes from a strong model plus context (a personal dictionary,
what is being said), which is what the Dictation Bank, the script and
the correction stage provide here.

### D31. Interface strings checked against Sarvam

Our Hindi, Telugu, Kannada and Tamil interface text was compared with
Sarvam's translation of the English (translate.TestProbeUIStrings). The
meaning matched everywhere; Sarvam used more English loanwords
(ईवेंट, సెట్టింగ్స్) where ours use native words (कार्यक्रम,
సెట్టింగ్‌లు), and it made two errors of its own. We keep ours.

### D32. Accuracy first: finals only, with a 300 ms end-of-speech window

Streamed live on the four real clips (references: YouTube drafts),
committing early from partials (even at phrase boundaries) against
committing only Sarvam's finals:

| Clip | WER early | WER finals only | first caption, finals only (500 ms) | (300 ms) |
|---|---|---|---|---|
| darbar | 14.7% | 10.6% | 18.6 s | 18.6 s, 10.3% |
| katha (live crowd) | 11.9% | 11.3% | 5.4 s | |
| pravachan | 16.4% | 10.1% | 8.7 s | |
| TV9 interview | 13.6% | 6.8% | 12.9 s | 2.2 s, 7.0% |

Partials are rougher than finals, so early commits cost 1 to 7 points.
Finals only now matches Sarvam's REST accuracy. Shortening Sarvam's
end-of-speech window to 300 ms (SARVAM_SILENCE_MS) gives finals at the
speaker's short pauses at no accuracy cost. A speaker who does not pause
for 300 ms (18 s at the start of the darbar) still arrives in one piece;
SARVAM_EARLY=on trades accuracy for those cases.
