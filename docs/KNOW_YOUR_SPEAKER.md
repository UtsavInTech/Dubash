# Know Your Speaker

Dubash learns each speaker before their event: how they speak, the words
and slang they use and what those mean, so captions and translations
carry their meaning, not just their words. Later, and only with explicit,
separate consent, their voice can speak the translations.

This is a design to build in phases. It is not legal advice: the consent
text and terms must be reviewed by a lawyer before public use.

## Why consent comes first

A voice is personal data (India's Digital Personal Data Protection Act,
2023, requires free, specific, informed consent for a stated purpose, and
the right to withdraw and to have the data erased). A cloned voice can be
used to fake speech: a deepfake. India's IT Rules amendments on
"synthetically generated information" (2025) move towards mandatory
labelling of AI-generated audio. So:

- Each use is a separate, opt-in permission. Nothing is pre-ticked.
- Voice cloning is never enabled by the organiser alone: the speaker
  themself must accept, and it can be withdrawn at any time.
- Anything spoken in a synthetic voice is labelled as such to listeners.
- Withdrawing deletes the recordings, transcripts and any voice model.

## Phases

### Phase A: profile, consent, vocabulary (build now)

After sign-in, the studio gets **Know Your Speaker** in the sidebar.

1. **Speaker profile**: name, how they want to be shown, the languages
   they speak (and mix), their style (discourse, comedy, politics,
   lecture, other), and their usual events.
2. **Consent** (each a separate checkbox, with the version and time of
   acceptance recorded, and who accepted):
   - *Recognition*: "Use my recordings and word list to recognise my
     speech better." Required for Phase A.
   - *Storage*: keep recordings for 30, 90 or 365 days (default 90),
     then delete them automatically.
   - *Synthetic voice* (Phase C, shown but disabled until then): "Create
     a synthetic copy of my voice to speak translations." Off by default;
     requires the speaker's own confirmation (a link sent to them), not
     the organiser's.
   - Terms and conditions and a privacy notice, linked, accepted with a
     checkbox; the accepted version is stored.
   - Who is accepting: "I am the speaker" or "I am the organiser and
     have the speaker's written permission" (then the speaker gets a
     link to confirm themself).
3. **Voice samples**: up to 30 minutes in total of clean recordings
   (WAV, MP3, M4A; files of 1-15 minutes), with an optional transcript
   each. The page shows a progress bar towards 30 minutes and checks each
   file (length, loudness, clipping, background noise) and says so.
4. **My words**: the speaker (or organiser) lists the words and phrases
   the speaker uses that people may not know: the phrase as said, what it
   means, and how it should come across in each language. For example:

   | Said | Meaning | English | Kannada |
   |---|---|---|---|
   | खटिया खड़ी कर देंगे | we will ruin you / beat you badly (rustic, threatening, half-joking) | "we'll finish you off" | the equivalent idiom, not a word-for-word line |
   | ठठरी बँध जाए / ठठरी के | literally "may your bier be tied": a rustic curse, used half-jokingly as a rebuke | "damn you!" / "get lost, you rascal!" (playful, not literal) | an equivalent playful rebuke |

   (Examples to show the idea; the speaker confirms the meanings.)

   Each line becomes a Dictation Bank entry at **speaker** scope (status
   *suggested*), with the meaning and per-language translations, so the
   organiser approves it like any other entry.
5. **Rehearsal**: run one sample through recognition with and without the
   speaker's vocabulary and show the difference (the existing rehearsal).

Data kept per speaker: profile; consent records (each permission, its
text version, time, who, how); samples (file, duration, checks, uploaded
when, delete after); phrases (as bank entries). Recordings are private to
the organisers and deleted on schedule or when consent is withdrawn.

### Phase B: learn from the samples

- Transcribe each sample (Sarvam REST; costs about ₹0.5 a minute, shown
  before running).
- Propose vocabulary from the transcripts: names, repeated rare words,
  Hinglish, slang candidates, as bank suggestions (the code that does this
  for scripts already exists).
- Per-speaker recognition hints: their top terms go to the recognizer's
  keyterm list (Sarvam takes up to 50) when they are live.
- A per-speaker accuracy score from the rehearsal, tracked over time.

Note what this is and is not: Sarvam does not let us retrain its model on
a speaker's voice. Phase B adapts around it (vocabulary, hints, the
correction stage with the speaker's glossary). Truly training on the
voice needs an open model we host (e.g. AI4Bharat's Indic models): a
research track, judged by measured accuracy, not assumed.

### Phase C: the speaker's voice (research, consent-gated)

Phones speak the translation in a synthetic voice resembling the
speaker. Prerequisites: the speaker's own confirmed consent; a model whose
licence allows commercial use and that supports the target Indian
languages; labelling of synthetic speech; latency measured end to end
(target: the spoken translation 2-4 s behind the speaker). Lip sync is
out of scope for live events.

## Who builds what

- Codex: vocabulary corrections; Phase A implementation to this design;
  the Phase C model survey (research only, no code).
- Claude: this design; review of Codex's work (security, privacy, data
  handling, tests); deployment; the Phase B recognition integration; the
  decision on any Phase C model after the survey.
