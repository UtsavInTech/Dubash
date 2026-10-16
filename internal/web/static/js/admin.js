import { NAMES, el } from './langs.js';

const $ = (id) => document.getElementById(id);
let rooms = [], selected = '', connected = false, creating = false, publicBase = location.origin;
let linkRequest, qrURL, toastTimer, refreshing = false, listSignature = '';
try { $('token').value = localStorage.getItem('dubash.token') ?? ''; selected = sessionStorage.getItem('dubash.selected') ?? ''; } catch { /* Storage is optional. */ }
const legacy = new URLSearchParams(location.search).get('token');
if (legacy) { $('token').value = legacy; history.replaceState(null, '', location.pathname); }
const auth = () => ({ 'X-Dubash-Token': $('token').value.trim() });
const current = () => rooms.find((r) => r.id === selected);

function notify(text) {
  $('toast').textContent = text; $('toast').hidden = false;
  clearTimeout(toastTimer); toastTimer = setTimeout(() => { $('toast').hidden = true; }, 4000);
}
async function request(path, options = {}) {
  const res = await fetch(path, { ...options, headers: { ...auth(), ...options.headers } });
  if (!res.ok) throw new Error((await res.text()).trim() || `Request failed (${res.status}).`);
  return res;
}
async function copy(input, message) {
  try { await navigator.clipboard.writeText(input.value); notify(message); }
  catch { input.focus(); input.select(); if (document.execCommand('copy')) notify(message); else notify('Link selected. Copy it with your device’s copy command.'); }
}
function status(s) { return s.ended ? 'Ended' : s.paused ? 'Paused' : s.live ? 'Live now' : 'Ready to start'; }
function setState(node, s) { node.textContent = status(s); node.className = `event-state${s.live && !s.paused ? ' is-live' : ''}${s.ended ? ' is-ended' : ''}`; }
function saveToken() { try { localStorage.setItem('dubash.token', $('token').value.trim()); } catch { /* Private mode. */ } }
function showLogin() {
  connected = false; location.replace('/login?next=/admin');
}
$('account').onclick = () => { location.href = '/login'; };
$('sign-out').onclick = async () => { await fetch('/api/session', { method: 'DELETE' }); try { localStorage.removeItem('dubash.token'); } catch {} location.href = '/login'; };
$('login-form').onsubmit = async (e) => { e.preventDefault(); await refresh(); };
$('copy-audience').onclick = () => copy($('audience-url'), 'Audience link copied. Ready to share.');
$('speaker-copy').onclick = () => copy($('speaker-url'), 'Private speaker link copied. Send only to your speaker.');
$('speaker-hide').onclick = () => { $('speaker').hidden = true; $('speaker-url').value = ''; };
$('event-menu-toggle').onclick = () => { const open = $('event-actions').hidden; $('event-actions').hidden = !open; $('event-menu-toggle').setAttribute('aria-expanded', String(open)); };

