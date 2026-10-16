# Sample clips

`en.wav` (~28 s) and `es.wav` (~22 s) are short talk openings used to try
Dubash without a microphone (the operator page's **sample EN** /
**sample ES** links) and to check the Live API from the terminal with
`cmd/asr-smoke`. Both are 16 kHz mono PCM16 WAV, the wire format.

They are synthetic speech generated with [Piper](https://github.com/rhasspy/piper)
from the text in `make-samples.sh`, using these voices from
[rhasspy/piper-voices](https://huggingface.co/rhasspy/piper-voices):

| Clip | Voice | Trained on | Dataset license |
|------|-------|------------|-----------------|
| `en.wav` | `en_US-ljspeech-high` | [LJ Speech](https://keithito.com/LJ-Speech-Dataset/) | Public domain |
| `es.wav` | `es_ES-davefx-medium` | davefx (Spanish, Spain) | CC0 |

No real person's recorded voice is redistributed here.

To regenerate them (needs Docker and ffmpeg; Piper runs in a
`python:3.12-slim` container because the macOS `piper-tts` wheel is broken):

```bash
samples/make-samples.sh
```
