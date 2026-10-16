// Modified by the Dubash contributors: English; pause, resume and end;
// readers and delays per language.
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
  return s < 60 ? `${s} s ago` : s < 3600 ? `${Math.round(s / 60)} min ago` : `${Math.round(s / 3600)} h ago`;
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
    textContent: quiet ? 'Not arriving' : s.audioLevel < -50 ? 'Silent' : `${Math.round(s.audioLevel)} dB`,
  });
  return el('td', { className: 'num' }, level, el('span', { className: 'sub', textContent: `${Math.round(s.audioSeconds)} s received` }));
}

// Text: when the recognizer last produced anything. Audio with voice but
// no text for a while means transcription is stuck.
function textCell(s) {
  const ago_ = since(s.lastTextAt);
  const stuck = s.live && s.audioLevel >= -50 && ago_ > 15;
  return el('td', { className: 'num' },
    el('span', { className: stuck ? 'lat-bad' : '', textContent: ago(s.lastTextAt) }),
    el('span', { className: 'sub', textContent: s.segments ? `last line ${ago(s.lastSegmentAt)}` : '' }));
}

async function newTalk(s) {
  const title = prompt(`New session in ${s.title}. Title (optional):`, '');
  if (title === null) return;
  const res = await fetch(`/api/admin/rooms/${s.id}/talks`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${$('token').value}` },
    body: new URLSearchParams({ title }),
  });
  if (!res.ok) $('error').textContent = `Could not start the session: ${await res.text()}`;
  refresh();
}

// control pauses, resumes or ends an event.
async function control(s, action) {
  if (action === 'end' && !confirm(`End ${s.title}? The speaker's console stops and readers see that the event is over.`)) return;
  const res = await fetch(`/api/admin/rooms/${s.id}/${action}`, {
    method: 'POST', headers: { Authorization: `Bearer ${$('token').value}` },
  });
  if (!res.ok) $('error').textContent = `${action} failed: ${await res.text()}`;
  refresh();
}

// Readers per language, and how long each language's translations take.
function readersCell(s) {
  const per = Object.entries(s.listeners ?? {}).filter(([l]) => l).map(([l, n]) => `${l.toUpperCase()} ${n}`);
  return el('td', { className: 'num' }, String(s.viewers), el('span', { className: 'sub', textContent: per.join(' · ') }));
}

function delayCell(s) {
  const tr = Object.entries(s.translation ?? {});
  if (!tr.length) return el('td', { className: 'num', textContent: '—' });
  const worst = Math.max(...tr.map(([, v]) => v.p95));
  return el('td', { className: 'num' },
    el('span', { className: latencyClass(worst), textContent: `${(worst / 1000).toFixed(1)} s p95` }),
    ...tr.map(([l, v]) => el('span', { className: 'sub', textContent: `${l.toUpperCase()} ${(v.p50 / 1000).toFixed(1)} / ${(v.p95 / 1000).toFixed(1)} s` })),
    el('span', { className: 'sub', textContent: s.commitLag?.n ? `commit ${(s.commitLag.p50 / 1000).toFixed(1)} s` : '' }));
}

function row(s) {
  const label = s.ended ? 'Ended' : s.paused ? 'Paused' : s.live ? 'Live' : 'Idle';
  const state = el('span', { className: s.live && !s.paused ? 'status live' : 'status', textContent: label });
  const errors = el('td', { className: 'num' }, String(s.errors));
  if (s.lastError) errors.append(el('span', { className: 'last-error', textContent: s.lastError }));
  const token = encodeURIComponent($('token').value);
  const links = [link('Console', `/operator/${s.id}?token=${token}`), ' · ', link('View', `/r/${s.id}`),
    ' · ', link('Screen', `/r/${s.id}?mode=screen`)];
  const talkQuery = s.talk ? `&talk=${s.talk}` : '';
  for (const l of [s.source, ...s.targets]) links.push(' · ', link(`SRT ${l.toUpperCase()}`, `/export/${s.id}/srt?lang=${l}${talkQuery}`));
  const button = el('button', { type: 'button', className: 'button secondary small', textContent: 'New session' });
  button.onclick = () => newTalk(s).catch((e) => { $('error').textContent = e.message; });
  const actions = [button];
  if (s.live) {
    const pause = el('button', { type: 'button', className: 'button secondary small', textContent: s.paused ? 'Resume' : 'Pause' });
    pause.onclick = () => control(s, s.paused ? 'resume' : 'pause').catch((e) => { $('error').textContent = e.message; });
    actions.push(' ', pause);
  }
  const end = el('button', { type: 'button', className: 'button secondary small', textContent: 'End event' });
  end.onclick = () => control(s, 'end').catch((e) => { $('error').textContent = e.message; });
  actions.push(' ', end);
  const talk = s.talk ? `Session ${s.talk}${s.talkTitle ? `: ${s.talkTitle}` : ''}` : 'No sessions yet';
  return el('tr', {},
    el('th', { scope: 'row' }, s.title,
      el('span', { className: 'sub', textContent: `${s.id} · ${s.source.toUpperCase()} → ${s.targets.map((t) => t.toUpperCase()).join(', ') || '—'}` }),
      el('span', { className: 'sub', textContent: talk })),
    el('td', {}, state),
    audioCell(s),
    textCell(s),
    readersCell(s),
    el('td', { className: 'num', textContent: String(s.segments) }),
    delayCell(s),
    errors,
    el('td', { className: 'links' }, ...links, el('div', {}, ...actions)));
}

function summary(rooms) {
  const sum = (f) => rooms.reduce((n, r) => n + f(r), 0);
  const segs = sum((r) => r.segments);
  const errs = sum((r) => r.errors);
  const live = rooms.filter((r) => r.live).length;
  const set = (id, v) => { $(id).querySelector('b').textContent = v; };
  set('sum-live', `${live} of ${rooms.length}`);
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
      $('error').textContent = 'Invalid token.';
      return;
    }
    const rooms = await res.json();
    $('error').textContent = '';
    $('metrics').href = `/metrics?token=${encodeURIComponent($('token').value)}`;
    summary(rooms);
    $('rows').replaceChildren(...rooms.map(row));
    $('updated').textContent = `Updated at ${new Date().toLocaleTimeString()}`;
  } catch (e) {
    $('error').textContent = `No connection: ${e.message}`;
  }
}

refresh();
setInterval(refresh, 2000);
