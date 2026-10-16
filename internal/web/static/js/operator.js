// Modified by the Dubash contributors: English, pause, end of event,
// listeners per language.
import { NAMES } from './langs.js';
import * as denoise from './denoise.js';

const roomId = decodeURIComponent(location.pathname.split('/').pop());
const params = new URLSearchParams(location.search);
const $ = (id) => document.getElementById(id);
// The console's own close code when the event was ended from the panel.
const EVENT_ENDED = 4001;
const REVOKED = 4003;
let languages = [];
let ready = false;
// Noise removal (DeepFilterNet3): where to load it from, and whether it is
// ready. Loaded in the background when the page opens.
let denoiseBase = '';
let denoiseAssets = null;
let dfNode = null;

// A speaker link carries its token in the URL fragment (#token=…), which
// the browser never sends to a server; the page takes it and removes it
// from the address bar. A speaker link is kept per event, apart from the
// organiser's admin token.
const SPEAKER = 'p1.';
const consoleKey = `dubash.console.${roomId}`;
function storedToken() {
  try { return localStorage.getItem(consoleKey) ?? localStorage.getItem('dubash.token') ?? ''; } catch { return ''; }
}
function storeToken(t) {
  try {
    localStorage.setItem(t.startsWith(SPEAKER) ? consoleKey : 'dubash.token', t);
    if (!t.startsWith(SPEAKER)) localStorage.removeItem(consoleKey);
  } catch { /* private mode */ }
}
const linked = new URLSearchParams(location.hash.slice(1)).get('token') ?? params.get('token');
if (linked) {
  storeToken(linked);
  params.delete('token');
  history.replaceState(null, '', location.pathname + (params.size ? `?${params}` : ''));
}
$('token').value = linked ?? storedToken();

let ctx, node, ws, stream, player, sentBytes = 0;

function setStatus(text, live = false) {
  $('status').textContent = text;
  $('status').classList.toggle('live', live);
  $('status').classList.toggle('is-live', live);
}
function fail(msg) {
  $('error').textContent = msg;
  stop();
}

async function detect() {
  try {
    const s = await navigator.mediaDevices.getUserMedia({ audio: true });
    s.getTracks().forEach((t) => t.stop());
    const devices = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'audioinput');
    const select = $('source');
    select.replaceChildren(new Option('Default microphone', 'default'), new Option('Play an audio file instead', 'file'),
      ...devices.map((d, i) => new Option(`Microphone: ${d.label || `input ${i + 1}`}`, d.deviceId)));
    select.value = devices[0]?.deviceId ?? 'default';
    toggleFileRow();
  } catch (e) {
    $('error').textContent = `Could not use the microphone: ${e.message}`;
  }
}

// denoiseStatus says how noise removal stands, under the microphone.
function denoiseStatus(text, warn = false) {
  const el = $('denoise-status');
  if (!el) return;
  el.textContent = text;
  el.classList.toggle('warn', warn);
}

// prepareDenoise loads DeepFilterNet3 in the background (about 11 MB the
// first time, then from the browser's cache).
async function prepareDenoise() {
  if (!denoiseBase) { denoiseStatus('Background-noise removal: the browser\'s built-in (DeepFilterNet3 is not set up on this server).'); return; }
  denoiseStatus('Background-noise removal (DeepFilterNet3): getting ready…');
  try {
    denoiseAssets = await denoise.load(denoiseBase);
    denoiseStatus('Background-noise removal (DeepFilterNet3): ready.');
  } catch (e) {
    denoiseAssets = null;
    denoiseStatus(`DeepFilterNet3 unavailable (${e.message}): the browser's built-in noise suppression is used instead.`, true);
  }
}

// cleanFeed: the console's "mixer or clean audio" switch (off by default).
function cleanFeed() {
  return $('clean-feed')?.checked ?? false;
}

function toggleFileRow() {
  const isFile = $('source').value === 'file';
  $('file-row').hidden = !isFile;
  $('player').hidden = !isFile || !$('player').src;
}

function loadFile(url) {
  // A fresh <audio> per source: createMediaElementSource works once per element.
  const fresh = document.createElement('audio');
  fresh.id = 'player';
  fresh.controls = true;
  fresh.src = url;
  $('player').replaceWith(fresh);
  toggleFileRow();
}

