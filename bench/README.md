# Benchmark data

```
bench/
  recordings/   NAME.wav + NAME.yaml per recording (the .wav files are git-ignored)
  vocab/        vocabulary lists, referenced by name from a recording
  pricing.yaml  provider rates in rupees, with the date they were checked
  results/      one directory per run (git-ignored)
  make-synthetic.sh   recreates the synthetic smoke-test clips (macOS)
```

## Add a recording

1. Convert the audio to 16-bit PCM WAV. Any sample rate works (it is
   resampled to 16 kHz); mono is best:

   ```bash
   ffmpeg -i talk.m4a -ac 1 -ar 16000 -sample_fmt s16 bench/recordings/hi-panel-01.wav
   ```

   Short is fine: 30 s–3 min per clip keeps costs low and failures
   readable. A long talk is better cut into several clips with their own
   references.

2. Write `bench/recordings/hi-panel-01.yaml`:

   ```yaml
   lang: hi                 # main spoken language: en, hi, te, kn, ta…
   vocabulary: acme-event   # optional: bench/vocab/acme-event.yaml
   tags: [code-switch, accent, numbers, noise]
   reference: >-
     Exactly what was said, in the script the language is normally written
     in, figures as digits (499, not "four ninety-nine"). For Hinglish,
     write English words the way you want captions to show them.
   translations:            # optional, for chrF
     te: మానవ అనువాదం…
   notes: Speaker from Lucknow, hall PA, applause at 1:10.
   ```

3. Run `go run ./cmd/lenguaraz-bench -only hi-panel`.

## Vocabulary file

```yaml
terms:
  - term: Sarvam AI          # canonical spelling
    aliases: [Sarvam]        # also correct
    misheard: [sarwam]       # known wrong recognitions (reported, not sent)
    keep: true               # never translate
  - term: Bengaluru
    translations: {hi: बेंगलुरु, te: బెంగళూరు, kn: ಬೆಂಗಳೂರು}
```

Sarvam takes at most 50 hints of at most 64 characters; extra ones are
dropped and the report says so.

## Synthetic clips

`make-synthetic.sh` creates five clips (en-IN, hi-IN ×2, te-IN, kn-IN)
with the macOS voices. They are tagged `synthetic` and only prove that the
pipeline runs; they are not evidence of accuracy.
