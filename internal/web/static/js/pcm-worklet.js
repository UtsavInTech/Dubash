// Converts the (mono) input to 16 kHz PCM16 and posts 100 ms chunks.
// The AudioContext is created at 16 kHz where the browser allows it, so the
// ratio is usually 1. With noise removal it runs at 48 kHz: a whole-number
// ratio is decimated through a windowed-sinc low-pass filter (so noise
// above 8 kHz does not fold back into the speech band); other ratios fall
// back to averaging each window.
//
// Modified by the Dubash contributors: FIR decimation.
const TAPS = 63;

// lowpass returns a Blackman-windowed sinc with cutoff fc (cycles/sample).
function lowpass(fc) {
  const h = new Float32Array(TAPS);
  const m = (TAPS - 1) / 2;
  let sum = 0;
  for (let i = 0; i < TAPS; i++) {
    const x = i - m;
    const sinc = x === 0 ? 2 * fc : Math.sin(2 * Math.PI * fc * x) / (Math.PI * x);
    const w = 0.42 - 0.5 * Math.cos((2 * Math.PI * i) / (TAPS - 1)) + 0.08 * Math.cos((4 * Math.PI * i) / (TAPS - 1));
    h[i] = sinc * w;
    sum += h[i];
  }
  for (let i = 0; i < TAPS; i++) h[i] /= sum;
  return h;
}

class PCM16k extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ratio = sampleRate / 16000;
    this.integer = Number.isInteger(this.ratio) && this.ratio > 1;
    if (this.integer) {
      // Pass up to about 7.2 kHz, stop by 8 kHz (the new Nyquist).
      this.h = lowpass(7600 / sampleRate);
      this.hist = new Float32Array(TAPS);
      this.hpos = 0;
      this.phase = 0;
    }
    this.acc = 0;
    this.count = 0;
    this.pos = 0;
    this.out = new Int16Array(1600);
    this.n = 0;
  }

  emit(s) {
    s = Math.max(-1, Math.min(1, s));
    this.out[this.n++] = s < 0 ? s * 0x8000 : s * 0x7fff;
    if (this.n === this.out.length) {
      this.port.postMessage(this.out.buffer, [this.out.buffer]);
      this.out = new Int16Array(1600);
      this.n = 0;
    }
  }

  process(inputs) {
    const input = inputs[0][0];
    if (!input) return true;
    if (this.integer) {
      for (let i = 0; i < input.length; i++) {
        this.hist[this.hpos] = input[i];
        this.hpos = (this.hpos + 1) % TAPS;
        if (++this.phase === this.ratio) {
          this.phase = 0;
          let y = 0;
          for (let k = 0, j = this.hpos; k < TAPS; k++, j = (j + 1) % TAPS) y += this.h[k] * this.hist[j];
          this.emit(y);
        }
      }
      return true;
    }
    for (let i = 0; i < input.length; i++) {
      this.acc += input[i];
      this.count++;
      this.pos += 1;
      if (this.pos >= this.ratio) {
        this.pos -= this.ratio;
        this.emit(this.acc / this.count);
        this.acc = 0;
        this.count = 0;
      }
    }
    return true;
  }
}

registerProcessor('pcm-16k', PCM16k);