async function start() {
  // Disabled until stop()/fail(): a second click while this awaits the
  // worklet or the microphone would open a second socket and leave the
  // first one holding the room with no audio.
  if (!ready) return;
  setStatus('Connecting…');
  $('start').textContent = 'Connecting…';
  $('start').disabled = true;
  $('source').disabled = true;
  $('detect').disabled = true;
  $('file').disabled = true;
  $('spoken').disabled = true;
  $('talk').disabled = true;
  $('error').textContent = '';
  const token = $('token').value.trim();
  storeToken(token);
  const isFile = $('source').value === 'file';
  player = $('player');
  if (isFile && !player.src) return fail('Choose an audio file, or press "Find microphones" and pick a microphone from the list.');

  // A microphone goes through DeepFilterNet3 at 48 kHz unless the feed
  // is clean (a mixer) or the model could not load.
  const wantDF = !isFile && !cleanFeed() && $('denoise-level').value !== 'off' && denoiseAssets;
  try {
    ctx = new AudioContext({ sampleRate: wantDF ? 48000 : 16000 });
  } catch {
    ctx = new AudioContext(); // the worklet resamples
  }
  await ctx.audioWorklet.addModule('/static/js/pcm-worklet.js');
  dfNode = null;
  if (wantDF) {
    try {
      dfNode = await denoise.node(ctx, denoiseAssets, Number($('denoise-level').value));
      dfNode.port.onmessage = (e) => { if (e.data.slow) denoiseStatus('This computer is too slow for full noise removal: some audio is being skipped. Choose "Light" or a faster device.', true); };
    } catch (e) {
      denoiseStatus(`Noise removal could not start (${e.message}); using the browser's.`, true);
    }
  }
  node = new AudioWorkletNode(ctx, 'pcm-16k', { channelCount: 1, channelCountMode: 'explicit', channelInterpretation: 'speakers' });
  const mute = ctx.createGain();
  mute.gain.value = 0;
  node.connect(mute).connect(ctx.destination); // keeps the worklet pulled

  let src;
  if (isFile) {
    src = ctx.createMediaElementSource(player);
    src.connect(ctx.destination); // the operator hears the file
  } else {
    stream = await navigator.mediaDevices.getUserMedia({
      // A laptop or phone microphone in a hall needs the browser's noise
      // suppression, echo cancellation and level control: without them the
      // recognizer hears no pauses and commits nothing. A clean mixer feed
      // is better untouched ("Clean feed" in the console).
      // With DeepFilterNet3 running, the browser keeps only echo
      // cancellation and level control (two noise suppressors in a row
      // damage speech).
      audio: { ...($('source').value === 'default' ? {} : { deviceId: { exact: $('source').value } }), channelCount: 1, ...(cleanFeed() ? { echoCancellation: false, noiseSuppression: false, autoGainControl: false } : { echoCancellation: true, noiseSuppression: !dfNode, autoGainControl: true }) },
    });
    src = ctx.createMediaStreamSource(stream);
  }
  if (dfNode) src.connect(dfNode).connect(node);
  else src.connect(node);
  document.documentElement.dataset.denoise = dfNode ? 'deepfilternet3' : (isFile || cleanFeed() ? 'none' : 'browser');

  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  // A local reference so a socket superseded by stop() (still CONNECTING
  // when Detener is clicked) can tell its own handlers are stale, instead
  // of racing the module-level `ws` that stop() has already cleared.
  const query = new URLSearchParams({ lang: $('spoken').value });
  if ($('talk').value.trim()) query.set('talk', $('talk').value.trim());
  // Browsers cannot set headers on a WebSocket: the token travels as a
  // subprotocol, never in the URL. A token with characters a subprotocol
  // cannot hold falls back to the query.
  const url = `${proto}://${location.host}/ingest/${encodeURIComponent(roomId)}`;
  let sock;
  try {
    sock = new WebSocket(`${url}?${query}`, token ? ['dubash', `dubash-token.${token}`] : ['dubash']);
  } catch {
    query.set('token', token);
    sock = new WebSocket(`${url}?${query}`);
  }
  sock.binaryType = 'arraybuffer';
  sentBytes = 0;
  attach(sock, isFile, () => {
    // The WebSocket never opened: some hosts' gateways drop WebSockets
    // (Zoho Catalyst does). Stream the same audio over plain HTTP.
    query.delete('token');
    const link = new HttpLink(`/api/admin/rooms/${encodeURIComponent(roomId)}/stream`, query, token);
    attach(link, isFile, null);
  });
  if (isFile) player.onended = () => stop();
}

