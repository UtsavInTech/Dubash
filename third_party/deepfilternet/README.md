# DeepFilterNet3 for the speaker console

Background-noise removal in the speaker's browser, before audio is sent
for recognition: [DeepFilterNet](https://github.com/Rikorose/DeepFilterNet)
by Hendrik Schröter et al., licensed MIT OR Apache-2.0 (copies here).
Chosen over RNNoise (BSD) and browser built-in suppression for quality:
PESQ 3.17 on VoiceBank+DEMAND for DeepFilterNet3 against 2.34 for RNNoise
(published figures), while still running several times faster than real
time in WebAssembly.

## Files

| File | What |
|---|---|
| `df_bg.wasm` | libDF compiled to WebAssembly (SIMD) |
| `DeepFilterNet3_onnx.tar.gz` | the DeepFilterNet3 model, unchanged from upstream `models/` |
| `SHA256SUMS` | checked by the console before use |
| `../../internal/web/static/js/vendor/df.js` | the matching wasm-bindgen glue |

## How they were built (reproducible)

```bash
git clone https://github.com/Rikorose/DeepFilterNet && cd DeepFilterNet
git checkout d375b2d8309e0935d165700c91da9de862a99c31
cargo update -p wasm-bindgen --precise 0.2.100   # 0.2.92's CLI no longer builds
RUSTFLAGS="-C target-feature=+simd128" cargo build --release \
  --target wasm32-unknown-unknown -p deep_filter --lib --no-default-features --features wasm
cargo install wasm-bindgen-cli --version 0.2.100 --locked
wasm-bindgen target/wasm32-unknown-unknown/release/df.wasm --out-dir out --target web --no-typescript
```

rustc 1.85.1. The model is `models/DeepFilterNet3_onnx.tar.gz` from the
same commit.

## Measured (2026-10-17, Apple M-series, Node 22, one thread)

Clean Hindi speech mixed with three other talkers plus pink noise,
48 kHz, attenuation limit 24 dB; SI-SDR in dB, higher is better.

| Input SNR | Noisy | Enhanced |
|---|---|---|
| 10 dB | 10.0 | 14.4 |
| 5 dB | 5.0 | 9.4 |
| 0 dB | 0.0 | 3.1 |

Real-time factor 0.13–0.17 (about 7× faster than real time); added delay
30 ms. A limit of 24 dB scored slightly better than no limit and avoids
the over-suppression artefacts that hurt speech recognition.
