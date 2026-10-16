// Copyright 2026 the Dubash contributors. Apache-2.0.
// Loads DeepFilterNet3 for the speaker console: the WebAssembly module and
// the model, from this server or a CDN (the server says where), checked
// against their SHA-256 before use and kept in the browser's cache, so a
// speaker downloads them once (about 11 MB).
const FILES = {
  wasm: { name: 'df_bg.wasm', sha256: '9219e1b1125dbc8c22337888756607322287284a8119c4f233913825dfd42039' },
  model: { name: 'DeepFilterNet3_onnx.tar.gz', sha256: 'c94d91f70911001c946e0fabb4aa9adc37045f45a03b56008cb0c8244cb63616' },
};
const CACHE = 'dubash-deepfilternet3-v1';

async function sha256(buf) {
  const d = new Uint8Array(await crypto.subtle.digest('SHA-256', buf));
  return [...d].map((b) => b.toString(16).padStart(2, '0')).join('');
}

async function fetchChecked(base, file) {
  const url = new URL(file.name, base).href;
  let cache = null;
  try { cache = await caches.open(CACHE); } catch { /* no Cache API (private mode): fetch every time */ }
  let res = cache && await cache.match(url);
  if (!res) {
    res = await fetch(url, { mode: 'cors', credentials: 'omit' });
    if (!res.ok) throw new Error(`${file.name}: HTTP ${res.status}`);
    if (cache) await cache.put(url, res.clone()).catch(() => {});
  }
  const buf = await res.arrayBuffer();
  if (await sha256(buf) !== file.sha256) {
    if (cache) await cache.delete(url).catch(() => {});
    throw new Error(`${file.name} failed its integrity check`);
  }
  return buf;
}

let loading = null;

// load resolves with { module, model } (a compiled WebAssembly.Module and
// the model bytes), or rejects if the browser cannot run it.
export function load(base) {
  if (!loading) {
    loading = (async () => {
      if (!base) throw new Error('noise removal is not set up on this server');
      if (!WebAssembly.validate(new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 96, 0, 1, 123, 3, 2, 1, 0, 10, 10, 1, 8, 0, 65, 0, 253, 15, 253, 98, 11]))) {
        throw new Error('this browser lacks WebAssembly SIMD');
      }
      const [wasm, model] = await Promise.all([fetchChecked(base, FILES.wasm), fetchChecked(base, FILES.model)]);
      return { module: await WebAssembly.compile(wasm), model };
    })();
    loading.catch(() => { loading = null; });
  }
  return loading;
}

// node builds the noise-removal AudioWorkletNode in ctx (48 kHz).
export async function node(ctx, assets, attenLim) {
  await ctx.audioWorklet.addModule('/static/js/denoise-worklet.js');
  const n = new AudioWorkletNode(ctx, 'deepfilter', {
    numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1], channelCount: 1, channelCountMode: 'explicit',
    processorOptions: { module: assets.module, model: assets.model, attenLim },
  });
  await new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error('noise removal did not start')), 8000);
    n.port.onmessage = (e) => {
      if (e.data.ready) { clearTimeout(t); resolve(); }
      if (e.data.error) { clearTimeout(t); reject(new Error(e.data.error)); }
    };
  });
  return n;
}