// attach makes link (a WebSocket, or an HttpLink with the same shape)
// the console's connection. fallback runs if a WebSocket never opens.
function attach(sock, isFile, fallback) {
  ws = sock;
  let opened = false;
  sock.onopen = () => {
    if (sock !== ws) return;
    opened = true;
    setStatus('Live · Captions on', true);
    $('start').textContent = 'You’re live';
    $('stop').disabled = false;
    $('pause').disabled = false;
    setPaused(false);
    if (isFile) player.play();
  };
  sock.onclose = (e) => {
    if (sock !== ws) return;
    if (!opened && fallback && e.code === 1006) return fallback();
    if (e.code === 1008) fail('Someone else is already live in this event.');
    else if (e.code === EVENT_ENDED) fail('The event was ended from the admin panel.');
    else if (e.code === REVOKED) fail('The organisers revoked this speaker link: ask them for a new one.');
    else if (e.code === 1006 && sentBytes === 0) fail('Could not connect: check the token, or whether the event is already live.');
    else if (e.code === 1006) fail('The connection to the server was lost.');
    else if (e.code !== 1000) fail(`Recognition failed: ${e.reason || `code ${e.code}`}`);
    else stop();
  };
  node.port.onmessage = (e) => {
    if (sock !== ws || sock.readyState !== WebSocket.OPEN) return;
    sock.send(e.data);
    sentBytes += e.data.byteLength;
    $('sent').textContent = `${(sentBytes / 32000).toFixed(0)} s sent`;
    const pcm = new Int16Array(e.data);
    let sum = 0;
    for (let i = 0; i < pcm.length; i++) sum += pcm[i] * pcm[i];
    const rms = Math.sqrt(sum / pcm.length) / 32768;
    $('level').style.clipPath = `inset(0 ${100 - Math.min(100, rms * 300)}% 0 0)`;
  };
}

// HttpLink streams audio over plain HTTP: a session, then about a quarter
// second of PCM per request, in order. It has the few parts of the
// WebSocket API the console uses (readyState, send, close, onopen,
// onclose with the same close codes).
class HttpLink {
  constructor(base, query, token) {
    this.readyState = 0;
    this.base = base;
    this.headers = token ? { 'X-Dubash-Token': token } : {};
    this.parts = [];
    this.size = 0;
    this.fails = 0;
    this.chain = Promise.resolve();
    this.begin(query);
  }
  async begin(query) {
    try {
      const r = await fetch(`${this.base}?${query}`, { method: 'POST', headers: this.headers });
      if (r.status === 409) return this.end(1008, 'room busy');
      if (r.status === 401 || r.status === 403) return this.end(1006, '');
      if (!r.ok) return this.end(4400, (await r.text()).trim() || `HTTP ${r.status}`);
      this.url = `${this.base}/${(await r.json()).session}`;
      if (this.readyState !== 0) return this.hangUp(); // stopped while connecting
      this.readyState = 1;
      this.timer = setInterval(() => this.flush(), 250);
      this.onopen?.();
    } catch {
      this.end(1006, '');
    }
  }
  send(data) {
    if (this.readyState !== 1) return;
    this.parts.push(new Uint8Array(data));
    this.size += data.byteLength;
  }
  flush() {
    if (!this.size) return;
    const body = new Uint8Array(this.size);
    let at = 0;
    for (const p of this.parts) { body.set(p, at); at += p.length; }
    this.parts = [];
    this.size = 0;
    this.chain = this.chain.then(() => this.post(body));
  }
  async post(body) {
    if (this.readyState === 3) return;
    try {
      const r = await fetch(this.url, { method: 'POST', headers: { ...this.headers, 'Content-Type': 'application/octet-stream' }, body });
      if (r.status === 410) {
        const { reason = '' } = await r.json().catch(() => ({}));
        const code = { ended: EVENT_ENDED, revoked: REVOKED, busy: 1008, stopped: 1000 }[reason] ?? 1011;
        return this.end(code, reason.replace(/^error: /, ''));
      }
      if (!r.ok) throw new Error(String(r.status));
      this.fails = 0;
    } catch {
      if (++this.fails >= 8) this.end(1006, 'connection lost');
    }
  }
  hangUp() {
    if (this.url) fetch(this.url, { method: 'DELETE', headers: this.headers }).catch(() => {});
  }
  close(code = 1000) {
    if (this.readyState >= 2) return;
    const wasOpen = this.readyState === 1;
    this.readyState = 2;
    clearInterval(this.timer);
    if (wasOpen) {
      this.flush();
      this.chain = this.chain.then(() => this.hangUp());
    }
    this.end(code, '');
  }
  end(code, reason) {
    if (this.readyState === 3) return;
    this.readyState = 3;
    clearInterval(this.timer);
    setTimeout(() => this.onclose?.({ code, reason }), 0); // asynchronous, like a WebSocket
  }
}

