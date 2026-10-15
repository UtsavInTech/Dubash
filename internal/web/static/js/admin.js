const $ = (id) => document.getElementById(id);
const params = new URLSearchParams(location.search);

function storedToken() {
  try { return localStorage.getItem('dubash.token') ?? ''; } catch { return ''; }
}
$('token').value = params.get('token') ?? storedToken();
$('token').onchange = () => { try { localStorage.setItem('dubash.token', $('token').value); } catch { /* private mode */ } refresh(); };

function ago(iso) {
  if (!iso || iso.startsWith('0001')) return '—';
  const s = Math.round((Date.now() - new Date(iso)) / 1000);
  return s < 60 ? `hace ${s} s` : s < 3600 ? `hace ${Math.round(s / 60)} min` : `hace ${Math.round(s / 3600)} h`;
}

// Translation latency budget: under 1.5 s reads as live, over 3 s lags.
function latencyClass(ms) {
  return ms < 1500 ? 'lat-ok' : ms < 3000 ? 'lat-warn' : 'lat-bad';
}

function el(tag, props = {}, ...children) {
  const node = Object.assign(document.createElement(tag), props);
  node.append(...children);
  return node;
}

function link(label, href) {
  return el('a', { href, target: '_blank', textContent: label });
}

// Seconds since an ISO time, or Infinity if never.
function since(iso) {
  return !iso || iso.startsWith('0001') ? Infinity : (Date.now() - new Date(iso)) / 1000;
}

// Audio: what reaches the server. A live room with no audio, or only
// silence, is the most common failure at a venue (muted channel, wrong input).
function audioCell(s) {
  if (!s.live) return el('td', { className: 'num', textContent: '—' });
  const quiet = since(s.lastAudioAt) > 5;
  const level = el('span', {
    className: quiet ? 'lat-bad' : s.audioLevel < -50 ? 'lat-warn' : 'lat-ok',
    textContent: quiet ? 'No llega' : s.audioLevel < -50 ? 'Silencio' : `${Math.round(s.audioLevel)} dB`,
  });
  return el('td', { className: 'num' }, level, el('span', { className: 'sub', textContent: `${Math.round(s.audioSeconds)} s recibidos` }));
}

// Text: when the recognizer last produced anything. Audio with voice but
// no text for a while means transcription is stuck.
function textCell(s) {
  const ago_ = since(s.lastTextAt);
  const stuck = s.live && s.audioLevel >= -50 && ago_ > 15;
  return el('td', { className: 'num' },
    el('span', { className: stuck ? 'lat-bad' : '', textContent: ago(s.lastTextAt) }),
    el('span', { className: 'sub', textContent: s.segments ? `última línea ${ago(s.lastSegmentAt)}` : '' }));
}

async function newTalk(s) {
  const title = prompt(`Nueva charla en ${s.title}. Título (opcional):`, '');
  if (title === null) return;
  const res = await fetch(`/api/admin/rooms/${s.id}/talks`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${$('token').value}` },
    body: new URLSearchParams({ title }),
  });
  if (!res.ok) $('error').textContent = `No se pudo empezar la charla: ${await res.text()}`;
  refresh();
}

function row(s) {
  const state = el('span', { className: s.live ? 'status live' : 'status', textContent: s.live ? 'En vivo' : 'Inactiva' });
  const errors = el('td', { className: 'num' }, String(s.errors));
  if (s.lastError) errors.append(el('span', { className: 'last-error', textContent: s.lastError }));
  const latency = s.segments
    ? el('span', { className: latencyClass(s.avgLatencyMs), textContent: `${(s.avgLatencyMs / 1000).toFixed(1)} s` })
    : '—';
  const token = encodeURIComponent($('token').value);
  const links = [link('Operar', `/operator/${s.id}?token=${token}`), ' · ', link('Ver', `/r/${s.id}`),
    ' · ', link('Pantalla', `/r/${s.id}?mode=screen`)];
  const talkQuery = s.talk ? `&talk=${s.talk}` : '';
  for (const l of [s.source, ...s.targets]) links.push(' · ', link(`SRT ${l.toUpperCase()}`, `/export/${s.id}/srt?lang=${l}${talkQuery}`));
  const button = el('button', { type: 'button', className: 'button secondary small', textContent: 'Nueva charla' });
  button.onclick = () => newTalk(s).catch((e) => { $('error').textContent = e.message; });
  const talk = s.talk ? `Charla ${s.talk}${s.talkTitle ? `: ${s.talkTitle}` : ''}` : 'Sin charlas todavía';
  return el('tr', {},
    el('th', { scope: 'row' }, s.title,
      el('span', { className: 'sub', textContent: `${s.id} · ${s.source.toUpperCase()} → ${s.targets.map((t) => t.toUpperCase()).join(', ') || '—'}` }),
      el('span', { className: 'sub', textContent: talk })),
    el('td', {}, state),
    audioCell(s),
    textCell(s),
    el('td', { className: 'num', textContent: String(s.viewers) }),
    el('td', { className: 'num', textContent: String(s.segments) }),
    el('td', { className: 'num' }, latency),
    errors,
    el('td', { className: 'links' }, ...links, el('div', {}, button)));
}

function summary(rooms) {
  const sum = (f) => rooms.reduce((n, r) => n + f(r), 0);
  const segs = sum((r) => r.segments);
  const errs = sum((r) => r.errors);
  const live = rooms.filter((r) => r.live).length;
  const set = (id, v) => { $(id).querySelector('b').textContent = v; };
  set('sum-live', `${live} de ${rooms.length}`);
  set('sum-viewers', sum((r) => r.viewers));
  set('sum-segments', segs);
  set('sum-latency', segs ? `${(sum((r) => r.avgLatencyMs * r.segments) / segs / 1000).toFixed(1)} s` : '—');
  set('sum-errors', errs);
  $('sum-live').classList.toggle('good', live > 0);
  $('sum-errors').classList.toggle('alert', errs > 0);
}

async function refresh() {
  try {
    const res = await fetch('/api/admin/status', { headers: { Authorization: `Bearer ${$('token').value}` } });
    if (res.status === 401) {
      $('error').textContent = 'Token inválido.';
      return;
    }
    const rooms = await res.json();
    $('error').textContent = '';
    $('metrics').href = `/metrics?token=${encodeURIComponent($('token').value)}`;
    summary(rooms);
    $('rows').replaceChildren(...rooms.map(row));
    $('updated').textContent = `Actualizado a las ${new Date().toLocaleTimeString()}`;
  } catch (e) {
    $('error').textContent = `Sin conexión: ${e.message}`;
  }
}

refresh();
setInterval(refresh, 2000);