function renderList() {
  const signature = JSON.stringify(rooms.map((r) => [r.id, r.title, status(r), r.source, r.targets]));
  if (signature !== listSignature) {
    const focused = document.activeElement?.dataset.event;
    $('event-list').replaceChildren(...rooms.map((r) => {
      const badge = el('span'); setState(badge, r);
      const button = el('button', { type: 'button', className: 'event-item' }, badge, el('strong', { textContent: r.title }), el('small', { textContent: `${NAMES[r.source] ?? r.source} + ${r.targets.length} translation${r.targets.length === 1 ? '' : 's'}` }));
      button.dataset.event = r.id; button.onclick = () => selectEvent(r.id);
      return button;
    }));
    listSignature = signature;
    if (focused) [...$('event-list').children].find((n) => n.dataset.event === focused)?.focus();
  }
  for (const button of $('event-list').children) button.setAttribute('aria-pressed', String(button.dataset.event === selected));
}
function selectEvent(id) {
  const changed = id !== selected || $('event-workspace').hidden;
  selected = id;
  try { sessionStorage.setItem('dubash.selected', id); } catch { /* Optional. */ }
  renderList(); renderEvent();
  if (changed) {
    $('speaker').hidden = true; $('speaker-url').value = ''; $('event-actions').hidden = true;
    $('event-menu-toggle').setAttribute('aria-expanded', 'false');
    loadLinks(id);
  }
}
function renderEvent() {
  const s = current();
  $('no-events').hidden = rooms.length > 0;
  $('event-workspace').hidden = !s;
  if (!s) return;
  $('event-title').textContent = s.title; setState($('event-state'), s);
  $('event-meta').textContent = [s.speaker && `With ${s.speaker}`, s.talkTitle && s.talkTitle, `Spoken in ${NAMES[s.source] ?? s.source}`].filter(Boolean).join(' · ');
  $('event-languages').replaceChildren(...[s.source, ...s.targets].map((l) => el('span', { textContent: NAMES[l] ?? l, lang: l })));
  $('stat-viewers').textContent = s.viewers; $('stat-lines').textContent = s.segments;
  $('stat-delay').textContent = s.avgLatencyMs ? `${(s.avgLatencyMs / 1000).toFixed(1)}s` : '—';
  $('speak-heading').textContent = s.ended ? 'This event has ended.' : s.live ? 'Your event is on air.' : 'Ready when you are.';
  $('open-console').textContent = s.ended ? 'Open speaker console →' : s.live ? 'Open live console →' : 'Start speaking →';
  $('end-event').disabled = s.ended;
  $('delete-transcript').disabled = s.live;
  $('new-talk').disabled = s.live;
  $('event-health').textContent = s.errors ? `${s.errors} pipeline error${s.errors === 1 ? '' : 's'}. ${s.lastError ?? ''}` : 'No pipeline errors reported.';
  $('event-cost').textContent = s.cost?.total ? `Estimated cost so far: ₹${s.cost.total.toFixed(2)}. Backup provider charges may be additional.` : '';
}
async function loadLinks(id) {
  linkRequest?.abort(); linkRequest = new AbortController();
  $('open-console').href = `/operator/${id}`;
  $('audience-url').value = `${publicBase}/r/${id}`;
  $('pin-note').hidden = true;
  $('local-warning').hidden = true;
  $('copy-audience').disabled = true;
  $('download-qr').disabled = true;
  $('share-qr').disabled = true;
  if (qrURL) { URL.revokeObjectURL(qrURL); qrURL = null; }
  $('event-qr').removeAttribute('src');
  const room = current();
  try {
    const signal = linkRequest.signal;
    const links = await (await request(`/api/admin/rooms/${id}/links`, { signal })).json();
    if (selected !== id) return;
    $('audience-url').value = links.audience;
    $('open-console').href = `/operator/${id}`;
    $('audience-preview').href = links.audience;
    $('poster-link').href = `/posters?room=${id}`;
    $('screen-link').href = `/r/${id}?mode=screen`;
    $('overlay-link').href = `/r/${id}?mode=overlay`;
    $('copy-audience').disabled = false;
    const hostname = new URL(links.audience).hostname;
    $('local-warning').hidden = !['localhost', '127.0.0.1', '::1', '[::1]'].includes(hostname);
    $('export-links').replaceChildren(...[room.source, ...room.targets].map((l) => {
      const button = el('button', { type: 'button', className: 'text-button', textContent: `Download SRT · ${NAMES[l] ?? l}` });
      button.onclick = () => download(`/export/${id}/srt?lang=${l}`).catch(showError);
      return button;
    }));
    const qr = await request(`/qr/${id}`, { signal });
    const blob = await qr.blob(); if (selected !== id) return;
    qrURL = URL.createObjectURL(blob); $('event-qr').src = qrURL;
    $('download-qr').disabled = false;
    $('share-qr').disabled = false;
    const access = await (await fetch(`/api/rooms/${id}/access`, { signal, headers: auth() })).json();
    if (selected !== id) return;
    $('pin-note').hidden = !access.pin;
    $('pin-note').textContent = 'This event has a PIN. Send the PIN separately to people opening the link. The QR code admits anyone who scans it.';
  } catch (e) { if (e.name !== 'AbortError') showError(e); }
}
function showError(e) { $('error').textContent = e.message; }
async function download(path, open = false) {
  const res = await request(path); const url = URL.createObjectURL(await res.blob());
  if (open) window.open(url, '_blank', 'noopener');
  else { const a = el('a', { href: url, download: (res.headers.get('Content-Disposition') ?? '').match(/filename="([^"]+)"/)?.[1] ?? 'transcript.srt' }); document.body.append(a); a.click(); a.remove(); }
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}
// PNG works in messaging apps and phone galleries; SVG is kept for crisp on-page display.
async function qrFile(id) {
  const response = await request(`/qr/${id}?format=png&download=1`);
  return new File([await response.blob()], `${id}-qr.png`, { type: 'image/png' });
}
function saveQR(file) {
  const url = URL.createObjectURL(file);
  const a = el('a', { href: url, download: file.name });
  document.body.append(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}
$('download-qr').onclick = async () => {
  const id = selected; $('download-qr').disabled = true;
  try { saveQR(await qrFile(id)); notify('QR image downloaded. Attach it in WhatsApp or your invitation.'); }
  catch (e) { showError(e); }
  finally { if (selected === id) $('download-qr').disabled = false; }
};
$('share-qr').onclick = async () => {
  const s = current(); if (!s) return;
  const audienceURL = $('audience-url').value;
  $('share-qr').disabled = true;
  try {
    const file = await qrFile(s.id);
    if (navigator.share && navigator.canShare?.({ files: [file] })) {
      await navigator.share({ title: s.title, text: `Join ${s.title}. Scan the QR code or open ${audienceURL} and choose your language.`, files: [file] });
      notify('QR shared.');
    } else {
      saveQR(file); notify('QR downloaded. Attach the image in WhatsApp or another messaging app.');
    }
  } catch (e) { if (e.name !== 'AbortError') showError(e); }
  finally { if (selected === s.id) $('share-qr').disabled = false; }
};
$('metrics').onclick = () => download('/metrics', true).catch(showError);
$('make-speaker').onclick = async () => {
  const s = current(); if (!s) return;
  $('make-speaker').disabled = true;
  try {
    const out = await (await request(`/api/admin/rooms/${s.id}/publisher-tokens`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ hours: 12 }) })).json();
    if (selected !== s.id) return;
    $('speaker-url').value = out.console;
    $('speaker-title').textContent = `Only share this with your speaker. It opens this event’s microphone console and expires ${new Date(out.expires).toLocaleString()}.`;
    $('speaker').hidden = false; $('speaker-url').focus();
  } catch (e) { showError(e); } finally { $('make-speaker').disabled = false; }
};
async function action(kind) {
  const s = current(); if (!s) return;
  let path = `/api/admin/rooms/${s.id}/`, method = 'POST', body;
  if (kind === 'end' && !confirm(`End ${s.title}? This stops the speaker and tells your audience the event is over.`)) return;
  if (kind === 'publisher-tokens/revoke' && !confirm(`Revoke all private speaker links for ${s.title}?`)) return;
  if (kind === 'transcript') { if (!confirm(`Permanently delete the transcript for ${s.title}?`)) return; method = 'DELETE'; }
  if (kind === 'talks') { const title = prompt('Name the next session (optional):', ''); if (title === null) return; body = new URLSearchParams({ title }); }
  await request(path + kind, { method, body });
  notify('Event updated.'); await refresh();
}
$('new-talk').onclick = () => action('talks').catch(showError);
$('end-event').onclick = () => action('end').catch(showError);
$('revoke').onclick = () => action('publisher-tokens/revoke').catch(showError);
$('delete-transcript').onclick = () => action('transcript').catch(showError);