// Pause keeps the microphone connected but sends nothing to recognition:
// nothing said meanwhile is captioned, stored or billed.
let paused = false;
function setPaused(on) {
  paused = on;
  $('pause').textContent = on ? 'Resume' : 'Pause';
  $('pause').setAttribute('aria-pressed', String(on));
  if (ws) setStatus(on ? 'Paused · Audio off' : 'Live · Captions on', !on);
}

async function control(action) {
  const res = await fetch(`/api/admin/rooms/${encodeURIComponent(roomId)}/${action}`, {
    method: 'POST', headers: { 'X-Dubash-Token': $('token').value.trim() },
  });
  if (!res.ok) throw new Error(res.status === 401 ? 'The token was rejected (expired or revoked?).' : `${action} failed: ${res.status}`);
  setPaused((await res.json()).paused);
}

// audience shows how many people read each language, and how fast.
async function audience() {
  const token = $('token').value.trim();

  try {
    const res = await fetch('/api/admin/status', { headers: { 'X-Dubash-Token': token } });
    if (!res.ok) return;
    const st = (await res.json()).find((r) => r.id === roomId);
    if (!st) return;
    const per = Object.entries(st.listeners ?? {}).filter(([l]) => l).map(([l, n]) => `${NAMES[l] ?? l} ${n}`);
    const lag = Object.entries(st.translation ?? {}).map(([l, v]) => `${NAMES[l] ?? l} ${(v.p50 / 1000).toFixed(1)} s`);
    $('audience').textContent = `Readers: ${st.viewers}${per.length ? ` (${per.join(', ')})` : ''}.` +
      (lag.length ? ` Translation delay (median): ${lag.join(', ')}.` : '');
  } catch { /* the next poll retries */ }
}

function stop() {
  // CONNECTING or OPEN: close it. A CONNECTING socket left open would
  // still fire onopen later (stuck "EN VIVO" UI) and keep the room busy
  // server-side for the next operator.
  if (ws && ws.readyState < WebSocket.CLOSING) ws.close(1000);
  ws = null;
  stream?.getTracks().forEach((t) => t.stop());
  stream = null;
  player?.pause();
  ctx?.close().catch(() => {});
  ctx = null;
  $('level').style.clipPath = '';
  $('start').disabled = !ready;
  $('start').textContent = 'Start speaking';
  $('source').disabled = false;
  $('detect').disabled = !ready;
  $('file').disabled = false;
  $('spoken').disabled = false;
  $('talk').disabled = false;
  $('stop').disabled = true;
  $('pause').disabled = true;
  setPaused(false);
  setStatus('Not live');
  if ($('source').value === 'file' && $('player').src) loadFile($('player').src);
}

// The language spoken decides what the recognizer listens for; the
// event's other languages become the translations.
function setSpoken(lang) {
  if (!languages.includes(lang)) return;
  $('spoken').value = lang;
  showTranslations();
}

