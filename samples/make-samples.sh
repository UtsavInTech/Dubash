#!/usr/bin/env bash
# Regenerates the sample clips with Piper text-to-speech, run inside Docker
# (the macOS piper-tts wheel is broken), then converts them to the wire
# format: 16 kHz mono PCM16 WAV. Requires Docker and ffmpeg. See README.md
# for the voices and their licenses.
set -euo pipefail
cd "$(dirname "$0")"

en="Welcome to Nerdearla. Today we are going to talk about how open source \
communities make conferences more accessible. Real time captions help people \
who are deaf or hard of hearing, people who are learning English, and anyone \
sitting in the back of a noisy room. With Gemini, we can transcribe a talk as \
it happens and translate it into Spanish in less than a second. Kubernetes, \
eBPF and WebAssembly are the kind of words a good glossary should protect."

es="Bienvenidos a Nerdearla. Hoy vamos a hablar de cómo las comunidades de \
código abierto hacen que las conferencias sean más accesibles. Los subtítulos \
en tiempo real ayudan a personas sordas o con hipoacusia, a quienes están \
aprendiendo español y a cualquiera que esté sentado al fondo de una sala \
ruidosa. Con Gemini podemos transcribir una charla mientras sucede y \
traducirla al inglés en menos de un segundo."

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# The script runs in a throwaway container and streams the raw clips back
# as a tar archive on stdout.
docker run --rm -i -e EN="$en" -e ES="$es" python:3.12-slim bash -s > "$tmp/out.tar" <<'SH'
set -e
pip install -q piper-tts >/dev/null 2>&1
mkdir -p /v /o && cd /v
python - <<'PY'
import urllib.request as u
base = "https://huggingface.co/rhasspy/piper-voices/resolve/main/"
for p in ["en/en_US/ljspeech/high/en_US-ljspeech-high", "es/es_ES/davefx/medium/es_ES-davefx-medium"]:
    n = p.split("/")[-1]
    for ext in [".onnx", ".onnx.json"]:
        u.urlretrieve(base + p + ext, n + ext)
PY
echo "$EN" | piper -m en_US-ljspeech-high.onnx -f /o/en.wav >/dev/null 2>&1
echo "$ES" | piper -m es_ES-davefx-medium.onnx -f /o/es.wav >/dev/null 2>&1
tar -C /o -cf - .
SH

tar -C "$tmp" -xf "$tmp/out.tar"
for f in en es; do
  ffmpeg -loglevel error -y -i "$tmp/$f.wav" -ac 1 -ar 16000 -sample_fmt s16 "$f.wav"
done
ls -lh ./*.wav
