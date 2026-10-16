// Copyright 2026 the Dubash contributors. Apache-2.0.
// DeepFilterNet3 noise removal in the audio thread. The main thread
// compiles the WebAssembly and fetches the model (denoise.js) and hands
// both over; this re-frames the browser's 128-sample blocks into the
// model's 480-sample (10 ms at 48 kHz) hops. Output lags input by one hop
// plus the model's look-ahead (about 30 ms in all).
import { initSync, df_create, df_get_frame_length, df_process_frame, df_set_atten_lim } from './vendor/df.js';

class DeepFilter extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const { module, model, attenLim } = options.processorOptions;
    try {
      initSync({ module });
      this.st = df_create(new Uint8Array(model), attenLim);
      this.hop = df_get_frame_length(this.st);
    } catch (e) {
      this.failed = true;
      this.port.postMessage({ error: String(e) });
      return;
    }
    this.in = new Float32Array(this.hop);
    this.inLen = 0;
    this.queue = [];
    this.cur = null;
    this.curPos = 0;
    this.bypass = false;
    this.slow = 0;
    this.port.onmessage = (e) => {
      if (typeof e.data.attenLim === 'number') df_set_atten_lim(this.st, e.data.attenLim);
      if (typeof e.data.bypass === 'boolean') this.bypass = e.data.bypass;
    };
    this.port.postMessage({ ready: true, hop: this.hop });
  }

  process(inputs, outputs) {
    const input = inputs[0]?.[0];
    const output = outputs[0]?.[0];
    if (!input || !output) return true;
    if (this.failed || this.bypass) {
      output.set(input);
      return true;
    }
    for (let i = 0; i < input.length;) {
      const k = Math.min(input.length - i, this.hop - this.inLen);
      this.in.set(input.subarray(i, i + k), this.inLen);
      this.inLen += k;
      i += k;
      if (this.inLen === this.hop) {
        this.queue.push(df_process_frame(this.st, this.in));
        this.inLen = 0;
      }
    }
    // If processing ever falls far behind (a very slow device), drop the
    // backlog rather than grow the delay.
    if (this.queue.length > 8) {
      this.queue.splice(0, this.queue.length - 2);
      if (++this.slow === 1) this.port.postMessage({ slow: true });
    }
    for (let o = 0; o < output.length;) {
      if (!this.cur) {
        this.cur = this.queue.shift();
        this.curPos = 0;
        if (!this.cur) break; // the first hop is still filling: silence
      }
      const k = Math.min(output.length - o, this.cur.length - this.curPos);
      output.set(this.cur.subarray(this.curPos, this.curPos + k), o);
      o += k;
      this.curPos += k;
      if (this.curPos === this.cur.length) this.cur = null;
    }
    return true;
  }
}

registerProcessor('deepfilter', DeepFilter);