// Event setup. Native dialog supplies focus trapping, Escape and focus return.
const defaults = new Set(['en', 'te', 'kn', 'ta']);
const english = { hi: 'Hindi', te: 'Telugu', kn: 'Kannada', ta: 'Tamil', bn: 'Bengali', mr: 'Marathi', gu: 'Gujarati', ml: 'Malayalam', or: 'Odia', pa: 'Punjabi' };
$('event-source').replaceChildren(...Object.entries(NAMES).map(([code, label]) => new Option(english[code] ? `${english[code]} · ${label}` : label, code)));
$('event-source').value = 'hi';
for (const [code, label] of Object.entries(NAMES)) {
  const input = el('input', { type: 'checkbox', value: code, name: 'target', checked: defaults.has(code) });
  const node = el('label', { className: 'language-option' }, input, el('span', { textContent: label, lang: code }));
  node.title = english[code] ?? label; $('target-languages').append(node);
}
function sourceChanged() { for (const input of $('target-languages').querySelectorAll('input')) { input.parentElement.hidden = input.value === $('event-source').value; if (input.value === $('event-source').value) input.checked = false; } }
$('event-source').onchange = sourceChanged; sourceChanged();
let slugEdited = false;
const slug = (text) => text.toLowerCase().normalize('NFKD').replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 70);
$('event-slug').oninput = () => { slugEdited = true; };
$('event-name').oninput = () => { if (!slugEdited) $('event-slug').value = slug($('event-name').value) || `event-${Date.now().toString(36)}`; };
function openCreate() {
  if (!connected) { showLogin(); return; }
  if ($('create-open').disabled) { notify('Event creation is managed by your deployment administrator.'); return; }
  $('create-error').textContent = ''; $('create-dialog').showModal();
}
$('create-open').onclick = openCreate; $('create-empty').onclick = openCreate;
$('create-close').onclick = () => { if (!creating) $('create-dialog').close(); };
$('create-dialog').addEventListener('cancel', (e) => { if (creating) e.preventDefault(); });
$('create-form').addEventListener('invalid', (e) => { const details = e.target.closest('details'); if (details) details.open = true; }, true);
$('create-form').onsubmit = async (e) => {
  e.preventDefault(); if (creating) return;
  creating = true; $('create-submit').disabled = true; $('create-submit').textContent = 'Creating…'; $('create-error').textContent = '';
  try {
    const input = { title: $('event-name').value.trim(), id: $('event-slug').value.trim(), speaker: $('event-speaker').value.trim(), source: $('event-source').value, targets: [...$('target-languages').querySelectorAll('input:checked')].filter((i) => !i.parentElement.hidden).map((i) => i.value), audiencePIN: $('event-pin').value, retentionDays: Number($('event-retention').value), exports: $('event-exports').value };
    const out = await (await request('/api/admin/events', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) })).json();
    selected = out.id; rooms.push(out); listSignature = ''; renderList(); renderEvent(); loadLinks(out.id);
    $('create-dialog').close(); $('create-form').reset(); $('event-source').value = 'hi';
    for (const input of $('target-languages').querySelectorAll('input')) input.checked = defaults.has(input.value);
    sourceChanged(); slugEdited = false;
    $('notice').textContent = 'Your event is ready. Copy the audience link below, then open your speaker console.'; $('notice').hidden = false;
    notify('Event created. Your audience link is ready.'); await refresh(); $('event-title').scrollIntoView({ block: 'nearest' });
  } catch (err) { $('create-error').textContent = err.message; const field = $('event-slug'); if (err.message.includes('link')) { field.closest('details').open = true; field.focus(); } }
  finally { creating = false; $('create-submit').disabled = false; $('create-submit').textContent = 'Create event →'; }
};
async function refresh() {
  if (refreshing) return; refreshing = true;
  try {
    const res = await fetch('/api/admin/status', { headers: auth() });
    if (res.status === 401 || res.status === 403) { showLogin(); if ($('token').value) $('error').textContent = 'That key could not open the organiser studio. Use your admin access key, not a speaker link.'; return; }
    if (!res.ok) throw new Error('Could not load your events. Please try again.');
    const next = await res.json(); if (!Array.isArray(next)) throw new Error('Unexpected event response.');
    const initial = !connected; rooms = next; connected = true; saveToken();
    $('login').hidden = true; $('studio').hidden = false; $('error').textContent = '';
    $('event-count').textContent = rooms.length; $('list-count').textContent = rooms.length;
    $('live-count').textContent = rooms.filter((r) => r.live && !r.paused).length;
    $('viewer-count').textContent = rooms.reduce((sum, r) => sum + r.viewers, 0);
    $('updated').textContent = `Updated ${new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`;
    if (!rooms.some((r) => r.id === selected)) selected = rooms[0]?.id ?? '';
    renderList(); renderEvent();
    if (initial) {
      if (selected) loadLinks(selected);
      const caps = await (await request('/api/admin/events')).json();
      $('create-open').disabled = !caps.create; $('create-empty').disabled = !caps.create;
      if (!caps.create) { $('notice').textContent = caps.reason; $('notice').hidden = false; }
    }
  } catch (e) { showError(e); } finally { refreshing = false; }
}
fetch('/api/site').then((r) => r.json()).then((site) => { publicBase = site.url.replace(/\/$/, ''); }).catch(() => {});
refresh(); setInterval(() => { if (connected) refresh(); }, 4000);