function showTranslations() {
  const others = languages.filter((l) => l !== $('spoken').value).map((l) => NAMES[l] ?? l);
  const spoken = NAMES[$('spoken').value] ?? $('spoken').value;
  $('spoken-hint').textContent = others.length
    ? `Recognized in ${spoken}, translated into ${others.join(', ')} while someone reads them.`
    : `Recognized in ${spoken}.`;
}

function preview(room) {
  const lines = $('lines');
  // The console reads the spoken language only: it never starts a
  // translation.
  const events = new EventSource(`/events/${encodeURIComponent(roomId)}`);
  events.addEventListener('final', (e) => {
    const seg = JSON.parse(e.data);
    if (lines.querySelector(`[data-id="${seg.id}"]`)) return; // replayed
    const p = document.createElement('p');
    p.dataset.id = seg.id;
    p.textContent = seg.text;
    p.title = 'Click to fix a word';
    p.tabIndex = 0;
    p.onclick = () => fixLine(p);
    p.onkeydown = (ev) => { if (ev.key === 'Enter') fixLine(p); };
    $('transcript-empty').hidden = true;
    lines.append(p);
    while (lines.children.length > 8) lines.firstChild.remove();
    $('interim').textContent = '';
  });
  // A fix from this or another console replaces the line.
  events.addEventListener('correction', (e) => {
    const seg = JSON.parse(e.data);
    const p = lines.querySelector(`[data-id="${seg.id}"]`);
    if (p && !p.querySelector('input')) p.textContent = seg.text;
  });
  events.addEventListener('interim', (e) => { $('transcript-empty').hidden = true; $('interim').textContent = JSON.parse(e.data).text; });
  window.addEventListener('pagehide', () => { events.close(); stop(); }, { once: true });
  const main = room.targets[0] ?? room.source;
  const outputs = [
    ['Audience page', 'For phones', `/r/${roomId}`],
    ['Stage screen', 'For the projector, with the QR code', `/r/${roomId}?lang=${main}&mode=screen`],
    ['OBS or vMix overlay', 'Browser Source with a transparent background', `/r/${roomId}?lang=${main}&mode=overlay`],
    ['QR poster', 'A4, to print', `/posters?room=${roomId}`],
  ];
  $('links').replaceChildren(...outputs.map(([label, note, href]) => {
    const li = document.createElement('li');
    const a = document.createElement('a');
    a.href = href;
    a.target = '_blank';
    a.textContent = label;
    const small = document.createElement('small');
    small.textContent = note;
    li.append(a, small);
    return li;
  }));
  $('cli').textContent = `dubash-ingest -server ${location.origin} \\\n  -room ${roomId} -lang ${room.source} -i srt://mixer:9000`;
}

// fixLine edits a committed line in place: Enter saves, Escape cancels.
// Readers get the fixed line at once, translated again; the replaced words
// are suggested to the Dictation Bank for approval.
function fixLine(p) {
  if (p.querySelector('input')) return;
  const old = p.textContent;
  const input = document.createElement('input');
  input.type = 'text';
  input.value = old;
  input.className = 'fix';
  input.setAttribute('aria-label', 'Fixed line');
  p.replaceChildren(input);
  input.focus();
  const done = (text) => { p.textContent = text; };
  input.onkeydown = async (ev) => {
    if (ev.key === 'Escape') return done(old);
    if (ev.key !== 'Enter') return;
    ev.preventDefault();
    const text = input.value.trim();
    if (!text || text === old) return done(old);
    input.disabled = true;
    try {
      const res = await fetch(`/api/admin/rooms/${encodeURIComponent(roomId)}/segments/${p.dataset.id}/correct`, {
        method: 'POST',
        headers: { 'X-Dubash-Token': $('token').value.trim() },
        body: new URLSearchParams({ text }),
      });
      if (!res.ok) throw new Error(res.status === 404 ? 'That line is too old to fix here.' : (await res.text()).trim());
      const out = await res.json();
      done(text);
      $('error').textContent = out.warning ?? '';
      $('fixes').textContent = out.suggested.length
        ? `Fixed. ${out.suggested.length} word${out.suggested.length > 1 ? 's' : ''} suggested to the Dictation Bank.`
        : 'Fixed.';
    } catch (e) {
      done(old);
      $('error').textContent = `Could not fix the line: ${e.message}`;
    }
  };
  input.onblur = () => { if (!input.disabled) done(old); };
}

