// Modified by the Dubash contributors: English, pause, end of event,
// listeners per language.
import { NAMES } from './langs.js';

const roomId = decodeURIComponent(location.pathname.split('/').pop());
const params = new URLSearchParams(location.search);
const $ = (id) => document.getElementById(id);
// The console's own close code when the event was ended from the panel.
const EVENT_ENDED = 4001;
let languages = [];

function storedToken() {
  try { return localStorage.getItem('dubash.token') ?? ''; } catch { return ''; }
}
function storeToken(t) {
  try { localStorage.setItem('dubash.token', t); } catch { /* private mode */ }
}
$('token').value = params.get('token') ?? storedToken();

let ctx, node, ws, stream, player, sentBytes = 0;

function setStatus(text, live = false) {
  $('status').textContent = text;
  $('status').classList.toggle('live', live);
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
    select.replaceChildren(new Option('Audio file', 'file'),
      ...devices.map((d, i) => new Option(`Microphone: ${d.label || `input ${i + 1}`}`, d.deviceId)));
    select.value = devices[0]?.deviceId ?? 'file';
    toggleFileRow();
  } catch (e) {
    $('error').textContent = `Could not use the microphone: ${e.message}`;
  }
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
  $('start').disabled = true;
  $('spoken').disabled = true;
  $('talk').disabled = true;
  $('error').textContent = '';
  const token = $('token').value.trim();
  storeToken(token);
  const isFile = $('source').value === 'file';
  player = $('player');
  if (isFile && !player.src) return fail('Choose an audio file, or press "Find microphones" and pick a microphone from the list.');

  try {
    ctx = new AudioContext({ sampleRate: 16000 });
  } catch {
    ctx = new AudioContext(); // the worklet resamples
  }
  await ctx.audioWorklet.addModule('/static/js/pcm-worklet.js');
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
      audio: { deviceId: { exact: $('source').value }, channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false },
    });
    src = ctx.createMediaStreamSource(stream);
  }
  src.connect(node);

  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  // A local reference so a socket superseded by stop() (still CONNECTING
  // when Detener is clicked) can tell its own handlers are stale, instead
  // of racing the module-level `ws` that stop() has already cleared.
  const query = new URLSearchParams({ token, lang: $('spoken').value });
  if ($('talk').value.trim()) query.set('talk', $('talk').value.trim());
  const sock = new WebSocket(`${proto}://${location.host}/ingest/${encodeURIComponent(roomId)}?${query}`);
  ws = sock;
  sock.binaryType = 'arraybuffer';
  sock.onopen = () => {
    if (sock !== ws) return;
    setStatus('Live', true);
    $('stop').disabled = false;
    $('pause').disabled = false;
    setPaused(false);
    if (isFile) player.play();
  };
  sock.onclose = (e) => {
    if (sock !== ws) return;
    if (e.code === 1008) fail('Someone else is already live in this event.');
    else if (e.code === EVENT_ENDED) fail('The event was ended from the admin panel.');
    else if (e.code === 1006 && sentBytes === 0) fail('Could not connect: check the token, or whether the event is already live.');
    else if (e.code === 1006) fail('The connection to the server was lost.');
    else if (e.code !== 1000) fail(`Recognition failed: ${e.reason || `code ${e.code}`}`);
    else stop();
  };
  sentBytes = 0;
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
  if (isFile) player.onended = () => stop();
}

// Pause keeps the microphone connected but sends nothing to recognition:
// nothing said meanwhile is captioned, stored or billed.
let paused = false;
function setPaused(on) {
  paused = on;
  $('pause').textContent = on ? 'Resume' : 'Pause';
  $('pause').setAttribute('aria-pressed', String(on));
  if (ws) setStatus(on ? 'Paused' : 'Live', !on);
}

async function control(action) {
  const res = await fetch(`/api/admin/rooms/${encodeURIComponent(roomId)}/${action}`, {
    method: 'POST', headers: { Authorization: `Bearer ${$('token').value.trim()}` },
  });
  if (!res.ok) throw new Error(res.status === 401 ? 'The admin token was rejected.' : `${action} failed: ${res.status}`);
  setPaused((await res.json()).paused);
}

// audience shows how many people read each language, and how fast.
async function audience() {
  const token = $('token').value.trim();
  if (!token) return;
  try {
    const res = await fetch('/api/admin/status', { headers: { Authorization: `Bearer ${token}` } });
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
  $('start').disabled = false;
  $('spoken').disabled = false;
  $('talk').disabled = false;
  $('stop').disabled = true;
  $('pause').disabled = true;
  setPaused(false);
  setStatus('Stopped');
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
    const p = document.createElement('p');
    p.textContent = seg.text;
    lines.append(p);
    while (lines.children.length > 6) lines.firstChild.remove();
    $('interim').textContent = '';
  });
  events.addEventListener('interim', (e) => { $('interim').textContent = JSON.parse(e.data).text; });
  const main = room.targets[0] ?? room.source;
  const outputs = [
    ['Audience page', 'For phones', `/r/${roomId}?lang=${main}`],
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
  $('start').onclick = () => start().catch((e) => fail(e.message));
  $('stop').onclick = stop;
  $('pause').onclick = () => control(paused ? 'resume' : 'pause').catch((e) => { $('error').textContent = e.message; });
  setInterval(audience, 3000);
  toggleFileRow();
}

main();