// Capture needs AudioWorklet (Chromium) and a secure context (https or
// http://localhost); over plain http on a LAN IP the browser hides both
// the worklet and the microphone API.
function unsupported() {
  if (!window.isSecureContext) {
    return 'This console needs a secure connection: open it over https (your tunnel) or at http://localhost, not at the network IP address.';
  }
  if (!window.AudioWorkletNode || !navigator.mediaDevices) {
    return 'This browser cannot capture audio: use an up-to-date Chrome or Edge.';
  }
  return '';
}

async function main() {
  const why = unsupported();
  if (why) {
    $('error').textContent = why;
    $('start').disabled = true;
    $('detect').disabled = true;
  }
  const rooms = await (await fetch('/api/rooms')).json();
  const room = rooms.find((r) => r.id === roomId);
  if (!room) {
    $('error').textContent = 'Event not found.';
    $('start').disabled = true;
    return;
  }
  document.title = `Console · ${room.title}`;
  $('title').textContent = room.title;
  languages = [room.source, ...room.targets];
  $('spoken').replaceChildren(...languages.map((l) => new Option(NAMES[l] ?? l, l)));
  $('spoken').value = room.source;
  $('spoken').onchange = showTranslations;
  showTranslations();
  for (const a of document.querySelectorAll('[data-sample]')) a.closest('span').hidden = !languages.includes(a.dataset.sample);
  let authorized = false;
  try {
    const response = await fetch(`/api/admin/rooms/${encodeURIComponent(roomId)}/links`, { headers: { 'X-Dubash-Token': $('token').value.trim() } });
    if (response.ok) {
      authorized = true;
      const links = await response.json();
      $('audience-url').value = links.audience;
      $('copy-audience').disabled = false;
      $('audience-open').href = links.audience;
    } else {
      $('audience-url').value = `${location.origin}/r/${roomId}`;
      $('audience-open').href = `/r/${roomId}`;
      if (response.status === 401 || response.status === 403) {
        $('error').textContent = 'Enter your speaker access key below, or open this console from your admin studio.';
        $('token').closest('details').open = true;
      }
    }
  } catch { $('error').textContent = 'Could not load the event links. Check your connection.'; }
  if ($('token').value.startsWith(SPEAKER)) { $('admin-back').hidden = true; $('back-events').href = '/'; $('back-events').textContent = '← Audience home'; }
  ready = !why && authorized;
  $('start').disabled = !ready;
  preview(room);
  $('detect').onclick = detect;
  $('source').onchange = toggleFileRow;
  $('file').onchange = (e) => e.target.files[0] && loadFile(URL.createObjectURL(e.target.files[0]));
  for (const a of document.querySelectorAll('[data-sample]')) {
    a.onclick = (e) => {
      e.preventDefault();
      if (!ws) setSpoken(a.dataset.sample); // the sample says which language it is
      loadFile(`/samples/${a.dataset.sample}.wav`);
    };
  }
  fetch('/api/site').then((r) => r.json()).then((site) => { denoiseBase = site.denoise ?? ''; prepareDenoise(); }).catch(() => prepareDenoise());
  $('start').onclick = () => start().catch((e) => fail(e.message));
  $('stop').onclick = stop;
  $('pause').onclick = () => control(paused ? 'resume' : 'pause').catch((e) => { $('error').textContent = e.message; });
  setInterval(audience, 3000);
  toggleFileRow();
}

$('copy-audience').onclick = async () => {
  try { await navigator.clipboard.writeText($('audience-url').value); }
  catch { $('audience-url').focus(); $('audience-url').select(); if (!document.execCommand('copy')) { $('toast').textContent = 'Link selected. Use your device’s copy command.'; $('toast').hidden = false; return; } }
  $('toast').textContent = 'Audience link copied. Ready to share.'; $('toast').hidden = false;
  setTimeout(() => { $('toast').hidden = true; }, 4000);
};
$('token').onchange = () => {
  const token = $('token').value.trim();
  const changed = token !== storedToken();
  storeToken(token);
  if (changed && !ws) location.reload();
};
main().catch((e) => { $('error').textContent = `Could not load your event: ${e.message}`; });
